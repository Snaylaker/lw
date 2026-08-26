// Package linear talks to the Linear GraphQL API over net/http. The queries are
// hand-written on purpose: an SDK's generated getters (issue.state, issue.team,
// project.status) fire one request per node, which is unacceptable for a list.
package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/snaylaker/lw/internal/domain"
	"github.com/snaylaker/lw/internal/lwerr"
)

const (
	// Endpoint is the only Linear URL this package talks to.
	Endpoint = "https://api.linear.app/graphql"
	// APIKeySettingsURL is where a personal API key is created.
	APIKeySettingsURL = "https://linear.app/settings/account/security"
)

// Doer is the injection seam for every request; *http.Client satisfies it.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// The error labels Linear reports, mirrored so the mapping table can be
// written out by hand rather than derived by an SDK.
const (
	ErrTypeFeatureNotAccessible = "FeatureNotAccessible"
	ErrTypeInvalidInput         = "InvalidInput"
	ErrTypeRatelimited          = "Ratelimited"
	ErrTypeNetworkError         = "NetworkError"
	ErrTypeAuthenticationError  = "AuthenticationError"
	ErrTypeForbidden            = "Forbidden"
	ErrTypeBootstrapError       = "BootstrapError"
	ErrTypeUnknown              = "Unknown"
	ErrTypeInternalError        = "InternalError"
	ErrTypeOther                = "Other"
	ErrTypeUserError            = "UserError"
	ErrTypeGraphqlError         = "GraphqlError"
	ErrTypeLockTimeout          = "LockTimeout"
	ErrTypeUsageLimitExceeded   = "UsageLimitExceeded"
)

// ErrAborted marks a request the caller cancelled. context.Canceled and a
// deadline count as aborts too.
var ErrAborted = errors.New("AbortError")

// APIError is what Linear itself answered: a non-2xx response or a body
// carrying GraphQL errors. Message holds Linear's own text and is never copied
// into user-facing output — MapError replaces it with a fixed literal.
type APIError struct {
	Status  int
	Type    string
	Message string
	Cause   error
}

func (e *APIError) Error() string {
	return fmt.Sprintf("linear api error (status %d, type %s)", e.Status, e.Type)
}

func (e *APIError) Unwrap() error { return e.Cause }

// TransportError stands in for the TypeError a fetch throws when it fails
// before any response arrives.
type TransportError struct {
	Cause error
}

func (e *TransportError) Error() string { return "linear request failed" }

func (e *TransportError) Unwrap() error { return e.Cause }

// ErrorTypeForStatus reproduces how the SDK labelled a response that carried no
// explicit extensions.type.
func ErrorTypeForStatus(status int) string {
	switch {
	case status == 403:
		return ErrTypeForbidden
	case status == 429:
		return ErrTypeRatelimited
	case status >= 400 && status < 500:
		return ErrTypeAuthenticationError
	case status == 500:
		return ErrTypeInternalError
	case status >= 500 && status < 600:
		return ErrTypeNetworkError
	default:
		return ErrTypeUnknown
	}
}

// An API key has no expiry, so a rejected one is revoked or under-scoped —
// never stale. The user replaces it in whichever source supplied it.
func reauthenticateAction() string {
	return "create a new Read key and update your credential source"
}

var abortMessageRE = regexp.MustCompile(`(?i)abort`)

func isAbortError(err error) bool {
	return errors.Is(err, ErrAborted) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded)
}

