package linear

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/snaylaker/lw/internal/domain"
	"github.com/snaylaker/lw/internal/lwerr"
)

const (
	// DefaultPageSize is how many nodes one page asks for.
	DefaultPageSize = 50
)

const ViewerQuery = `
query Viewer {
  viewer { id }
}`

// ProjectsQuery backs the optional project browser. One page keeps toggling
// responsive; filtering within those active projects happens locally.
const ProjectsQuery = `
query Projects($first: Int!, $after: String, $filter: ProjectFilter) {
  projects(first: $first, after: $after, filter: $filter, includeArchived: false) {
    nodes { id name updatedAt status { name type } }
    pageInfo { hasNextPage endCursor }
  }
}`

// TeamsQuery backs the optional team browser.
const TeamsQuery = `
query Teams($first: Int!, $after: String) {
  teams(first: $first, after: $after, includeArchived: false) {
    nodes { id key name }
    pageInfo { hasNextPage endCursor }
  }
}`

// IssuesQuery lists filtered issues and backs exact/team/project lookups.
const IssuesQuery = `
query Issues($first: Int!, $after: String, $filter: IssueFilter) {
  issues(first: $first, after: $after, filter: $filter) {
    nodes { id identifier title url branchName state { name type } team { id key name } project { id name } }
    pageInfo { hasNextPage endCursor }
  }
}`

// SearchIssuesQuery is the interactive entry point. Linear ranks these rows by
// relevance; lw deliberately preserves that order instead of sorting locally.
const SearchIssuesQuery = `
query SearchIssues($term: String!, $first: Int!, $after: String, $filter: IssueFilter) {
  searchIssues(term: $term, first: $first, after: $after, filter: $filter, includeArchived: false) {
    nodes { id identifier title url branchName state { name type } team { id key name } project { id name } }
    pageInfo { hasNextPage endCursor }
  }
}`

// issueIdentifierRE is anchored at both ends: an alphanumeric team key, a
// hyphen, then ASCII digits.
var issueIdentifierRE = regexp.MustCompile(`^([A-Za-z0-9]+)-([0-9]+)$`)

type ninFilter struct {
	Nin []string `json:"nin"`
}

type typeFilter struct {
	Type ninFilter `json:"type"`
}

// notDone excludes finished work from every list.
func notDone() typeFilter {
	return typeFilter{Type: ninFilter{Nin: []string{"completed", "canceled"}}}
}

type eqString struct {
	Eq string `json:"eq"`
}

type eqNumber struct {
	Eq json.Number `json:"eq"`
}

type projectsFilter struct {
	Status typeFilter `json:"status"`
}

type projectIDFilter struct {
	ID eqString `json:"id"`
}

type projectIssuesFilter struct {
	Project projectIDFilter `json:"project"`
	State   typeFilter      `json:"state"`
}

type teamIssuesFilter struct {
	Team  teamKeyFilter `json:"team"`
	State typeFilter    `json:"state"`
}

type issueStateFilter struct {
	State typeFilter `json:"state"`
}

type teamKeyFilter struct {
	Key eqString `json:"key"`
}

type issueLookupFilter struct {
	Number eqNumber      `json:"number"`
	Team   teamKeyFilter `json:"team"`
}

type pageInfo struct {
	HasNextPage bool    `json:"hasNextPage"`
	EndCursor   *string `json:"endCursor"`
}

type connection[N any] struct {
	Nodes    []N      `json:"nodes"`
	PageInfo pageInfo `json:"pageInfo"`
}

// ValidateCredential checks a pasted key before onboarding saves it. Only the
// viewer id is requested; no workspace data is cached by this probe.
func ValidateCredential(ctx context.Context, credential domain.Credential, client Doer) error {
	data, err := NewRawRequest(credential, client)(ctx, ViewerQuery, Variables{})
	if err != nil {
		return MapError(err)
	}
	if isEmptyData(data) {
		return lwerr.New(lwerr.AuthRequired, "Linear rejected the credentials.", "create a new Read key and retry")
	}
	var parsed struct {
		Viewer *struct {
			ID string `json:"id"`
		} `json:"viewer"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return MapError(err)
	}
	if parsed.Viewer == nil || parsed.Viewer.ID == "" {
		return lwerr.New(lwerr.AuthRequired, "Linear rejected the credentials.", "create a new Read key and retry")
	}
	return nil
}

type projectNode struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	UpdatedAt string `json:"updatedAt"`
	Status    *struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"status"`
}

type teamNode struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

type issueNode struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	BranchName string `json:"branchName"`
	State      *struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"state"`
	Team *struct {
		ID   string `json:"id"`
		Key  string `json:"key"`
		Name string `json:"name"`
	} `json:"team"`
	Project *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"project"`
}

