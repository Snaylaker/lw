package tui

import (
	"context"
	"reflect"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/snaylaker/lw/internal/credential"
	"github.com/snaylaker/lw/internal/domain"
	"github.com/snaylaker/lw/internal/lwerr"
)

var (
	testRepo       = domain.Repo{Root: "/repos/acme-api", Name: "acme-api"}
	testFlowResult = domain.FlowResult{CheckoutPath: "/tmp/worktrees/DEMO-4009", Created: true}
)

type appHarness struct {
	t     *testing.T
	model *Launcher
	sent  []tea.Msg

	holdFlow bool
	held     []tea.Cmd

	queries       []string
	flowIssues    []domain.Issue
	repoCalls     int
	recordedRepos []string
	recordedFor   []string
}

func newApp(t *testing.T, tweak func(*LauncherDeps, *appHarness)) *appHarness {
	t.Helper()
	h := &appHarness{t: t}
	deps := LauncherDeps{
		Repo: testRepo,
		ListRepos: func() []RankedRepo {
			h.repoCalls++
			return RankRepos(&testRepo, nil, []domain.Repo{{Root: "/repos/other", Name: "other"}})
		},
		SearchIssues: func(_ context.Context, query string) ([]domain.Issue, error) {
			h.queries = append(h.queries, query)
			return append([]domain.Issue(nil), testIssues...), nil
		},
		ListProjects: func(context.Context) ([]domain.Project, error) {
			return []domain.Project{{ID: "project-cli", Name: "CLI Reliability", StatusName: "In Progress"}}, nil
		},
		ListProjectIssues: func(_ context.Context, _ domain.Project) ([]domain.Issue, error) {
			return append([]domain.Issue(nil), testIssues[:1]...), nil
		},
		ListTeams: func(context.Context) ([]domain.Team, error) {
			return []domain.Team{{ID: "team-demo", Key: "DEMO", Name: "Developer Experience"}}, nil
		},
		ListTeamIssues: func(_ context.Context, _ domain.Team) ([]domain.Issue, error) {
			return append([]domain.Issue(nil), testIssues...), nil
		},
		RecordRepoUse: func(issue domain.Issue, repo domain.Repo) {
			h.recordedRepos = append(h.recordedRepos, repo.Root)
			h.recordedFor = append(h.recordedFor, issue.ID)
		},
		ResolveBranch: func(_ context.Context, _ domain.Repo, issue domain.Issue) (domain.BranchResolution, error) {
			branch := domain.Branch{Name: issue.WorktreeKey}
			return domain.BranchResolution{Selected: &branch}, nil
		},
		ChooseBranch: func(_ context.Context, _ domain.Repo, name string) (domain.Branch, error) {
			return domain.Branch{Name: name}, nil
		},
		ExecuteFlow: func(ctx context.Context, _ domain.Repo, issue domain.Issue, _ domain.Branch, onStage func(domain.StageUpdate)) (domain.FlowResult, error) {
			h.flowIssues = append(h.flowIssues, issue)
			onStage(domain.StageUpdate{Stage: domain.StagePreparing, State: domain.StateDone})
			onStage(domain.StageUpdate{Stage: domain.StageCreatingWorktree, State: domain.StateActive})
			if err := ctx.Err(); err != nil {
				return domain.FlowResult{}, err
			}
			return testFlowResult, nil
		},
		DoneClose: time.Millisecond,
	}
	if tweak != nil {
		tweak(&deps, h)
	}
	h.model = NewLauncher(deps)
	h.model.Send = func(msg tea.Msg) { h.sent = append(h.sent, msg) }
	return h
}

func (h *appHarness) start() { h.pump(h.model.Init()) }

func (h *appHarness) press(msg tea.Msg) {
	h.t.Helper()
	_, cmd := h.model.Update(msg)
	h.pump(cmd)
}

func (h *appHarness) search(query string) {
	h.t.Helper()
	if h.model.issuePicker == nil {
		h.t.Fatal("issue picker is not open")
	}
	h.model.issuePicker.SetQuery(query)
	h.model.issuePicker.SetSearching()
	h.pump(h.model.searchIssues(query))
}

func (h *appHarness) chooseFirstRepo() {
	h.t.Helper()
	if h.model.screen != ScreenRepos || h.model.repoPicker == nil {
		h.t.Fatalf("screen = %q, want repositories", h.model.screen)
	}
	h.press(typedKey(tea.KeyEnter))
}

func (h *appHarness) releaseFlow() {
	h.holdFlow = false
	held := h.held
	h.held = nil
	h.pump(held...)
}

