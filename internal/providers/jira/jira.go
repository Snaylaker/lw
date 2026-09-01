// Package jira implements read-only Jira Cloud issue search and lookup.
package jira

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/snaylaker/lw/internal/lwerr"
	issueprovider "github.com/snaylaker/lw/provider"
)

var referenceRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*-[1-9][0-9]*$`)

type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

type Options struct {
	BaseURL    string
	Email      string
	Token      string
	HTTPClient Doer
}

var _ issueprovider.Provider = (*Client)(nil)

type Client struct {
	baseURL string
	email   string
	token   string
	http    Doer
}

func New(options Options) (*Client, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(options.BaseURL), "/")
	if baseURL == "" {
		return nil, lwerr.New(lwerr.ConfigInvalid, "JIRA_BASE_URL is not configured.",
			"set it to the Jira Cloud site URL, for example https://example.atlassian.net")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && !localHost(parsed.Hostname())) {
		return nil, lwerr.New(lwerr.ConfigInvalid, "JIRA_BASE_URL is not a safe absolute URL.",
			"use an https Jira Cloud URL")
	}
	token := strings.TrimSpace(options.Token)
	email := strings.TrimSpace(options.Email)
	if token == "" || email == "" {
		return nil, lwerr.New(lwerr.AuthRequired, "Jira credentials are not configured.",
			"set JIRA_EMAIL and JIRA_API_TOKEN, then re-run")
	}
	client := options.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return &Client{baseURL: baseURL, email: email, token: token, http: client}, nil
}

func (*Client) ID() issueprovider.ID { return issueprovider.Jira }
func (*Client) DisplayName() string  { return "Jira" }

func ValidateReference(reference string) error {
	if !referenceRE.MatchString(strings.TrimSpace(reference)) {
		return fmt.Errorf("expected an issue key like PROJ-123")
	}
	return nil
}

func (*Client) ValidateReference(reference string) error { return ValidateReference(reference) }

func (c *Client) Resolve(ctx context.Context, reference string) (issueprovider.WorkItem, error) {
	reference = strings.ToUpper(strings.TrimSpace(reference))
	if err := c.ValidateReference(reference); err != nil {
		return issueprovider.WorkItem{}, err
	}
	params := url.Values{"fields": {"summary,status,project"}}
	var issue issueJSON
	if err := c.request(ctx, http.MethodGet, "/rest/api/3/issue/"+url.PathEscape(reference), params, nil, &issue); err != nil {
		return issueprovider.WorkItem{}, err
	}
	return c.toWorkItem(issue), nil
}

func (c *Client) Search(ctx context.Context, query string) ([]issueprovider.WorkItem, error) {
	query = strings.TrimSpace(query)
	jql := `text ~ "` + escapeJQL(query) + `" AND statusCategory != Done ORDER BY updated DESC`
	if ValidateReference(query) == nil {
		jql = `key = "` + strings.ToUpper(query) + `" AND statusCategory != Done`
	}
	body := struct {
		JQL        string   `json:"jql"`
		Fields     []string `json:"fields"`
		MaxResults int      `json:"maxResults"`
	}{
		JQL:        jql,
		Fields:     []string{"summary", "status", "project"},
		MaxResults: 20,
	}
	var response struct {
		Issues []issueJSON `json:"issues"`
	}
	if err := c.request(ctx, http.MethodPost, "/rest/api/3/search/jql", nil, body, &response); err != nil {
		return nil, err
	}
	items := make([]issueprovider.WorkItem, 0, len(response.Issues))
	for _, issue := range response.Issues {
		items = append(items, c.toWorkItem(issue))
	}
	return items, nil
}

func (c *Client) request(ctx context.Context, method, path string, params url.Values, body, destination any) error {
	endpoint := c.baseURL + path
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}
	var source io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		source = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, source)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(c.email+":"+c.token)))
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return lwerr.NewCancelled()
		}
		return lwerr.Wrap(err, lwerr.ProviderUnavailable, "Jira is unreachable.",
			"check your network and JIRA_BASE_URL, then re-run")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
		return jiraStatusError(response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(destination); err != nil {
		return lwerr.Wrap(err, lwerr.ProviderUnavailable, "Jira returned an unreadable response.",
			"retry; if it continues, check JIRA_BASE_URL")
	}
	return nil
}

func jiraStatusError(status int) error {
	switch status {
	case http.StatusUnauthorized:
		return lwerr.New(lwerr.AuthRequired, "Jira rejected the credentials.",
			"check JIRA_EMAIL and JIRA_API_TOKEN, then re-run")
	case http.StatusForbidden:
		return lwerr.New(lwerr.ProviderUnavailable, "Jira refused the issue request.",
			"check the account's Jira project permissions, then re-run")
	case http.StatusNotFound:
		return lwerr.New(lwerr.ConfigInvalid, "Jira issue not found.",
			"check the issue key, Jira site URL, and project access")
	default:
		return lwerr.New(lwerr.ProviderUnavailable, fmt.Sprintf("Jira returned HTTP %d.", status),
			"retry; if it continues, check Jira status")
	}
}

type issueJSON struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	Fields struct {
		Summary string `json:"summary"`
		Status  struct {
			Name           string `json:"name"`
			StatusCategory struct {
				Key string `json:"key"`
			} `json:"statusCategory"`
		} `json:"status"`
		Project struct {
			ID   string `json:"id"`
			Key  string `json:"key"`
			Name string `json:"name"`
		} `json:"project"`
	} `json:"fields"`
}

func (c *Client) toWorkItem(issue issueJSON) issueprovider.WorkItem {
	key := strings.ToUpper(issue.Key)
	stateType := issue.Fields.Status.StatusCategory.Key
	switch stateType {
	case "done":
		stateType = "completed"
	case "indeterminate":
		stateType = "started"
	case "new":
		stateType = "unstarted"
	}
	return issueprovider.WorkItem{
		Provider: issueprovider.Jira, ExternalID: issue.ID,
		Reference: key, WorktreeKey: key, Title: issue.Fields.Summary,
		URL:       c.baseURL + "/browse/" + url.PathEscape(key),
		StateType: stateType, StateName: issue.Fields.Status.Name,
		BranchKeys: []string{key},
		Scopes: []issueprovider.Scope{{
			Kind: "project", ID: issue.Fields.Project.ID,
			Key: issue.Fields.Project.Key, Name: issue.Fields.Project.Name,
		}},
	}
}

func escapeJQL(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

func localHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
