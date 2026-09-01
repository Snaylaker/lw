package linear

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/snaylaker/lw/internal/domain"
	"github.com/snaylaker/lw/internal/lwerr"
)

var testCredential = domain.Credential{Key: "t"}

type call struct {
	query     string
	variables Variables
}

type fakeRaw struct {
	pages []string
	calls []call
	next  int
}

// fn serves each page in turn and repeats the last one, so a page that always
// claims hasNextPage exercises the cap.
func (f *fakeRaw) fn(_ context.Context, query string, variables Variables) (json.RawMessage, error) {
	f.calls = append(f.calls, call{query: query, variables: variables})
	page := f.pages[min(f.next, len(f.pages)-1)]
	f.next++
	return json.RawMessage(page), nil
}

func throwingRaw(err error) RawRequest {
	return func(context.Context, string, Variables) (json.RawMessage, error) { return nil, err }
}

func issueNodeJSON(identifier, title, teamKey string) string {
	return fmt.Sprintf(`{"id":"id-%s","identifier":%q,"title":%q,"url":"https://linear.app/issue/%s","state":{"name":"In Progress","type":"started"},"team":{"key":%q}}`,
		identifier, identifier, title, identifier, teamKey)
}

func issuePage(nodes []string, hasNextPage bool, endCursor string) string {
	cursor := "null"
	if endCursor != "" {
		cursor = fmt.Sprintf("%q", endCursor)
	}
	return fmt.Sprintf(`{"issues":{"nodes":[%s],"pageInfo":{"hasNextPage":%t,"endCursor":%s}}}`,
		strings.Join(nodes, ","), hasNextPage, cursor)
}

func projectPage(nodes []string) string {
	return fmt.Sprintf(`{"projects":{"nodes":[%s],"pageInfo":{"hasNextPage":false,"endCursor":null}}}`,
		strings.Join(nodes, ","))
}

func teamPage(nodes []string) string {
	return fmt.Sprintf(`{"teams":{"nodes":[%s],"pageInfo":{"hasNextPage":false,"endCursor":null}}}`,
		strings.Join(nodes, ","))
}

func searchIssuePage(nodes []string) string {
	return fmt.Sprintf(`{"searchIssues":{"nodes":[%s],"pageInfo":{"hasNextPage":false,"endCursor":null}}}`,
		strings.Join(nodes, ","))
}

func issueIdentifiers(items []domain.Issue) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.WorktreeKey)
	}
	return ids
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func filterJSON(t *testing.T, variables Variables) string {
	t.Helper()
	encoded, err := json.Marshal(variables.Filter)
	if err != nil {
		t.Fatalf("marshal filter: %v", err)
	}
	return string(encoded)
}

func TestValidateCredentialRequiresAViewer(t *testing.T) {
	valid := &fakeDoer{respond: jsonResponder(200, `{"data":{"viewer":{"id":"user-1"}}}`)}
	if err := ValidateCredential(context.Background(), domain.Credential{Key: "secret"}, valid); err != nil {
		t.Fatal(err)
	}
	if len(valid.bodies) != 1 || !strings.Contains(valid.bodies[0], "query Viewer") {
		t.Errorf("request bodies = %v", valid.bodies)
	}

	rejected := &fakeDoer{respond: jsonResponder(200, `{"data":{"viewer":null}}`)}
	if err := ValidateCredential(context.Background(), domain.Credential{Key: "secret"}, rejected); !lwerr.Is(err, lwerr.AuthRequired) {
		t.Errorf("error = %v, want auth_required", err)
	}
}

