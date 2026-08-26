package linear

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/snaylaker/lw/internal/domain"
	"github.com/snaylaker/lw/internal/lwerr"
)

type fakeDoer struct {
	requests []*http.Request
	bodies   []string
	respond  func(*http.Request) (*http.Response, error)
}

func (d *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	body := ""
	if req.Body != nil {
		raw, _ := io.ReadAll(req.Body)
		body = string(raw)
	}
	d.requests = append(d.requests, req)
	d.bodies = append(d.bodies, body)
	return d.respond(req)
}

func jsonResponder(status int, body string) func(*http.Request) (*http.Response, error) {
	return func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Request:    req,
		}, nil
	}
}

func TestEndpointAndSettingsURL(t *testing.T) {
	if Endpoint != "https://api.linear.app/graphql" {
		t.Errorf("Endpoint = %q", Endpoint)
	}
	if APIKeySettingsURL != "https://linear.app/settings/account/security" {
		t.Errorf("APIKeySettingsURL = %q", APIKeySettingsURL)
	}
}

func TestRawRequestSendsThePersonalKeyWithoutABearerPrefix(t *testing.T) {
	doer := &fakeDoer{respond: jsonResponder(200, `{"data":{"issues":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}`)}
	raw := NewRawRequest(domain.Credential{Key: "lin_api_secret"}, doer)

	after := "cursor-1"
	data, err := raw(context.Background(), IssuesQuery, Variables{First: 50, After: &after, Filter: issueStateFilter{State: notDone()}})
	if err != nil {
		t.Fatalf("raw: %v", err)
	}
	if len(data) == 0 {
		t.Error("data must survive the round trip")
	}
	req := doer.requests[0]
	if req.Method != http.MethodPost || req.URL.String() != Endpoint {
		t.Errorf("request = %s %s", req.Method, req.URL)
	}
	if got := req.Header.Get("Authorization"); got != "lin_api_secret" {
		t.Errorf("Authorization = %q, want the raw key", got)
	}
	if strings.Contains(req.Header.Get("Authorization"), "Bearer") {
		t.Error("Authorization must carry no Bearer prefix")
	}
	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	var sent struct {
		Query     string          `json:"query"`
		Variables json.RawMessage `json:"variables"`
	}
	if err := json.Unmarshal([]byte(doer.bodies[0]), &sent); err != nil {
		t.Fatalf("request body: %v", err)
	}
	if sent.Query != IssuesQuery {
		t.Errorf("query = %q", sent.Query)
	}
	want := `{"first":50,"after":"cursor-1","filter":{"state":{"type":{"nin":["completed","canceled"]}}}}`
	if string(sent.Variables) != want {
		t.Errorf("variables = %s, want %s", sent.Variables, want)
	}
}

func TestRawRequestSendsAnExplicitNullCursorOnTheFirstPage(t *testing.T) {
	doer := &fakeDoer{respond: jsonResponder(200, `{"data":{}}`)}
	raw := NewRawRequest(domain.Credential{Key: "k"}, doer)
	if _, err := raw(context.Background(), IssuesQuery, Variables{First: 1, Filter: map[string]string{}}); err != nil {
		t.Fatalf("raw: %v", err)
	}
	if !strings.Contains(doer.bodies[0], `"after":null`) {
		t.Errorf("body = %s, want an explicit null cursor", doer.bodies[0])
	}
}

func TestRawRequestMapsResponsesToAPIErrors(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantType   string
		wantStatus int
	}{
		{"forbidden", 403, `{}`, ErrTypeForbidden, 403},
		{"rate limited", 429, `{}`, ErrTypeRatelimited, 429},
		{"other 4xx", 400, `{}`, ErrTypeAuthenticationError, 400},
		{"internal", 500, `{}`, ErrTypeInternalError, 500},
		{"other 5xx", 502, `{}`, ErrTypeNetworkError, 502},
		{"explicit extension type", 200, `{"errors":[{"message":"nope","extensions":{"type":"Ratelimited"}}]}`, ErrTypeRatelimited, 200},
		{"errors without a type", 200, `{"errors":[{"message":"Access denied"}]}`, ErrTypeUnknown, 200},
		{"not json", 500, `<html>`, ErrTypeInternalError, 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doer := &fakeDoer{respond: jsonResponder(tc.status, tc.body)}
			_, err := NewRawRequest(domain.Credential{Key: "k"}, doer)(context.Background(), IssuesQuery, Variables{First: 1})
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v, want an APIError", err)
			}
			if apiErr.Type != tc.wantType || apiErr.Status != tc.wantStatus {
				t.Errorf("APIError = %d/%s, want %d/%s", apiErr.Status, apiErr.Type, tc.wantStatus, tc.wantType)
			}
		})
	}
}

func TestRawRequestMapsATransportFailure(t *testing.T) {
	doer := &fakeDoer{respond: func(*http.Request) (*http.Response, error) { return nil, errors.New("ECONNREFUSED") }}
	_, err := NewRawRequest(domain.Credential{Key: "k"}, doer)(context.Background(), IssuesQuery, Variables{First: 1})
	var transportErr *TransportError
	if !errors.As(err, &transportErr) {
		t.Fatalf("err = %v, want a TransportError", err)
	}
	if mapped := MapError(err); mapped.Kind != lwerr.LinearUnavailable {
		t.Errorf("mapped kind = %s, want linear_unavailable", mapped.Kind)
	}
}

