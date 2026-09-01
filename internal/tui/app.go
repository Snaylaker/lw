package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/snaylaker/lw/internal/credential"
	"github.com/snaylaker/lw/internal/domain"
	"github.com/snaylaker/lw/internal/lwerr"
)

// CredentialSetup is present only when onboarding must collect a key. Grouping
// the file destination with its save operation prevents partial setup states.
type CredentialSetup struct {
	File string
	Save func(context.Context, string, credential.Store) (credential.Location, error)
}

// LauncherDeps is every side effect the launcher screens need. The model owns
// only sequencing; providers, config and git remain behind injected functions.
type LauncherDeps struct {
	Credential        *CredentialSetup
	ProviderName      string
	BrowseCollections bool
	// Repo is the repository the user is standing in, if any. It is offered as
	// the first row, not silently assumed by the interactive flow.
	Repo          domain.Repo
	PreselectRepo *domain.Repo // --repo; skips repository selection

	NeedsRepoRoot     bool
	SuggestedRepoRoot string
	ListRepos         func() []RankedRepo
	SetRepoRoot       func(string) ([]RankedRepo, error)

	// SearchIssues searches the selected provider. RepoForIssue uses the
	// issue's durable provider scope association when one exists.
	SearchIssues      func(context.Context, string) ([]domain.Issue, error)
	ListProjects      func(context.Context) ([]domain.Project, error)
	ListProjectIssues func(context.Context, domain.Project) ([]domain.Issue, error)
	ProjectPins       []string
	ToggleProjectPin  func(domain.Project) ([]string, error)
	ListTeams         func(context.Context) ([]domain.Team, error)
	ListTeamIssues    func(context.Context, domain.Team) ([]domain.Issue, error)
	TeamPins          []string
	ToggleTeamPin     func(domain.Team) ([]string, error)
	RepoForIssue      func(domain.Issue) (domain.Repo, bool)
	RecordRepoUse     func(domain.Issue, domain.Repo)

	// ResolveBranch runs after issue and repository selection. ChooseBranch
	// validates an edited name without fetching a second time.
	ResolveBranch func(context.Context, domain.Repo, domain.Issue) (domain.BranchResolution, error)
	ChooseBranch  func(context.Context, domain.Repo, string) (domain.Branch, error)

	// ExecuteFlow creates or reuses the worktree for the already-resolved branch.
	ExecuteFlow func(context.Context, domain.Repo, domain.Issue, domain.Branch, func(domain.StageUpdate)) (domain.FlowResult, error)
	DoneClose   time.Duration
}

const defaultDoneClose = 700 * time.Millisecond

// LauncherOutcome is what the caller maps to an exit code: cancelled is 130, a
// result is 0, and neither (Escape on the error view) is 1.
type LauncherOutcome struct {
	Result    *domain.FlowResult
	Cancelled bool
}

type Screen string

type issueSourceKind uint8

const (
	issueSourceWorkspace issueSourceKind = iota
	issueSourceProject
	issueSourceTeam
)

type issueSource struct {
	kind    issueSourceKind
	project domain.Project
	team    domain.Team
}

func projectSource(project domain.Project) issueSource {
	return issueSource{kind: issueSourceProject, project: project}
}

func teamSource(team domain.Team) issueSource {
	return issueSource{kind: issueSourceTeam, team: team}
}

const (
	ScreenCredential      Screen = "credential"
	ScreenCredentialSaved Screen = "credential-saved"
	ScreenRoot            Screen = "root"
	ScreenIssues          Screen = "issues"
	ScreenProjects        Screen = "projects"
	ScreenTeams           Screen = "teams"
	ScreenRepos           Screen = "repos"
	ScreenBranchLoading   Screen = "branch-loading"
	ScreenBranches        Screen = "branches"
	ScreenBranchInput     Screen = "branch-input"
	ScreenProgress        Screen = "progress"
	ScreenDone            Screen = "done"
	ScreenError           Screen = "error"
)

// view is one screen of the launcher.
type view interface {
	Update(tea.Msg) tea.Cmd
	View() string
	Destroy()
	SetWidth(int)
}

type (
	credentialSavedMsg struct {
		token    int
		location credential.Location
		err      error
	}
	issueSearchDueMsg struct {
		query      string
		generation int
	}
	issuesLoadedMsg struct {
		token int
		query string
		items []domain.Issue
		err   error
	}
	projectsLoadedMsg struct {
		token int
		items []domain.Project
		err   error
	}
	projectIssuesLoadedMsg struct {
		token     int
		projectID string
		items     []domain.Issue
		err       error
	}
	teamsLoadedMsg struct {
		token int
		items []domain.Team
		err   error
	}
	teamIssuesLoadedMsg struct {
		token  int
		teamID string
		items  []domain.Issue
		err    error
	}
	stageMsg struct {
		token  int
		update domain.StageUpdate
	}
	flowFinishedMsg struct {
		token  int
		result domain.FlowResult
		err    error
	}
	doneTimerMsg struct{}
)

