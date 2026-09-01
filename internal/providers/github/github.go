// Package github implements read-only GitHub issue search and lookup.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/snaylaker/lw/internal/lwerr"
	issueprovider "github.com/snaylaker/lw/provider"
)

const defaultAPIURL = "https://api.github.com"

var (
	fullReferenceRE = regexp.MustCompile(`^([^/\s]+)/([^/#\s]+)#([1-9][0-9]*)$`)
	numberRE        = regexp.MustCompile(`^#?([1-9][0-9]*)$`)
)

type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

type Options struct {
	Token      string
	APIURL     string
	Repository string
	HTTPClient Doer
}

var _ issueprovider.Provider = (*Client)(nil)

type Client struct {
	token      string
	apiURL     string
	repository string
	http       Doer
}

func New(options Options) (*Client, error) {
	apiURL := strings.TrimRight(strings.TrimSpace(options.APIURL), "/")
	if apiURL == "" {
		apiURL = defaultAPIURL
	}
	parsed, err := url.Parse(apiURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && !localHost(parsed.Hostname())) {
		return nil, lwerr.New(lwerr.ConfigInvalid, "GITHUB_API_URL is not a safe absolute URL.",
			"use an https GitHub API URL")
	}
	client := options.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return &Client{
		token: strings.TrimSpace(options.Token), apiURL: apiURL,
		repository: strings.Trim(strings.TrimSpace(options.Repository), "/"), http: client,
	}, nil
}

func (*Client) ID() issueprovider.ID { return issueprovider.GitHub }
func (*Client) DisplayName() string  { return "GitHub" }

func (c *Client) ValidateReference(reference string) error {
	_, _, _, err := c.parseReference(reference)
	return err
}

func (c *Client) Resolve(ctx context.Context, reference string) (issueprovider.WorkItem, error) {
	owner, repo, number, err := c.parseReference(reference)
	if err != nil {
		return issueprovider.WorkItem{}, err
	}
	var issue issueJSON
	if err := c.get(ctx, fmt.Sprintf("/repos/%s/%s/issues/%d", url.PathEscape(owner), url.PathEscape(repo), number), nil, &issue); err != nil {
		return issueprovider.WorkItem{}, err
	}
	if isPullRequest(issue.PullRequest) {
		return issueprovider.WorkItem{}, lwerr.New(lwerr.ConfigInvalid,
			fmt.Sprintf("%s/%s#%d is a pull request, not an issue", owner, repo, number),
			"choose a GitHub issue and re-run")
	}
	return toWorkItem(issue, owner, repo), nil
}

func (c *Client) Search(ctx context.Context, query string) ([]issueprovider.WorkItem, error) {
	if c.ValidateReference(query) == nil {
		item, err := c.Resolve(ctx, query)
		if err != nil {
			return nil, err
		}
		if item.StateType == "completed" {
			return nil, nil
		}
		return []issueprovider.WorkItem{item}, nil
	}
	search := strings.TrimSpace(query) + " is:issue is:open"
	if c.repository != "" {
		search += " repo:" + c.repository
	}
	params := url.Values{"q": {search}, "per_page": {"20"}}
	var response struct {
		Items []issueJSON `json:"items"`
	}
	if err := c.get(ctx, "/search/issues", params, &response); err != nil {
		return nil, err
	}
	items := make([]issueprovider.WorkItem, 0, len(response.Items))
	for _, issue := range response.Items {
		if isPullRequest(issue.PullRequest) {
			continue
		}
		owner, repo, ok := repositoryFromAPIURL(issue.RepositoryURL)
		if !ok {
			continue
		}
		items = append(items, toWorkItem(issue, owner, repo))
	}
	return items, nil
}

func (c *Client) parseReference(reference string) (string, string, int, error) {
	reference = strings.TrimSpace(reference)
	if match := fullReferenceRE.FindStringSubmatch(reference); match != nil {
		number, _ := strconv.Atoi(match[3])
		return match[1], match[2], number, nil
	}
	if match := numberRE.FindStringSubmatch(reference); match != nil && c.repository != "" {
		owner, repo, ok := strings.Cut(c.repository, "/")
		if ok && owner != "" && repo != "" && !strings.Contains(repo, "/") {
			number, _ := strconv.Atoi(match[1])
			return owner, repo, number, nil
		}
	}
	return "", "", 0, fmt.Errorf("expected owner/repository#123, or #123 inside a GitHub repository")
}