func pickProjects(data json.RawMessage) (*connection[projectNode], error) {
	var parsed struct {
		Projects connection[projectNode] `json:"projects"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	return &parsed.Projects, nil
}

func pickTeams(data json.RawMessage) (*connection[teamNode], error) {
	var parsed struct {
		Teams connection[teamNode] `json:"teams"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	return &parsed.Teams, nil
}

func pickIssues(data json.RawMessage) (*connection[issueNode], error) {
	var parsed struct {
		Issues connection[issueNode] `json:"issues"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	return &parsed.Issues, nil
}

func pickSearchedIssues(data json.RawMessage) (*connection[issueNode], error) {
	var parsed struct {
		Issues connection[issueNode] `json:"searchIssues"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	return &parsed.Issues, nil
}

// compareNames uses a case-folded byte comparison. Every ASCII ordering is
// deterministic; non-ASCII ordering is stable but not locale-aware.
func compareNames(a, b string) int {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	if la != lb {
		return strings.Compare(la, lb)
	}
	return strings.Compare(a, b)
}

func toProjects(nodes []projectNode) []domain.Project {
	items := make([]domain.Project, 0, len(nodes))
	for _, node := range nodes {
		item := domain.Project{ID: node.ID, Name: node.Name, UpdatedAt: node.UpdatedAt}
		if node.Status != nil {
			item.StatusName = node.Status.Name
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, aerr := time.Parse(time.RFC3339, items[i].UpdatedAt)
		b, berr := time.Parse(time.RFC3339, items[j].UpdatedAt)
		if aerr == nil && berr == nil && !a.Equal(b) {
			return a.After(b)
		}
		return compareNames(items[i].Name, items[j].Name) < 0
	})
	return items
}

func toTeams(nodes []teamNode) []domain.Team {
	items := make([]domain.Team, 0, len(nodes))
	for _, node := range nodes {
		items = append(items, domain.Team{ID: node.ID, Key: node.Key, Name: node.Name})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if byName := compareNames(items[i].Name, items[j].Name); byName != 0 {
			return byName < 0
		}
		return compareNames(items[i].Key, items[j].Key) < 0
	})
	return items
}

func toIssue(node issueNode) domain.Issue {
	item := domain.Issue{
		ID:              node.ID,
		Identifier:      node.Identifier,
		Title:           node.Title,
		URL:             node.URL,
		SuggestedBranch: node.BranchName,
	}
	if node.State != nil {
		item.StateType = node.State.Type
		item.StateName = node.State.Name
	}
	if node.Team != nil {
		item.TeamID = node.Team.ID
		item.TeamKey = node.Team.Key
		item.TeamName = node.Team.Name
	}
	if node.Project != nil {
		item.ProjectID = node.Project.ID
		item.ProjectName = node.Project.Name
	}
	return item
}

// identifierNumber reads the digits after the last hyphen the way JS Number()
// does: an empty or non-numeric suffix, and any non-finite result, is 0.
func identifierNumber(identifier string) float64 {
	suffix := identifier[strings.LastIndex(identifier, "-")+1:]
	number, err := strconv.ParseFloat(strings.TrimSpace(suffix), 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0
	}
	return number
}

// toIssues lists open issues, newest (highest number) first within each team key.
func toIssues(nodes []issueNode) []domain.Issue {
	items := make([]domain.Issue, 0, len(nodes))
	for _, node := range nodes {
		items = append(items, toIssue(node))
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if team := compareNames(a.TeamKey, b.TeamKey); team != 0 {
			return team < 0
		}
		return identifierNumber(a.Identifier) > identifierNumber(b.Identifier)
	})
	return items
}

type ListProjectsRequest struct {
	Credential domain.Credential
	RawRequest RawRequest
	HTTPClient Doer
}

// ListProjects returns the first page of active projects for the optional
// project browser. The browser filters these rows locally as the user types.
func ListProjects(ctx context.Context, req ListProjectsRequest) ([]domain.Project, error) {
	data, requestErr := rawFor(req.RawRequest, req.Credential, req.HTTPClient)(ctx, ProjectsQuery, Variables{
		First:  DefaultPageSize,
		After:  nil,
		Filter: projectsFilter{Status: notDone()},
	})
	if requestErr != nil {
		return nil, MapError(requestErr)
	}
	if isEmptyData(data) {
		return nil, lwerr.New(lwerr.LinearUnavailable, "Linear returned an empty response.", "Retry in a moment.")
	}
	conn, err := pickProjects(data)
	if err != nil {
		return nil, MapError(err)
	}
	return toProjects(conn.Nodes), nil
}