// Launcher runs conditional onboarding, then workspace issue search ↔ optional
// project/team browsing → optional repository selection → progress → done/error in
// one terminal session.
type Launcher struct {
	deps LauncherDeps
	Send func(tea.Msg)

	screen  Screen
	current view

	credentialPicker *CredentialPicker
	issuePicker      *IssuePicker
	projectPicker    *ProjectPicker
	teamPicker       *TeamPicker
	repoPicker       *RepoPicker
	branchPicker     *BranchPicker
	branchInput      *BranchInput

	currentIssue          *domain.Issue
	currentRepo           *domain.Repo
	issueSource           issueSource
	returnSource          issueSource
	repoSource            issueSource
	searchQuery           string
	searchItems           []domain.Issue
	searchResultsQuery    string
	searchResultsLoaded   bool
	projectQuery          string
	projectItems          []domain.Project
	projectsLoaded        bool
	projectIssueItems     []domain.Issue
	projectIssueQuery     string
	projectIssuesLoadedID string
	teamQuery             string
	teamItems             []domain.Team
	teamsLoaded           bool
	teamIssueItems        []domain.Issue
	teamIssueQuery        string
	teamIssuesLoadedID    string
	retryAction           func() tea.Cmd
	progress              *ProgressView
	doneResult            *domain.FlowResult

	loadToken  int
	cancelLoad context.CancelFunc

	flowToken   int
	flowCancel  context.CancelFunc
	flowRunning bool
	flowAborted bool

	settled bool
	outcome LauncherOutcome
	err     error
	pending []tea.Cmd
	width   int
	height  int
}

func NewLauncher(deps LauncherDeps) *Launcher {
	return &Launcher{deps: deps, screen: ScreenIssues, Send: func(tea.Msg) {}}
}

func (m *Launcher) Outcome() LauncherOutcome { return m.outcome }
func (m *Launcher) Err() error               { return m.err }
func (m *Launcher) Settled() bool            { return m.settled }

func (m *Launcher) Init() tea.Cmd {
	if m.deps.Credential != nil {
		return m.openCredential()
	}
	if m.deps.NeedsRepoRoot && m.deps.PreselectRepo == nil {
		return m.openRoot(m.openIssues)
	}
	return m.openIssues()
}

func (m *Launcher) View() string {
	if m.current == nil {
		return ""
	}
	return m.current.View()
}

func (m *Launcher) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = typed.Width, typed.Height
		if m.current != nil {
			m.current.SetWidth(typed.Width)
		}
		return m, nil
	case tea.KeyMsg:
		if consumed, cmd := m.onKey(typed); consumed {
			return m, m.batch(cmd)
		}
	case credentialSavedMsg:
		return m, m.batch(m.onCredentialSaved(typed))
	case issueSearchDueMsg:
		return m, m.batch(m.onIssueSearchDue(typed))
	case issuesLoadedMsg:
		return m, m.batch(m.onIssuesLoaded(typed))
	case projectsLoadedMsg:
		return m, m.batch(m.onProjectsLoaded(typed))
	case projectIssuesLoadedMsg:
		return m, m.batch(m.onProjectIssuesLoaded(typed))
	case teamsLoadedMsg:
		return m, m.batch(m.onTeamsLoaded(typed))
	case teamIssuesLoadedMsg:
		return m, m.batch(m.onTeamIssuesLoaded(typed))
	case branchResolvedMsg:
		return m, m.batch(m.onBranchResolved(typed))
	case branchChosenMsg:
		return m, m.batch(m.onBranchChosen(typed))
	case stageMsg:
		return m, m.batch(m.onStage(typed))
	case flowFinishedMsg:
		return m, m.batch(m.onFlowFinished(typed))
	case doneTimerMsg:
		if m.screen == ScreenDone && m.doneResult != nil {
			m.settle(LauncherOutcome{Result: m.doneResult})
		}
		return m, m.batch(nil)
	}
	if m.settled || m.current == nil {
		return m, m.batch(nil)
	}
	current := m.current
	return m, m.batch(current.Update(msg))
}

func (m *Launcher) enqueue(cmd tea.Cmd) {
	if cmd != nil {
		m.pending = append(m.pending, cmd)
	}
}

func (m *Launcher) batch(cmd tea.Cmd) tea.Cmd {
	cmds := m.pending
	m.pending = nil
	if cmd != nil {
		cmds = append(cmds, cmd)
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// show swaps the screen. Picker pointers are cleared so a late callback cannot
// mutate a screen that is no longer visible.
func (m *Launcher) show(next view) {
	if m.current != nil {
		m.current.Destroy()
	}
	m.credentialPicker = nil
	m.issuePicker = nil
	m.projectPicker = nil
	m.teamPicker = nil
	m.repoPicker = nil
	m.branchPicker = nil
	m.branchInput = nil
	m.current = next
	if m.width > 0 {
		next.SetWidth(m.width)
	}
}

func (m *Launcher) beginLoad() (context.Context, int) {
	if m.cancelLoad != nil {
		m.cancelLoad()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancelLoad = cancel
	m.loadToken++
	return ctx, m.loadToken
}

func (m *Launcher) abortLoad() {
	if m.cancelLoad != nil {
		m.cancelLoad()
		m.cancelLoad = nil
	}
	m.loadToken++
}

func (m *Launcher) settle(outcome LauncherOutcome) {
	if m.settled {
		return
	}
	m.settled = true
	m.outcome = outcome
	m.enqueue(tea.Quit)
}

func (m *Launcher) fail(err error) {
	if m.settled {
		return
	}
	m.settled = true
	m.err = err
	m.enqueue(tea.Quit)
}

func (m *Launcher) handleFailure(err error, retry func() tea.Cmd) {
	if e, ok := lwerr.As(err); ok {
		m.showError(e, retry)
		return
	}
	m.fail(err)
}

func (m *Launcher) showError(err *lwerr.Error, retry func() tea.Cmd) {
	m.screen = ScreenError
	m.retryAction = nil
	if err.Kind == lwerr.LinearUnavailable || err.Kind == lwerr.ProviderUnavailable {
		m.retryAction = retry
	}
	m.show(NewErrorView(ErrorViewOptions{Error: err, Retryable: m.retryAction != nil}))
}