func (h *appHarness) pump(cmds ...tea.Cmd) {
	h.t.Helper()
	queue := append([]tea.Cmd(nil), cmds...)
	for steps := 0; len(queue) > 0 || len(h.sent) > 0; steps++ {
		if steps > 500 {
			h.t.Fatal("the model never stopped producing work")
		}
		if len(h.sent) > 0 {
			msg := h.sent[0]
			h.sent = h.sent[1:]
			queue = append(queue, h.deliver(msg)...)
			continue
		}
		cmd := queue[0]
		queue = queue[1:]
		if cmd == nil {
			continue
		}
		if h.holdFlow && h.model.flowRunning {
			h.held = append(h.held, cmd)
			continue
		}
		queue = append(queue, h.deliver(cmd())...)
	}
}

func (h *appHarness) deliver(msg tea.Msg) []tea.Cmd {
	switch typed := msg.(type) {
	case nil, tea.QuitMsg:
		return nil
	case tea.BatchMsg:
		return typed
	}
	_, cmd := h.model.Update(msg)
	return []tea.Cmd{cmd}
}

func (h *appHarness) frame() string { return plain(h.model.View()) }

func (h *appHarness) outcome() LauncherOutcome {
	h.t.Helper()
	if !h.model.Settled() {
		h.t.Fatalf("launcher not settled; screen = %s", h.model.screen)
	}
	return h.model.Outcome()
}

func TestBranchResolutionPromptsForAnAmbiguousExistingBranch(t *testing.T) {
	var executed string
	h := newApp(t, func(deps *LauncherDeps, _ *appHarness) {
		repo := testRepo
		deps.PreselectRepo = &repo
		deps.ResolveBranch = func(context.Context, domain.Repo, domain.Issue) (domain.BranchResolution, error) {
			return domain.BranchResolution{Candidates: []domain.Branch{
				{Name: "alex/demo-4009-one", ExistingLocal: true},
				{Name: "alex/demo-4009-two", ExistingRemote: "refs/remotes/origin/alex/demo-4009-two"},
			}}, nil
		}
		deps.ExecuteFlow = func(_ context.Context, _ domain.Repo, _ domain.Issue, branch domain.Branch, _ func(domain.StageUpdate)) (domain.FlowResult, error) {
			executed = branch.Name
			return testFlowResult, nil
		}
	})
	h.start()
	h.search("DEMO")
	h.press(typedKey(tea.KeyEnter))
	if h.model.screen != ScreenBranches {
		t.Fatalf("screen = %q, want branches", h.model.screen)
	}
	mustContain(t, h.frame(), "Several existing branches")
	h.press(typedKey(tea.KeyEnter))
	if executed != "alex/demo-4009-one" {
		t.Fatalf("executed branch = %q", executed)
	}
}

func TestBranchResolutionOffersAnEditableLinearSuggestion(t *testing.T) {
	var chosen, executed string
	h := newApp(t, func(deps *LauncherDeps, _ *appHarness) {
		repo := testRepo
		deps.PreselectRepo = &repo
		deps.ResolveBranch = func(context.Context, domain.Repo, domain.Issue) (domain.BranchResolution, error) {
			return domain.BranchResolution{Suggested: "linear/demo-4009-suggestion"}, nil
		}
		deps.ChooseBranch = func(_ context.Context, _ domain.Repo, name string) (domain.Branch, error) {
			chosen = name
			return domain.Branch{Name: name, Base: "origin/main"}, nil
		}
		deps.ExecuteFlow = func(_ context.Context, _ domain.Repo, _ domain.Issue, branch domain.Branch, _ func(domain.StageUpdate)) (domain.FlowResult, error) {
			executed = branch.Name
			return testFlowResult, nil
		}
	})
	h.start()
	h.search("DEMO")
	h.press(typedKey(tea.KeyEnter))
	if h.model.screen != ScreenBranchInput {
		t.Fatalf("screen = %q, want branch input", h.model.screen)
	}
	mustContain(t, h.frame(), "linear/demo-4009-suggestion")
	mustContain(t, h.frame(), "Type to replace it")
	for _, r := range "team/demo-4009-custom" {
		h.press(runeKey(r))
	}
	h.press(typedKey(tea.KeyEnter))
	if chosen != "team/demo-4009-custom" || executed != chosen {
		t.Fatalf("chosen = %q, executed = %q", chosen, executed)
	}
}