type ListTeamsRequest struct {
	Credential domain.Credential
	RawRequest RawRequest
	HTTPClient Doer
}

// ListTeams returns one bounded, alphabetized page for the optional browser.
func ListTeams(ctx context.Context, req ListTeamsRequest) ([]domain.Team, error) {
	data, requestErr := rawFor(req.RawRequest, req.Credential, req.HTTPClient)(ctx, TeamsQuery, Variables{
		First: DefaultPageSize,
		After: nil,
	})
	if requestErr != nil {
		return nil, MapError(requestErr)
	}
	if isEmptyData(data) {
		return nil, lwerr.New(lwerr.LinearUnavailable, "Linear returned an empty response.", "Retry in a moment.")
	}
	conn, err := pickTeams(data)
	if err != nil {
		return nil, MapError(err)
	}
	return toTeams(conn.Nodes), nil
}

type ListTeamIssuesRequest struct {
	Credential domain.Credential
	TeamKey    string
	RawRequest RawRequest
	HTTPClient Doer
}

func ListTeamIssues(ctx context.Context, req ListTeamIssuesRequest) ([]domain.Issue, error) {
	return fetchTeamIssues(ctx, rawFor(req.RawRequest, req.Credential, req.HTTPClient), strings.ToUpper(req.TeamKey), DefaultPageSize)
}

type ListProjectIssuesRequest struct {
	Credential domain.Credential
	ProjectID  string
	RawRequest RawRequest
	HTTPClient Doer
}

// ListProjectIssues returns the first page of active issues for a selected
// project. The project issue view filters this bounded list locally.
func ListProjectIssues(ctx context.Context, req ListProjectIssuesRequest) ([]domain.Issue, error) {
	data, requestErr := rawFor(req.RawRequest, req.Credential, req.HTTPClient)(ctx, IssuesQuery, Variables{
		First: DefaultPageSize,
		After: nil,
		Filter: projectIssuesFilter{
			Project: projectIDFilter{ID: eqString{Eq: req.ProjectID}},
			State:   notDone(),
		},
	})
	if requestErr != nil {
		return nil, MapError(requestErr)
	}
	if isEmptyData(data) {
		return nil, lwerr.New(lwerr.LinearUnavailable, "Linear returned an empty response.", "Retry in a moment.")
	}
	conn, err := pickIssues(data)
	if err != nil {
		return nil, MapError(err)
	}
	return toIssues(conn.Nodes), nil
}

// FindIssuesRequest configures the launcher's single search box.
type FindIssuesRequest struct {
	Credential domain.Credential
	Query      string
	PageSize   int
	RawRequest RawRequest
	HTTPClient Doer
}

var teamQueryRE = regexp.MustCompile(`(?i)^[A-Z][A-Z0-9]*-?$`)

// FindIssues gives identifier, team and free-text searches distinct deterministic
// behavior while keeping one input. TEAM-123 resolves exactly; an uppercase
// team key such as DEMO lists that team's active issues; everything else uses
// Linear's ranked workspace search. A nonexistent team falls back to text so
// an uppercase word can still find an issue title.
func FindIssues(ctx context.Context, req FindIssuesRequest) ([]domain.Issue, error) {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return []domain.Issue{}, nil
	}
	raw := rawFor(req.RawRequest, req.Credential, req.HTTPClient)
	if issueIdentifierRE.MatchString(query) {
		issue, found, _, _, err := lookupIssue(ctx, ResolveIssueRequest{Identifier: query, RawRequest: raw})
		if err != nil {
			return nil, err
		}
		if !found || issue.StateType == "completed" || issue.StateType == "canceled" {
			return []domain.Issue{}, nil
		}
		return []domain.Issue{issue}, nil
	}
	if teamQueryRE.MatchString(query) {
		teamKey := strings.ToUpper(strings.TrimSuffix(query, "-"))
		items, err := fetchTeamIssues(ctx, raw, teamKey, req.PageSize)
		if err != nil {
			return nil, err
		}
		if len(items) > 0 {
			return items, nil
		}
	}
	return SearchIssues(ctx, SearchIssuesRequest{
		Term: query, PageSize: req.PageSize, RawRequest: raw,
	})
}