func TestQueriesAreVerbatim(t *testing.T) {
	wantProjects := "\nquery Projects($first: Int!, $after: String, $filter: ProjectFilter) {\n" +
		"  projects(first: $first, after: $after, filter: $filter, includeArchived: false) {\n" +
		"    nodes { id name updatedAt status { name type } }\n" +
		"    pageInfo { hasNextPage endCursor }\n  }\n}"
	if ProjectsQuery != wantProjects {
		t.Errorf("ProjectsQuery = %q, want %q", ProjectsQuery, wantProjects)
	}
	wantTeams := "\nquery Teams($first: Int!, $after: String) {\n" +
		"  teams(first: $first, after: $after, includeArchived: false) {\n" +
		"    nodes { id key name }\n" +
		"    pageInfo { hasNextPage endCursor }\n  }\n}"
	if TeamsQuery != wantTeams {
		t.Errorf("TeamsQuery = %q, want %q", TeamsQuery, wantTeams)
	}
	wantIssues := "\nquery Issues($first: Int!, $after: String, $filter: IssueFilter) {\n" +
		"  issues(first: $first, after: $after, filter: $filter) {\n" +
		"    nodes { id identifier title url branchName state { name type } team { id key name } project { id name } }\n" +
		"    pageInfo { hasNextPage endCursor }\n  }\n}"
	if IssuesQuery != wantIssues {
		t.Errorf("IssuesQuery = %q, want %q", IssuesQuery, wantIssues)
	}
	wantSearch := "\nquery SearchIssues($term: String!, $first: Int!, $after: String, $filter: IssueFilter) {\n" +
		"  searchIssues(term: $term, first: $first, after: $after, filter: $filter, includeArchived: false) {\n" +
		"    nodes { id identifier title url branchName state { name type } team { id key name } project { id name } }\n" +
		"    pageInfo { hasNextPage endCursor }\n  }\n}"
	if SearchIssuesQuery != wantSearch {
		t.Errorf("SearchIssuesQuery = %q, want %q", SearchIssuesQuery, wantSearch)
	}
	if DefaultPageSize != 50 {
		t.Errorf("default page size = %d, want 50", DefaultPageSize)
	}
}

func TestListProjectsReturnsOneActivePageNewestFirst(t *testing.T) {
	raw := &fakeRaw{pages: []string{projectPage([]string{
		`{"id":"older","name":"Authentication","updatedAt":"2025-01-01T00:00:00Z","status":{"name":"Planned","type":"planned"}}`,
		`{"id":"newer","name":"CLI Reliability","updatedAt":"2025-02-01T00:00:00Z","status":{"name":"In Progress","type":"started"}}`,
	})}}
	items, err := ListProjects(context.Background(), ListProjectsRequest{RawRequest: raw.fn})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != "newer" || items[0].StatusName != "In Progress" {
		t.Fatalf("projects = %+v", items)
	}
	if len(raw.calls) != 1 || raw.calls[0].query != ProjectsQuery || raw.calls[0].variables.First != 50 {
		t.Fatalf("calls = %+v", raw.calls)
	}
	if got, want := filterJSON(t, raw.calls[0].variables), `{"status":{"type":{"nin":["completed","canceled"]}}}`; got != want {
		t.Fatalf("filter = %s, want %s", got, want)
	}
}

func TestListTeamsReturnsOneAlphabetizedPage(t *testing.T) {
	raw := &fakeRaw{pages: []string{teamPage([]string{
		`{"id":"demo","key":"DEMO","name":"Developer Experience"}`,
		`{"id":"eng","key":"ENG","name":"Engineering"}`,
	})}}
	items, err := ListTeams(context.Background(), ListTeamsRequest{RawRequest: raw.fn})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Key != "DEMO" || items[1].Key != "ENG" {
		t.Fatalf("teams = %+v", items)
	}
	if len(raw.calls) != 1 || raw.calls[0].query != TeamsQuery || raw.calls[0].variables.First != 50 {
		t.Fatalf("calls = %+v", raw.calls)
	}
}

func TestListTeamIssuesFiltersByTeamAndActiveState(t *testing.T) {
	raw := &fakeRaw{pages: []string{issuePage([]string{issueNodeJSON("DEMO-4009", "Fix", "DEMO")}, false, "")}}
	items, err := ListTeamIssues(context.Background(), ListTeamIssuesRequest{TeamKey: "demo", RawRequest: raw.fn})
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(issueIdentifiers(items), []string{"DEMO-4009"}) {
		t.Fatalf("issues = %+v", items)
	}
	if got, want := filterJSON(t, raw.calls[0].variables), `{"team":{"key":{"eq":"DEMO"}},"state":{"type":{"nin":["completed","canceled"]}}}`; got != want {
		t.Fatalf("filter = %s, want %s", got, want)
	}
}