// MapError normalizes any failure from the Linear API into an *lwerr.Error.
// Messages are fixed strings (never interpolated from the API's own error) so
// no request material can leak into user-facing text; the original error rides
// on Cause.
func MapError(err error) *lwerr.Error {
	if err == nil {
		return nil
	}
	if mapped, ok := lwerr.As(err); ok {
		return mapped
	}
	if isAbortError(err) {
		return lwerr.Wrap(err, lwerr.Cancelled, "The Linear request was cancelled.", "Try again.")
	}

	var apiErr *APIError
	if errors.As(err, &apiErr) {
		// An aborted request can surface as an API error; the message is all
		// there is to detect it by.
		if abortMessageRE.MatchString(apiErr.Message) {
			return lwerr.Wrap(err, lwerr.Cancelled, "The Linear request was cancelled.", "Try again.")
		}
		if apiErr.Type == ErrTypeAuthenticationError ||
			apiErr.Type == ErrTypeForbidden ||
			apiErr.Status == 401 ||
			apiErr.Status == 403 {
			return lwerr.Wrap(err, lwerr.AuthRequired, "Linear rejected the credentials.", reauthenticateAction())
		}
		if apiErr.Type == ErrTypeNetworkError ||
			apiErr.Type == ErrTypeRatelimited ||
			apiErr.Status >= 500 {
			return lwerr.Wrap(err, lwerr.LinearUnavailable,
				"Linear is unreachable.",
				"Check your connection and retry.")
		}
		return lwerr.Wrap(err, lwerr.Internal, "Linear returned an unexpected error.", "Retry; if it persists, report the issue.")
	}

	var transportErr *TransportError
	if errors.As(err, &transportErr) {
		return lwerr.Wrap(err, lwerr.LinearUnavailable, "Linear is unreachable.", "Check your connection and retry.")
	}

	return lwerr.Wrap(err, lwerr.Internal, "Unexpected error while talking to Linear.", "Retry; if it persists, report the issue.")
}

// Variables are the three GraphQL variables every list query takes. After is a
// pointer so the first page sends an explicit `null`.
type Variables struct {
	First  int     `json:"first"`
	After  *string `json:"after"`
	Filter any     `json:"filter"`
	Term   string  `json:"term,omitempty"`
}

// RawRequest performs one GraphQL request. Empty data with a nil error means the
// response carried no `data` member at all.
type RawRequest func(ctx context.Context, query string, variables Variables) (json.RawMessage, error)

type graphQLError struct {
	Message    string `json:"message"`
	Extensions struct {
		Type string `json:"type"`
	} `json:"extensions"`
}

func newAPIError(status int, errs []graphQLError) *APIError {
	apiErr := &APIError{Status: status, Type: ErrorTypeForStatus(status)}
	if len(errs) > 0 {
		apiErr.Message = errs[0].Message
		if errs[0].Extensions.Type != "" {
			apiErr.Type = errs[0].Extensions.Type
		}
	}
	if apiErr.Message == "" {
		apiErr.Message = fmt.Sprintf("HTTP %d", status)
	}
	return apiErr
}

// NewRawRequest builds the default transport: one POST per request, with the
// personal API key in the Authorization header raw — no `Bearer` prefix.
func NewRawRequest(credential domain.Credential, client Doer) RawRequest {
	if client == nil {
		client = http.DefaultClient
	}
	return func(ctx context.Context, query string, variables Variables) (json.RawMessage, error) {
		body, err := json.Marshal(struct {
			Query     string    `json:"query"`
			Variables Variables `json:"variables"`
		}{Query: query, Variables: variables})
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, Endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, &TransportError{Cause: err}
		}
		req.Header.Set("Authorization", credential.Key)
		req.Header.Set("Content-Type", "application/json")

		res, err := client.Do(req)
		if err != nil {
			return nil, &TransportError{Cause: err}
		}
		defer res.Body.Close()
		payload, err := io.ReadAll(res.Body)
		if err != nil {
			return nil, &TransportError{Cause: err}
		}
		var parsed struct {
			Data   json.RawMessage `json:"data"`
			Errors []graphQLError  `json:"errors"`
		}
		// A body that is not JSON leaves both members empty; the status alone
		// then decides.
		_ = json.Unmarshal(payload, &parsed)
		if res.StatusCode < 200 || res.StatusCode > 299 || len(parsed.Errors) > 0 {
			return nil, newAPIError(res.StatusCode, parsed.Errors)
		}
		return parsed.Data, nil
	}
}

func rawFor(raw RawRequest, credential domain.Credential, client Doer) RawRequest {
	if raw != nil {
		return raw
	}
	return NewRawRequest(credential, client)
}

// isEmptyData is the "data === undefined" test: an absent, null or blank data
// member.
func isEmptyData(data json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(data))
	return trimmed == "" || trimmed == "null"
}