func (c *Client) get(ctx context.Context, path string, params url.Values, destination any) error {
	endpoint := c.apiURL + path
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "lw")
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return lwerr.NewCancelled()
		}
		return lwerr.Wrap(err, lwerr.ProviderUnavailable, "GitHub is unreachable.",
			"check your network and GitHub API URL, then re-run")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
		return githubStatusError(response.StatusCode, c.token != "")
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(destination); err != nil {
		return lwerr.Wrap(err, lwerr.ProviderUnavailable, "GitHub returned an unreadable response.",
			"retry; if it continues, check the configured GitHub API URL")
	}
	return nil
}

func githubStatusError(status int, authenticated bool) error {
	switch status {
	case http.StatusUnauthorized:
		return lwerr.New(lwerr.AuthRequired, "GitHub rejected the token.",
			"set a valid read-only GITHUB_TOKEN and re-run")
	case http.StatusForbidden:
		next := "set GITHUB_TOKEN for private issues or a higher API rate limit, then re-run"
		if authenticated {
			next = "check the token's repository access and GitHub API rate limit, then re-run"
		}
		return lwerr.New(lwerr.ProviderUnavailable, "GitHub refused the issue request.", next)
	case http.StatusNotFound:
		return lwerr.New(lwerr.ConfigInvalid, "GitHub issue not found.",
			"check the owner, repository, issue number, and token access")
	default:
		return lwerr.New(lwerr.ProviderUnavailable,
			fmt.Sprintf("GitHub returned HTTP %d.", status), "retry; if it continues, check GitHub status")
	}
}

type issueJSON struct {
	ID            int64           `json:"id"`
	NodeID        string          `json:"node_id"`
	Number        int             `json:"number"`
	Title         string          `json:"title"`
	HTMLURL       string          `json:"html_url"`
	State         string          `json:"state"`
	StateReason   string          `json:"state_reason"`
	RepositoryURL string          `json:"repository_url"`
	PullRequest   json.RawMessage `json:"pull_request"`
}

func toWorkItem(issue issueJSON, owner, repo string) issueprovider.WorkItem {
	reference := fmt.Sprintf("%s/%s#%d", owner, repo, issue.Number)
	key := fmt.Sprintf("GH-%s-%s-%d", safePart(owner), safePart(repo), issue.Number)
	externalID := issue.NodeID
	if externalID == "" {
		externalID = strconv.FormatInt(issue.ID, 10)
	}
	stateType := issue.State
	if issue.State == "closed" {
		stateType = "completed"
	}
	return issueprovider.WorkItem{
		Provider: issueprovider.GitHub, ExternalID: externalID,
		Reference: reference, WorktreeKey: key, Title: issue.Title, URL: issue.HTMLURL,
		StateType: stateType, StateName: issue.State,
		BranchKeys: []string{key, fmt.Sprintf("GH-%d", issue.Number), strconv.Itoa(issue.Number)},
		Scopes: []issueprovider.Scope{{
			Kind: "repository", ID: strings.ToLower(owner + "/" + repo),
			Key: owner + "/" + repo, Name: owner + "/" + repo,
		}},
	}
}

func isPullRequest(value json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(value))
	return trimmed != "" && trimmed != "null"
}

func repositoryFromAPIURL(value string) (string, string, bool) {
	parsed, err := url.Parse(value)
	if err != nil {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 3 || parts[len(parts)-3] != "repos" {
		return "", "", false
	}
	return parts[len(parts)-2], parts[len(parts)-1], true
}

func localHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func safePart(value string) string {
	var result []rune
	separator := false
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			result = append(result, r)
			separator = false
		} else if !separator && len(result) > 0 {
			result = append(result, '-')
			separator = true
		}
	}
	return strings.Trim(string(result), "-")
}