func TestListProjectIssuesFiltersByProjectAndActiveState(t *testing.T) {
	raw := &fakeRaw{pages: []string{issuePage([]string{issueNodeJSON("DEMO-4009", "Fix", "DEMO")}, false, "")}}
	items, err := ListProjectIssues(context.Background(), ListProjectIssuesRequest{ProjectID: "project-cli", RawRequest: raw.fn})
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(issueIdentifiers(items), []string{"DEMO-4009"}) {
		t.Fatalf("issues = %+v", items)
	}
	if got, want := filterJSON(t, raw.calls[0].variables), `{"project":{"id":{"eq":"project-cli"}},"state":{"type":{"nin":["completed","canceled"]}}}`; got != want {
		t.Fatalf("filter = %s, want %s", got, want)
	}
}

func TestAuthenticationErrorsMapToAuthRequired(t *testing.T) {
	_, err := SearchIssues(context.Background(), SearchIssuesRequest{
		Term: "auth", RawRequest: throwingRaw(&APIError{Type: ErrTypeAuthenticationError}),
	})
	if !lwerr.Is(err, lwerr.AuthRequired) {
		t.Errorf("err = %v, want auth_required", err)
	}
}

func TestAbortsMapToCancelled(t *testing.T) {
	_, err := SearchIssues(context.Background(), SearchIssuesRequest{
		Term: "abort", RawRequest: throwingRaw(fmt.Errorf("This operation was aborted: %w", ErrAborted)),
	})
	if !lwerr.Is(err, lwerr.Cancelled) {
		t.Errorf("err = %v, want cancelled", err)
	}
}

func TestResolveIssueQueriesByTeamKeyAndNumber(t *testing.T) {
	node := strings.Replace(issueNodeJSON("ENG-3971", "Fix it", "ENG"),
		`,"state"`, `,"branchName":"alex/eng-3971-fix-it","state"`, 1)
	raw := &fakeRaw{pages: []string{issuePage([]string{node}, false, "")}}
	item, err := ResolveIssue(context.Background(), ResolveIssueRequest{
		Credential: testCredential, Identifier: "ENG-3971", RawRequest: raw.fn,
	})
	if err != nil {
		t.Fatalf("ResolveIssue: %v", err)
	}
	want := `{"number":{"eq":3971},"team":{"key":{"eq":"ENG"}}}`
	if got := filterJSON(t, raw.calls[0].variables); got != want {
		t.Errorf("filter = %s, want %s", got, want)
	}
	if raw.calls[0].query != IssuesQuery {
		t.Error("resolveIssue must reuse the issues query")
	}
	if raw.calls[0].variables.First != 1 || raw.calls[0].variables.After != nil {
		t.Errorf("variables = %+v, want first 1 and a null cursor", raw.calls[0].variables)
	}
	if item.WorktreeKey != "ENG-3971" || item.SuggestedBranch != "alex/eng-3971-fix-it" {
		t.Errorf("issue = %+v", item)
	}
}

func TestResolveIssueUppercasesTheIdentifier(t *testing.T) {
	raw := &fakeRaw{pages: []string{issuePage([]string{issueNodeJSON("ENG-3971", "Fix it", "ENG")}, false, "")}}
	if _, err := ResolveIssue(context.Background(), ResolveIssueRequest{
		Credential: testCredential, Identifier: " eng-3971 ", RawRequest: raw.fn,
	}); err != nil {
		t.Fatalf("ResolveIssue: %v", err)
	}
	want := `{"number":{"eq":3971},"team":{"key":{"eq":"ENG"}}}`
	if got := filterJSON(t, raw.calls[0].variables); got != want {
		t.Errorf("filter = %s, want %s", got, want)
	}
}

func TestResolveIssueRejectsMalformedIdentifiersWithoutCallingTheAPI(t *testing.T) {
	for _, bad := range []string{"", "ENG", "ENG-", "-3971", "ENG 3971", "ENG-12a", "ENG--12"} {
		called := false
		raw := func(context.Context, string, Variables) (json.RawMessage, error) {
			called = true
			return nil, nil
		}
		_, err := ResolveIssue(context.Background(), ResolveIssueRequest{
			Credential: testCredential, Identifier: bad, RawRequest: raw,
		})
		mapped, ok := lwerr.As(err)
		if !ok || mapped.Kind != lwerr.Internal {
			t.Fatalf("%q: err = %v, want internal", bad, err)
		}
		if mapped.NextAction == "" {
			t.Errorf("%q: next action is empty", bad)
		}
		if want := `"` + bad + `" is not a valid Linear issue identifier.`; mapped.Message != want {
			t.Errorf("%q: message = %q, want %q", bad, mapped.Message, want)
		}
		if mapped.NextAction != "Use the TEAM-123 format, e.g. ENG-3971." {
			t.Errorf("%q: next action = %q", bad, mapped.NextAction)
		}
		if called {
			t.Errorf("%q: rawRequest must not run", bad)
		}
	}
}