func TestOnboardingContinuesDirectlyToIssueSearch(t *testing.T) {
	var key, root string
	h := newApp(t, func(deps *LauncherDeps, _ *appHarness) {
		deps.Credential = &CredentialSetup{
			File: "/secure/credentials",
			Save: func(_ context.Context, value string, target credential.Store) (credential.Location, error) {
				key = value
				if target != credential.StoreKeyring {
					t.Errorf("store = %v", target)
				}
				return credential.KeyringLocation(), nil
			},
		}
		deps.NeedsRepoRoot = true
		deps.SuggestedRepoRoot = "/home/alex/Work"
		deps.SetRepoRoot = func(value string) ([]RankedRepo, error) {
			root = value
			return RankRepos(nil, nil, []domain.Repo{{Root: value + "/api", Name: "api"}}), nil
		}
	})
	h.start()
	mustContain(t, h.frame(), "Connect to Linear")
	for _, r := range "lin_api_secret" {
		h.press(runeKey(r))
	}
	h.press(typedKey(tea.KeyEnter))
	if key != "lin_api_secret" {
		t.Errorf("key = %q", key)
	}
	mustContain(t, h.frame(), "Connected to Linear")
	h.press(typedKey(tea.KeyEnter))
	mustContain(t, h.frame(), "Where are your repositories?")
	h.press(typedKey(tea.KeyEnter))
	if root != "/home/alex/Work" {
		t.Errorf("root = %q", root)
	}
	mustContain(t, h.frame(), "Find a Linear issue")
	mustNotContain(t, h.frame(), "Choose a Linear project")
}

func TestFileFallbackStillRequiresExplicitApproval(t *testing.T) {
	var calls []credential.Store
	h := newApp(t, func(deps *LauncherDeps, _ *appHarness) {
		deps.Credential = &CredentialSetup{
			File: "/secure/credentials",
			Save: func(_ context.Context, _ string, target credential.Store) (credential.Location, error) {
				calls = append(calls, target)
				if target == credential.StoreKeyring {
					return credential.Location{}, credential.ErrKeyringUnavailable
				}
				return credential.FileLocation("/secure/credentials"), nil
			},
		}
	})
	h.start()
	for _, r := range "secret" {
		h.press(runeKey(r))
	}
	h.press(typedKey(tea.KeyEnter))
	mustContain(t, h.frame(), "has not been saved")
	h.press(typedKey(tea.KeyEnter))
	if !reflect.DeepEqual(calls, []credential.Store{credential.StoreKeyring, credential.StoreFile}) {
		t.Fatalf("stores = %v", calls)
	}
}

func TestTabTogglesProjectBrowserAndIssueSearch(t *testing.T) {
	projectLoads := 0
	h := newApp(t, func(deps *LauncherDeps, _ *appHarness) {
		deps.ListProjects = func(context.Context) ([]domain.Project, error) {
			projectLoads++
			return []domain.Project{
				{ID: "project-cli", Name: "CLI Reliability", StatusName: "In Progress"},
				{ID: "project-auth", Name: "Authentication", StatusName: "Planned"},
			}, nil
		}
	})
	h.start()
	h.model.issuePicker.SetQuery("timeout")
	h.press(typedKey(tea.KeyTab))
	if h.model.screen != ScreenProjects {
		t.Fatalf("screen = %q", h.model.screen)
	}
	mustContain(t, h.frame(), "Find a Linear project")
	mustContain(t, h.frame(), "CLI Reliability")
	if projectLoads != 1 {
		t.Fatalf("project loads = %d", projectLoads)
	}

	h.press(typedKey(tea.KeyTab))
	mustContain(t, h.frame(), "Find a Linear team")
	h.press(typedKey(tea.KeyTab))
	mustContain(t, h.frame(), "Find a Linear issue")
	if got := h.model.issuePicker.Query(); got != "timeout" {
		t.Fatalf("restored issue query = %q", got)
	}

	h.press(typedKey(tea.KeyTab))
	if projectLoads != 1 {
		t.Fatalf("cached cycle reloaded projects: %d", projectLoads)
	}
}

func TestProjectAndTeamPinsSurviveViewCycling(t *testing.T) {
	var projectPins, teamPins []string
	h := newApp(t, func(deps *LauncherDeps, _ *appHarness) {
		deps.ToggleProjectPin = func(project domain.Project) ([]string, error) {
			projectPins = []string{project.ID}
			return projectPins, nil
		}
		deps.ToggleTeamPin = func(team domain.Team) ([]string, error) {
			teamPins = []string{team.ID}
			return teamPins, nil
		}
	})
	h.start()
	h.press(typedKey(tea.KeyTab))
	h.press(typedKey(tea.KeyCtrlP))
	mustContain(t, h.frame(), "★ CLI Reliability")
	h.press(typedKey(tea.KeyTab))
	h.press(typedKey(tea.KeyCtrlP))
	mustContain(t, h.frame(), "★ Developer Experience")
	h.press(typedKey(tea.KeyTab)) // issues
	h.press(typedKey(tea.KeyTab)) // projects again
	mustContain(t, h.frame(), "★ CLI Reliability")
	if !reflect.DeepEqual(projectPins, []string{"project-cli"}) || !reflect.DeepEqual(teamPins, []string{"team-demo"}) {
		t.Fatalf("pins = projects %v, teams %v", projectPins, teamPins)
	}
}