func TestErrorTypeForStatus(t *testing.T) {
	cases := map[int]string{
		403: ErrTypeForbidden,
		429: ErrTypeRatelimited,
		401: ErrTypeAuthenticationError,
		404: ErrTypeAuthenticationError,
		500: ErrTypeInternalError,
		503: ErrTypeNetworkError,
		200: ErrTypeUnknown,
		0:   ErrTypeUnknown,
	}
	for status, want := range cases {
		if got := ErrorTypeForStatus(status); got != want {
			t.Errorf("ErrorTypeForStatus(%d) = %s, want %s", status, got, want)
		}
	}
}

func TestMapErrorTable(t *testing.T) {
	const reauth = "create a new Read key and update your credential source"
	cases := []struct {
		name           string
		err            error
		wantKind       lwerr.Kind
		wantMessage    string
		wantNextAction string
	}{
		{"anything else", errors.New("wrapped"), lwerr.Internal, "Unexpected error while talking to Linear.", "Retry; if it persists, report the issue."},
		{"context cancelled", context.Canceled, lwerr.Cancelled, "The Linear request was cancelled.", "Try again."},
		{"deadline", context.DeadlineExceeded, lwerr.Cancelled, "The Linear request was cancelled.", "Try again."},
		{"sentinel abort", ErrAborted, lwerr.Cancelled, "The Linear request was cancelled.", "Try again."},
		{"api error naming an abort", &APIError{Status: 500, Message: "This operation was aborted"}, lwerr.Cancelled, "The Linear request was cancelled.", "Try again."},
		{"authentication type", &APIError{Type: ErrTypeAuthenticationError}, lwerr.AuthRequired, "Linear rejected the credentials.", reauth},
		{"forbidden type", &APIError{Type: ErrTypeForbidden}, lwerr.AuthRequired, "Linear rejected the credentials.", reauth},
		{"401 without a type", &APIError{Status: 401}, lwerr.AuthRequired, "Linear rejected the credentials.", reauth},
		{"403 without a type", &APIError{Status: 403}, lwerr.AuthRequired, "Linear rejected the credentials.", reauth},
		{"network type", &APIError{Type: ErrTypeNetworkError}, lwerr.LinearUnavailable, "Linear is unreachable.", "Check your connection and retry."},
		{"rate limited", &APIError{Type: ErrTypeRatelimited, Status: 429}, lwerr.LinearUnavailable, "Linear is unreachable.", "Check your connection and retry."},
		{"502", &APIError{Status: 502, Type: ErrTypeNetworkError}, lwerr.LinearUnavailable, "Linear is unreachable.", "Check your connection and retry."},
		{"500 lands on status, not type", &APIError{Status: 500, Type: ErrTypeInternalError}, lwerr.LinearUnavailable, "Linear is unreachable.", "Check your connection and retry."},
		{"any other 4xx is authentication", &APIError{Status: 404, Type: ErrorTypeForStatus(404)}, lwerr.AuthRequired, "Linear rejected the credentials.", reauth},
		{"other api error", &APIError{Status: 200, Type: ErrTypeInvalidInput}, lwerr.Internal, "Linear returned an unexpected error.", "Retry; if it persists, report the issue."},
		{"transport", &TransportError{Cause: errors.New("boom")}, lwerr.LinearUnavailable, "Linear is unreachable.", "Check your connection and retry."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mapped := MapError(tc.err)
			if mapped.Kind != tc.wantKind {
				t.Errorf("kind = %s, want %s", mapped.Kind, tc.wantKind)
			}
			if mapped.Message != tc.wantMessage {
				t.Errorf("message = %q, want %q", mapped.Message, tc.wantMessage)
			}
			if mapped.NextAction != tc.wantNextAction {
				t.Errorf("next action = %q, want %q", mapped.NextAction, tc.wantNextAction)
			}
			if !errors.Is(mapped, tc.err) && mapped.Cause == nil {
				t.Error("the original error must ride on Cause")
			}
		})
	}
}

func TestMapErrorReturnsAnLwerrUnchanged(t *testing.T) {
	original := lwerr.New(lwerr.NotARepo, "not a repo", "cd into a checkout")
	if mapped := MapError(original); mapped != original {
		t.Errorf("mapped = %+v, want the original error", mapped)
	}
}

func TestMapErrorNeverCopiesTheAPIMessage(t *testing.T) {
	mapped := MapError(&APIError{Status: 400, Message: "Bearer lin_oauth_abc123"})
	if strings.Contains(mapped.Message, "lin_oauth_abc123") || strings.Contains(mapped.NextAction, "lin_oauth_abc123") {
		t.Errorf("credential material leaked: %q / %q", mapped.Message, mapped.NextAction)
	}
}

func TestRejectedKeyIsAdvisedToCreateANewKey(t *testing.T) {
	mapped := MapError(&APIError{Status: 401, Message: "nope"})
	if mapped.Kind != lwerr.AuthRequired {
		t.Fatalf("kind = %s, want auth_required", mapped.Kind)
	}
	if !strings.Contains(mapped.NextAction, "create a new Read key") {
		t.Errorf("next action = %q, want it to ask for a new Read key", mapped.NextAction)
	}
	// lw stores nothing, so no next action may point at a command that would
	// re-authenticate: the user updates whatever source they chose in §6. A next
	// action naming something that does not exist is the worst kind there is.
	for _, gone := range []string{"lw auth", "keychain", "stored credential"} {
		if strings.Contains(strings.ToLower(mapped.NextAction), gone) {
			t.Errorf("next action = %q, want no mention of %q", mapped.NextAction, gone)
		}
	}
	if !strings.Contains(mapped.NextAction, "credential source") {
		t.Errorf("next action = %q, want it to point at the user's credential source", mapped.NextAction)
	}
}

func TestMapErrorOfNilIsNil(t *testing.T) {
	if MapError(nil) != nil {
		t.Error("MapError(nil) must be nil")
	}
}