func TestResolveIssueReportsAMissingIssue(t *testing.T) {
	raw := &fakeRaw{pages: []string{issuePage(nil, false, "")}}
	_, err := ResolveIssue(context.Background(), ResolveIssueRequest{
		Credential: testCredential, Identifier: "ENG-9999", RawRequest: raw.fn,
	})
	mapped, ok := lwerr.As(err)
	if !ok || mapped.Kind != lwerr.Internal {
		t.Fatalf("err = %v, want internal", err)
	}
	if mapped.Message != "No Linear issue found for ENG-9999." {
		t.Errorf("message = %q", mapped.Message)
	}
	if mapped.NextAction != "Check the identifier and try again." {
		t.Errorf("next action = %q", mapped.NextAction)
	}
	if !strings.Contains(mapped.Message, "ENG-9999") {
		t.Error("message must name the identifier")
	}
}

func TestResolveIssueNormalizesTheNumberInTheNotFoundMessage(t *testing.T) {
	raw := &fakeRaw{pages: []string{issuePage(nil, false, "")}}
	_, err := ResolveIssue(context.Background(), ResolveIssueRequest{
		Credential: testCredential, Identifier: "eng-0009999", RawRequest: raw.fn,
	})
	mapped, _ := lwerr.As(err)
	if mapped == nil || mapped.Message != "No Linear issue found for ENG-9999." {
		t.Errorf("message = %v", err)
	}
}

func TestResolveIssueMapsRequestErrors(t *testing.T) {
	_, err := ResolveIssue(context.Background(), ResolveIssueRequest{
		Credential: testCredential,
		Identifier: "ENG-1",
		RawRequest: throwingRaw(&APIError{Status: 502, Type: ErrorTypeForStatus(502)}),
	})
	if !lwerr.Is(err, lwerr.LinearUnavailable) {
		t.Errorf("err = %v, want linear_unavailable", err)
	}
}

func TestResolveIssueEmptyDataIsNotFound(t *testing.T) {
	_, err := ResolveIssue(context.Background(), ResolveIssueRequest{
		Credential: testCredential,
		Identifier: "ENG-1",
		RawRequest: func(context.Context, string, Variables) (json.RawMessage, error) { return nil, nil },
	})
	mapped, ok := lwerr.As(err)
	if !ok || mapped.Kind != lwerr.Internal || mapped.Message != "No Linear issue found for ENG-1." {
		t.Errorf("err = %v, want a not-found internal error", err)
	}
}

func TestFindIssuesUsesRankedWorkspaceSearchForText(t *testing.T) {
	raw := &fakeRaw{pages: []string{searchIssuePage([]string{
		issueNodeJSON("DEMO-2", "Second by relevance", "DEMO"),
		issueNodeJSON("ENG-9", "Another semantic match", "ENG"),
	})}}
	items, err := FindIssues(context.Background(), FindIssuesRequest{Query: "authentication timeout", RawRequest: raw.fn})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := issueIdentifiers(items), []string{"DEMO-2", "ENG-9"}; !equalStrings(got, want) {
		t.Fatalf("issues = %v, want %v", got, want)
	}
	if len(raw.calls) != 1 || raw.calls[0].query != SearchIssuesQuery || raw.calls[0].variables.Term != "authentication timeout" {
		t.Fatalf("calls = %+v", raw.calls)
	}
	if got := filterJSON(t, raw.calls[0].variables); got != `{"state":{"type":{"nin":["completed","canceled"]}}}` {
		t.Errorf("filter = %s", got)
	}
}