func TestSelectingProjectOpensItsLocallyFilterableIssues(t *testing.T) {
	var projectIDs []string
	h := newApp(t, func(deps *LauncherDeps, _ *appHarness) {
		deps.ListProjectIssues = func(_ context.Context, project domain.Project) ([]domain.Issue, error) {
			projectIDs = append(projectIDs, project.ID)
			return append([]domain.Issue(nil), testIssues...), nil
		}
	})
	h.start()
	h.press(typedKey(tea.KeyTab))
	h.press(typedKey(tea.KeyEnter))
	mustContain(t, h.frame(), "Issues in CLI Reliability")
	mustContain(t, h.frame(), "DEMO-4009")
	if !reflect.DeepEqual(projectIDs, []string{"project-cli"}) {
		t.Fatalf("project issue loads = %v", projectIDs)
	}

	for _, r := range "timeout" {
		h.press(runeKey(r))
	}
	mustNotContain(t, h.frame(), "DEMO-4009")
	mustContain(t, h.frame(), "DEMO-4007")
	if len(h.queries) != 0 {
		t.Fatalf("project filtering called workspace search: %v", h.queries)
	}

	h.press(typedKey(tea.KeyEscape))
	mustContain(t, h.frame(), "Find a Linear project")
	h.press(typedKey(tea.KeyTab))
	mustContain(t, h.frame(), "Find a Linear team")
	h.press(typedKey(tea.KeyTab))
	mustContain(t, h.frame(), "Issues in CLI Reliability")
	if got := h.model.issuePicker.Query(); got != "timeout" {
		t.Fatalf("restored project issue query = %q", got)
	}
	mustContain(t, h.frame(), "DEMO-4007")
}

func TestSelectingTeamOpensItsLocallyFilterableIssues(t *testing.T) {
	var teamKeys []string
	h := newApp(t, func(deps *LauncherDeps, _ *appHarness) {
		deps.ListTeamIssues = func(_ context.Context, team domain.Team) ([]domain.Issue, error) {
			teamKeys = append(teamKeys, team.Key)
			return append([]domain.Issue(nil), testIssues...), nil
		}
	})
	h.start()
	h.press(typedKey(tea.KeyTab)) // projects
	h.press(typedKey(tea.KeyTab)) // teams
	mustContain(t, h.frame(), "Find a Linear team")
	h.press(typedKey(tea.KeyEnter))
	mustContain(t, h.frame(), "Issues in Developer Experience (DEMO)")
	if !reflect.DeepEqual(teamKeys, []string{"DEMO"}) {
		t.Fatalf("team issue loads = %v", teamKeys)
	}
	for _, r := range "timeout" {
		h.press(runeKey(r))
	}
	mustRowLabels(t, h.model.issuePicker.Rows(), "DEMO-4007 Repository scan timeout")
	if len(h.queries) != 0 {
		t.Fatalf("team filtering called workspace search: %v", h.queries)
	}
	h.press(typedKey(tea.KeyEscape))
	mustContain(t, h.frame(), "Find a Linear team")
}

func TestHappyPathSearchIssueRepositoryAndWorktree(t *testing.T) {
	h := newApp(t, nil)
	h.holdFlow = true
	h.start()
	mustContain(t, h.frame(), "Find a Linear issue")
	h.search("DEMO")
	mustContain(t, h.frame(), "DEMO-4009")
	mustContain(t, h.frame(), "DEMO-4007")
	if !reflect.DeepEqual(h.queries, []string{"DEMO"}) {
		t.Fatalf("queries = %v", h.queries)
	}

	h.press(typedKey(tea.KeyEnter))
	mustContain(t, h.frame(), "DEMO-4009 ▸ choose a repository")
	h.chooseFirstRepo()
	mustContain(t, h.frame(), "creating worktree")
	h.releaseFlow()

	outcome := h.outcome()
	if outcome.Cancelled || outcome.Result == nil || outcome.Result.CheckoutPath != testFlowResult.CheckoutPath {
		t.Fatalf("outcome = %+v", outcome)
	}
	if len(h.flowIssues) != 1 || h.flowIssues[0].WorktreeKey != "DEMO-4009" {
		t.Fatalf("flow issues = %+v", h.flowIssues)
	}
	if !reflect.DeepEqual(h.recordedFor, []string{"issue-1"}) {
		t.Fatalf("recorded issues = %v", h.recordedFor)
	}
}