func fetchTeamIssues(ctx context.Context, raw RawRequest, teamKey string, pageSize int) ([]domain.Issue, error) {
	if pageSize <= 0 || pageSize > DefaultPageSize {
		pageSize = DefaultPageSize
	}
	data, requestErr := raw(ctx, IssuesQuery, Variables{
		First: pageSize,
		After: nil,
		Filter: teamIssuesFilter{
			Team:  teamKeyFilter{Key: eqString{Eq: teamKey}},
			State: notDone(),
		},
	})
	if requestErr != nil {
		return nil, MapError(requestErr)
	}
	if isEmptyData(data) {
		return nil, lwerr.New(lwerr.LinearUnavailable, "Linear returned an empty response.", "Retry in a moment.")
	}
	conn, err := pickIssues(data)
	if err != nil {
		return nil, MapError(err)
	}
	return toIssues(conn.Nodes), nil
}

// SearchIssuesRequest configures the workspace-wide interactive search.
type SearchIssuesRequest struct {
	Credential domain.Credential
	Term       string
	PageSize   int
	RawRequest RawRequest
	HTTPClient Doer
}

// SearchIssues searches every visible issue in the workspace. Linear owns the
// ranking, so the returned order is preserved. Finished issues are excluded,
// matching the launcher's focus on work that can still be started.
func SearchIssues(ctx context.Context, req SearchIssuesRequest) ([]domain.Issue, error) {
	term := strings.TrimSpace(req.Term)
	if term == "" {
		return []domain.Issue{}, nil
	}
	pageSize := req.PageSize
	if pageSize <= 0 || pageSize > DefaultPageSize {
		pageSize = DefaultPageSize
	}
	data, requestErr := rawFor(req.RawRequest, req.Credential, req.HTTPClient)(ctx, SearchIssuesQuery, Variables{
		First:  pageSize,
		After:  nil,
		Filter: issueStateFilter{State: notDone()},
		Term:   term,
	})
	if requestErr != nil {
		return nil, MapError(requestErr)
	}
	if isEmptyData(data) {
		return nil, lwerr.New(lwerr.LinearUnavailable, "Linear returned an empty response.", "Retry in a moment.")
	}
	conn, err := pickSearchedIssues(data)
	if err != nil {
		return nil, MapError(err)
	}
	items := make([]domain.Issue, 0, len(conn.Nodes))
	for _, node := range conn.Nodes {
		items = append(items, toIssue(node))
	}
	return items, nil
}

// ResolveIssueRequest configures ResolveIssue, which backs the --issue flag.
type ResolveIssueRequest struct {
	Credential domain.Credential
	Identifier string
	RawRequest RawRequest
	HTTPClient Doer
}

// ResolveIssue looks up a single issue by identifier (e.g. "ENG-3971"). A
// malformed identifier fails before any request is made.
func ResolveIssue(ctx context.Context, req ResolveIssueRequest) (domain.Issue, error) {
	issue, found, teamKey, literal, err := lookupIssue(ctx, req)
	if err != nil {
		return domain.Issue{}, err
	}
	if !found {
		return domain.Issue{}, lwerr.New(lwerr.Internal,
			fmt.Sprintf("No Linear issue found for %s-%s.", teamKey, literal),
			"Check the identifier and try again.")
	}
	return issue, nil
}

// lookupIssue shares exact lookup between direct mode (where absence is an
// actionable error) and interactive search (where absence is simply 0 rows).
func lookupIssue(ctx context.Context, req ResolveIssueRequest) (domain.Issue, bool, string, string, error) {
	match := issueIdentifierRE.FindStringSubmatch(strings.TrimSpace(req.Identifier))
	if match == nil {
		return domain.Issue{}, false, "", "", lwerr.New(lwerr.Internal,
			`"`+req.Identifier+`" is not a valid Linear issue identifier.`,
			"Use the TEAM-123 format, e.g. ENG-3971.")
	}
	teamKey := strings.ToUpper(match[1])
	literal := match[2]
	if number, err := strconv.ParseFloat(match[2], 64); err == nil {
		literal = strconv.FormatFloat(number, 'f', -1, 64)
	}

	data, requestErr := rawFor(req.RawRequest, req.Credential, req.HTTPClient)(ctx, IssuesQuery, Variables{
		First:  1,
		After:  nil,
		Filter: issueLookupFilter{Number: eqNumber{Eq: json.Number(literal)}, Team: teamKeyFilter{Key: eqString{Eq: teamKey}}},
	})
	if requestErr != nil {
		return domain.Issue{}, false, teamKey, literal, MapError(requestErr)
	}
	if isEmptyData(data) {
		return domain.Issue{}, false, teamKey, literal, nil
	}
	conn, err := pickIssues(data)
	if err != nil {
		return domain.Issue{}, false, teamKey, literal, MapError(err)
	}
	if len(conn.Nodes) == 0 {
		return domain.Issue{}, false, teamKey, literal, nil
	}
	return toIssue(conn.Nodes[0]), true, teamKey, literal, nil
}