func TestFindIssuesTreatsATeamKeyAsAnActiveTeamList(t *testing.T) {
	raw := &fakeRaw{pages: []string{issuePage([]string{
		issueNodeJSON("DEMO-4009", "Dynamic prompt", "DEMO"),
		issueNodeJSON("DEMO-4007", "Statement timeout", "DEMO"),
	}, false, "")}}
	items, err := FindIssues(context.Background(), FindIssuesRequest{Query: "demo", RawRequest: raw.fn})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := issueIdentifiers(items), []string{"DEMO-4009", "DEMO-4007"}; !equalStrings(got, want) {
		t.Fatalf("issues = %v, want %v", got, want)
	}
	if len(raw.calls) != 1 || raw.calls[0].query != IssuesQuery {
		t.Fatalf("calls = %+v", raw.calls)
	}
	if got := filterJSON(t, raw.calls[0].variables); got != `{"team":{"key":{"eq":"DEMO"}},"state":{"type":{"nin":["completed","canceled"]}}}` {
		t.Errorf("filter = %s", got)
	}
}

func TestFindIssuesFallsBackToTextWhenAnUppercaseWordIsNotATeam(t *testing.T) {
	raw := &fakeRaw{pages: []string{
		issuePage(nil, false, ""),
		searchIssuePage([]string{issueNodeJSON("ENG-1", "API timeout", "ENG")}),
	}}
	items, err := FindIssues(context.Background(), FindIssuesRequest{Query: "TIMEOUT", RawRequest: raw.fn})
	if err != nil {
		t.Fatal(err)
	}
	if got := issueIdentifiers(items); !equalStrings(got, []string{"ENG-1"}) {
		t.Fatalf("issues = %v", got)
	}
	if len(raw.calls) != 2 || raw.calls[1].query != SearchIssuesQuery {
		t.Fatalf("calls = %+v", raw.calls)
	}
}

func TestFindIssuesReturnsNoRowsForAnUnknownExactIdentifier(t *testing.T) {
	raw := &fakeRaw{pages: []string{issuePage(nil, false, "")}}
	items, err := FindIssues(context.Background(), FindIssuesRequest{Query: "DEMO-999999", RawRequest: raw.fn})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("items = %+v", items)
	}
}

func TestFindIssuesResolvesAnExactIdentifierAndCarriesRoutingMetadata(t *testing.T) {
	node := `{"id":"issue-1","identifier":"DEMO-4009","title":"Dynamic prompt","url":"https://linear.app/issue/DEMO-4009","state":{"name":"Todo","type":"unstarted"},"team":{"id":"team-demo","key":"DEMO","name":"Developer Experience"},"project":{"id":"project-cli","name":"CLI Reliability"}}`
	raw := &fakeRaw{pages: []string{issuePage([]string{node}, false, "")}}
	items, err := FindIssues(context.Background(), FindIssuesRequest{Query: "demo-4009", RawRequest: raw.fn})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
	project, hasProject := items[0].Scope("linear_project")
	team, hasTeam := items[0].Scope("linear_team")
	if items[0].Provider != "linear" || items[0].ExternalID != "issue-1" ||
		!hasProject || project.ID != "project-cli" || !hasTeam || team.ID != "team-demo" {
		t.Fatalf("items = %+v", items)
	}
	if len(raw.calls) != 1 || raw.calls[0].variables.Term != "" {
		t.Fatalf("calls = %+v", raw.calls)
	}
}

func TestNoQueryErrorLeaksTheCredential(t *testing.T) {
	const secret = "lin_api_super_secret"
	errs := []error{
		&APIError{Status: 401, Message: "bad key " + secret},
		&TransportError{Cause: errors.New("dial failed for " + secret)},
		errors.New("boom " + secret),
	}
	for _, cause := range errs {
		_, err := SearchIssues(context.Background(), SearchIssuesRequest{
			Credential: domain.Credential{Key: secret}, Term: "secret", RawRequest: throwingRaw(cause),
		})
		mapped, ok := lwerr.As(err)
		if !ok {
			t.Fatalf("err = %v, want an lwerr", err)
		}
		if strings.Contains(mapped.Message, secret) || strings.Contains(mapped.NextAction, secret) {
			t.Errorf("credential leaked: %q / %q", mapped.Message, mapped.NextAction)
		}
	}
}