func TestRememberedRepositorySkipsRepositoryPicker(t *testing.T) {
	remembered := domain.Repo{Root: "/repos/cli", Name: "cli"}
	h := newApp(t, func(deps *LauncherDeps, _ *appHarness) {
		deps.RepoForIssue = func(issue domain.Issue) (domain.Repo, bool) {
			project, ok := issue.Scope("linear_project")
			if !ok || project.ID != "project-cli" {
				t.Fatalf("scopes = %+v", issue.Scopes)
			}
			return remembered, true
		}
	})
	h.start()
	h.search("DEMO-4009")
	h.press(typedKey(tea.KeyEnter))
	if h.repoCalls != 0 {
		t.Fatalf("repository picker called %d times", h.repoCalls)
	}
	if outcome := h.outcome(); outcome.Result == nil {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestExplicitRepositorySkipsRepositoryPicker(t *testing.T) {
	named := domain.Repo{Root: "/repos/named", Name: "named"}
	h := newApp(t, func(deps *LauncherDeps, _ *appHarness) { deps.PreselectRepo = &named })
	h.start()
	h.search("timeout")
	h.press(typedKey(tea.KeyEnter))
	if h.repoCalls != 0 {
		t.Fatalf("repository picker called %d times", h.repoCalls)
	}
	if outcome := h.outcome(); outcome.Result == nil {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestEscapeReturnsFromRepositoriesToTheSameSearch(t *testing.T) {
	h := newApp(t, nil)
	h.start()
	h.search("DEMO")
	h.press(typedKey(tea.KeyDown))
	h.press(typedKey(tea.KeyEnter))
	mustContain(t, h.frame(), "DEMO-4007 ▸ choose a repository")
	h.press(typedKey(tea.KeyEsc))
	mustContain(t, h.frame(), "Find a Linear issue")
	if h.model.issuePicker.Query() != "DEMO" {
		t.Errorf("query = %q", h.model.issuePicker.Query())
	}
	mustContain(t, h.frame(), "DEMO-4007")
}

func TestEscapeOnIssueSearchCancels(t *testing.T) {
	h := newApp(t, nil)
	h.start()
	h.press(typedKey(tea.KeyEsc))
	outcome := h.outcome()
	if !outcome.Cancelled || outcome.Result != nil {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestLateSearchResultsCannotReplaceANewerQuery(t *testing.T) {
	h := newApp(t, nil)
	h.start()
	h.model.loadToken = 1
	h.model.issuePicker.SetQuery("new query")
	h.press(issuesLoadedMsg{token: 1, query: "old query", items: testIssues})
	if len(h.model.issuePicker.Rows()) != 0 {
		t.Fatalf("stale rows = %+v", h.model.issuePicker.Rows())
	}
}

func TestCtrlRRepeatsTheCurrentSearch(t *testing.T) {
	h := newApp(t, nil)
	h.start()
	h.search("DEMO")
	h.press(typedKey(tea.KeyCtrlR))
	if !reflect.DeepEqual(h.queries, []string{"DEMO", "DEMO"}) {
		t.Fatalf("queries = %v", h.queries)
	}
}

func TestSearchFailureOffersRetry(t *testing.T) {
	calls := 0
	h := newApp(t, func(deps *LauncherDeps, _ *appHarness) {
		deps.SearchIssues = func(context.Context, string) ([]domain.Issue, error) {
			calls++
			if calls == 1 {
				return nil, lwerr.New(lwerr.LinearUnavailable, "Linear is unreachable.", "Check your connection and retry.")
			}
			return testIssues, nil
		}
	})
	h.start()
	h.search("DEMO")
	mustContain(t, h.frame(), "[r] retry")
	h.press(runeKey('r'))
	mustContain(t, h.frame(), "DEMO-4009")
	if calls != 2 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestCtrlCCancelsAFlowOnlyAfterItStops(t *testing.T) {
	h := newApp(t, nil)
	h.holdFlow = true
	h.start()
	h.search("DEMO")
	h.press(typedKey(tea.KeyEnter))
	h.chooseFirstRepo()
	h.press(typedKey(tea.KeyCtrlC))
	if h.model.Settled() {
		t.Fatal("settled before flow returned")
	}
	mustContain(t, h.frame(), "cancelling…")
	h.releaseFlow()
	if outcome := h.outcome(); !outcome.Cancelled {
		t.Fatalf("outcome = %+v", outcome)
	}
}
