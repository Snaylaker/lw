package cli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/snaylaker/lw/internal/config"
	"github.com/snaylaker/lw/internal/credential"
	"github.com/snaylaker/lw/internal/domain"
	"github.com/snaylaker/lw/internal/gitrepo"
	"github.com/snaylaker/lw/internal/tui"
	"github.com/snaylaker/lw/internal/worktree"
	issueprovider "github.com/snaylaker/lw/provider"
)

const issueResponse = `{"data":{"issues":{"nodes":[{"id":"issue-ENG-3971","identifier":"ENG-3971","title":"Improve command completion output","url":"https://linear.app/acme/issue/ENG-3971","state":{"name":"In Progress","type":"started"},"team":{"id":"team-eng","key":"ENG","name":"Engineering"},"project":{"id":"project-tools","name":"Billing"}}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}`

// --- step 1: the repository, before anything else ----------------------------

// SPEC §4: --repo is validated immediately, before anything can touch the
// network, so a bad path fails instantly rather than after a round trip.
func TestRunValidatesTheRepoFlagBeforeAnyNetworkOrCredential(t *testing.T) {
	h := newHarness(t).withKey("lin_api_key")
	notARepo := realPath(t, t.TempDir())
	h.credential.stdout = "lin_api_from_command\n"
	h.writeConfig(map[string]any{"credentialCommand": "print-the-key"})

	code := h.run("--repo", notARepo)

	if code != 1 {
		t.Fatalf("code = %d, want 1 (stderr %q)", code, h.stderr.String())
	}
	// SPEC §4's literals, out of the real resolver and through the real reporter.
	want := "error: not inside a git repository\n" +
		"next: run lw from inside a repository, or pass --repo <path>\n"
	if h.stderr.String() != want {
		t.Errorf("stderr = %q, want %q", h.stderr.String(), want)
	}
	if h.http.requests() != 0 {
		t.Errorf("%d Linear requests were made before the repository was resolved", h.http.requests())
	}
	if h.credential.calls != 0 {
		t.Errorf("credentialCommand ran %d times before the repository was resolved", h.credential.calls)
	}
	if h.launched != nil {
		t.Error("the launcher opened for an invocation that could not work")
	}
}

// SPEC §4: --repo names the repository outright, so the repo picker is skipped.
func TestRunRepoFlagPreselectsTheRepositoryAndSkipsThePicker(t *testing.T) {
	h := newHarness(t).withKey("lin_api_key")
	h.writeConfig(map[string]any{})
	other := newRepo(t)
	h.picks(testIssue("ENG-1"))

	if code := h.run("--repo", other); code != 0 {
		t.Fatalf("code = %d (stderr %q)", code, h.stderr.String())
	}
	if h.launched.PreselectRepo == nil {
		t.Fatal("--repo did not preselect a repository, so the picker would open")
	}
	if h.launched.PreselectRepo.Root != other {
		t.Errorf("preselected repo = %q, want the --repo value %q", h.launched.PreselectRepo.Root, other)
	}
	// The worktree is cut from the named repository, not the current directory.
	if got := readFile(t, filepath.Join(h.worktreeForRepo(other, "ENG-1"), "README.md")); got == "" {
		t.Error("the worktree was not created under the --repo repository")
	}
}

// Not standing in a repository is no longer an error: the run asks which one to
// use, so the launcher opens and the repo picker does the work.
func TestRunOutsideARepositoryOpensTheLauncherInsteadOfFailing(t *testing.T) {
	h := newHarness(t).withKey("lin_api_key")
	h.writeConfig(map[string]any{})
	h.dir = realPath(t, t.TempDir())
	h.launch = func(deps tui.LauncherDeps) (tui.LauncherOutcome, error) {
		if deps.ListRepos == nil {
			t.Error("the launcher was given no way to list repositories")
		}
		if deps.Repo.Root != "" {
			t.Errorf("deps.Repo = %+v, want the zero value outside a repository", deps.Repo)
		}
		return tui.LauncherOutcome{Cancelled: true}, nil
	}

	if code := h.run(); code != 130 {
		t.Fatalf("code = %d, want 130 (stderr %q)", code, h.stderr.String())
	}
	if h.launched == nil {
		t.Error("the launcher never opened outside a repository")
	}
}

func TestRunReportsMissingGitInsteadOfPretendingTheDirectoryIsWrong(t *testing.T) {
	h := newHarness(t).withKey("lin_api_key")
	h.writeConfig(map[string]any{})
	h.gitRun = func(context.Context, string, string, []string) (gitrepo.ExecResult, error) {
		return gitrepo.ExecResult{}, exec.ErrNotFound
	}

	if code := h.run("--issue", "ENG-1"); code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	want := "error: git could not be run\nnext: install git and make sure it is on PATH\n"
	if h.stderr.String() != want {
		t.Errorf("stderr = %q, want %q", h.stderr.String(), want)
	}
	if h.http.requests() != 0 || h.credential.calls != 0 {
		t.Error("missing git was discovered after credential or network work")
	}
}

func TestRunCanSetARepoRootWithoutLeavingTheLauncher(t *testing.T) {
	h := newHarness(t).withKey("lin_api_key")
	h.dir = realPath(t, t.TempDir())
	h.writeConfig(map[string]any{})
	h.launch = func(deps tui.LauncherDeps) (tui.LauncherOutcome, error) {
		if !deps.NeedsRepoRoot || deps.SuggestedRepoRoot != h.dir {
			t.Fatalf("root setup = %v, %q", deps.NeedsRepoRoot, deps.SuggestedRepoRoot)
		}
		if repos := deps.ListRepos(); len(repos) != 0 {
			t.Fatalf("initial repos = %+v, want none", repos)
		}
		repos, err := deps.SetRepoRoot(h.repo)
		if err != nil {
			t.Fatalf("set root: %v", err)
		}
		if len(repos) != 1 || repos[0].Repo.Root != h.repo {
			t.Fatalf("repos = %+v, want the entered repository", repos)
		}
		return tui.LauncherOutcome{Cancelled: true}, nil
	}

	if code := h.run(); code != 130 {
		t.Fatalf("code = %d, want 130", code)
	}
	stored, err := config.ReadStoredConfig(h.configPath())
	if err != nil {
		t.Fatal(err)
	}
	roots := config.RepoRoots(stored, h.env)
	if len(roots) != 1 || roots[0] != filepath.Dir(h.repo) {
		t.Errorf("stored roots = %v", roots)
	}
}

func TestRepoListDropsAStaleRecentPath(t *testing.T) {
	h := newHarness(t).withKey("lin_api_key")
	h.dir = realPath(t, t.TempDir())
	h.writeConfig(map[string]any{
		"repos": map[string]any{
			"recent": []map[string]any{{"path": filepath.Join(t.TempDir(), "gone"), "usedAt": 1}},
		},
	})
	h.launch = func(deps tui.LauncherDeps) (tui.LauncherOutcome, error) {
		if repos := deps.ListRepos(); len(repos) != 0 {
			t.Errorf("repos = %+v, want the stale entry dropped", repos)
		}
		return tui.LauncherOutcome{Cancelled: true}, nil
	}

	if code := h.run(); code != 130 {
		t.Fatalf("code = %d, want 130", code)
	}
}

// SPEC §4: running lw from inside a worktree it created must cut the next
// worktree from the main checkout, not from the linked one.
func TestRunResolvesALinkedWorktreeToItsMainCheckout(t *testing.T) {
	h := newHarness(t).withKey("lin_api_key")
	h.writeConfig(map[string]any{})

	linked := filepath.Join(realPath(t, t.TempDir()), "ENG-1")
	git(t, h.repo, "worktree", "add", "-b", "ENG-1", linked)

	main := h.repo
	// The run now starts from inside the linked worktree.
	h.repo = realPath(t, linked)
	h.picks(testIssue("ENG-2"))

	if code := h.run(); code != 0 {
		t.Fatalf("code = %d (stderr %q)", code, h.stderr.String())
	}
	if h.launched.Repo.Root != main {
		t.Errorf("repo root = %q, want the main checkout %q", h.launched.Repo.Root, main)
	}
	// And the new worktree is a sibling of the first, cut from the main repo.
	created := filepath.Join(h.worktreeRoot, filepath.Base(main), "ENG-2")
	if _, err := os.Stat(filepath.Join(created, "README.md")); err != nil {
		t.Errorf("the worktree was not created at %s: %v", created, err)
	}
}

// --- step 2: the credential --------------------------------------------------

func TestRunStartsCredentialOnboardingWithoutTouchingTheNetwork(t *testing.T) {
	h := newHarness(t) // no saved key, environment key or credentialCommand
	h.writeConfig(map[string]any{})
	h.launch = func(deps tui.LauncherDeps) (tui.LauncherOutcome, error) {
		if deps.Credential == nil || deps.Credential.Save == nil {
			t.Fatal("launcher was not given credential onboarding")
		}
		return tui.LauncherOutcome{Cancelled: true}, nil
	}

	if code := h.run(); code != 130 {
		t.Fatalf("code = %d, want 130", code)
	}
	if h.http.requests() != 0 {
		t.Errorf("%d Linear requests were made before a key was submitted", h.http.requests())
	}
}

func TestCredentialOnboardingValidatesSavesAndContinues(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(map[string]any{"repos": map[string]any{"roots": []string{filepath.Dir(h.repo)}}})
	h.http.response = `{"data":{"viewer":{"id":"user-1"}}}`
	h.launch = func(deps tui.LauncherDeps) (tui.LauncherOutcome, error) {
		if deps.Credential == nil {
			t.Fatal("credential onboarding was skipped")
		}
		result, err := deps.Credential.Save(context.Background(), "lin_api_pasted", credential.StoreKeyring)
		if err != nil {
			t.Fatalf("set credential: %v", err)
		}
		if result.Store() != credential.StoreKeyring {
			t.Errorf("saved location = %+v", result)
		}
		return tui.LauncherOutcome{Cancelled: true}, nil
	}

	if code := h.run(); code != 130 {
		t.Fatalf("code = %d", code)
	}
	if h.vault.key != "lin_api_pasted" {
		t.Errorf("saved key = %q", h.vault.key)
	}
	if h.http.requests() != 1 {
		t.Errorf("validation requests = %d, want 1", h.http.requests())
	}
}

func TestCredentialOnboardingRequiresFileConsentWithoutRevalidating(t *testing.T) {
	h := newHarness(t)
	h.vault.keyringUnavailable = true
	h.writeConfig(map[string]any{"repos": map[string]any{"roots": []string{filepath.Dir(h.repo)}}})
	h.http.response = `{"data":{"viewer":{"id":"user-1"}}}`
	h.launch = func(deps tui.LauncherDeps) (tui.LauncherOutcome, error) {
		_, err := deps.Credential.Save(context.Background(), "lin_api_pasted", credential.StoreKeyring)
		if !errors.Is(err, credential.ErrKeyringUnavailable) {
			t.Fatalf("keyring save = %v", err)
		}
		if deps.Credential.File != filepath.Join(h.configDir, "credentials") {
			t.Fatalf("credential file = %q", deps.Credential.File)
		}
		second, err := deps.Credential.Save(context.Background(), "lin_api_pasted", credential.StoreFile)
		if err != nil {
			t.Fatal(err)
		}
		if second.Store() != credential.StoreFile {
			t.Errorf("file location = %+v", second)
		}
		return tui.LauncherOutcome{Cancelled: true}, nil
	}

	if code := h.run(); code != 130 {
		t.Fatalf("code = %d", code)
	}
	if h.http.requests() != 1 {
		t.Errorf("validation requests = %d, want 1", h.http.requests())
	}
	if !reflect.DeepEqual(h.vault.saveCalls, []credential.Store{credential.StoreKeyring, credential.StoreFile}) {
		t.Errorf("save calls = %v", h.vault.saveCalls)
	}
}

func TestCredentialFileSaveCannotBypassValidation(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(map[string]any{"repos": map[string]any{"roots": []string{filepath.Dir(h.repo)}}})
	h.http.status = 401
	h.launch = func(deps tui.LauncherDeps) (tui.LauncherOutcome, error) {
		if _, err := deps.Credential.Save(context.Background(), "unvalidated", credential.StoreFile); err == nil {
			t.Fatal("direct file save accepted an unvalidated credential")
		}
		return tui.LauncherOutcome{Cancelled: true}, nil
	}

	if code := h.run(); code != 130 {
		t.Fatalf("code = %d", code)
	}
	if h.http.requests() != 1 {
		t.Errorf("validation requests = %d, want 1", h.http.requests())
	}
	if len(h.vault.saveCalls) != 0 {
		t.Errorf("vault save calls = %v, want none", h.vault.saveCalls)
	}
}

func TestRunReadsTheKeyFromCredentialCommandBeforeTheEnvironment(t *testing.T) {
	h := newHarness(t).withKey("from_the_environment")
	h.credential.stdout = "from_the_command\n"
	h.writeConfig(map[string]any{"credentialCommand": "print-the-key"})
	h.picks(testIssue("ENG-1"))

	if code := h.run(); code != 0 {
		t.Fatalf("code = %d (stderr %q)", code, h.stderr.String())
	}
	if h.credential.calls != 1 {
		t.Fatalf("credentialCommand ran %d times, want 1", h.credential.calls)
	}
	args := h.credential.args[0]
	if args[len(args)-1] != "print-the-key" {
		t.Errorf("the shell was given %q, want the configured command last", args)
	}
}

// --- steps 4 and 5 ------------------------------------------------------------

func TestRunOpensTheWorktreeWritesItsMetadataAndPrintsThePath(t *testing.T) {
	h := newHarness(t).withKey("lin_api_key")
	h.writeConfig(map[string]any{})
	issue := testIssue("ENG-3971")
	h.picks(issue)

	if code := h.run(); code != 0 {
		t.Fatalf("code = %d (stderr %q)", code, h.stderr.String())
	}

	path := h.worktreeFor("ENG-3971")
	if _, err := os.Stat(filepath.Join(path, "README.md")); err != nil {
		t.Fatalf("the worktree is not a checkout: %v", err)
	}
	metadata, err := worktree.ReadMetadata(context.Background(), path, nil)
	if err != nil || metadata == nil {
		t.Fatalf("metadata = %v, %v", metadata, err)
	}
	if metadata.Identifier != "ENG-3971" || metadata.URL != issue.URL || metadata.Team != "ENG" {
		t.Errorf("metadata = %+v", metadata)
	}
	// The output contract: stdout is exactly the path and nothing else, so
	// `cd $(lw)` works.
	if got := h.stdout.String(); got != path+"\n" {
		t.Errorf("stdout = %q, want exactly %q", got, path+"\n")
	}
}

// --- the launcher's outcomes -------------------------------------------------

func TestRunACancelledLauncherPrintsNothingAndExits130(t *testing.T) {
	h := newHarness(t).withKey("lin_api_key")
	h.writeConfig(map[string]any{})
	h.launch = func(tui.LauncherDeps) (tui.LauncherOutcome, error) {
		return tui.LauncherOutcome{Cancelled: true}, nil
	}

	code := h.run()

	if code != 130 {
		t.Errorf("code = %d, want 130", code)
	}
	if h.stdout.Len() != 0 || h.stderr.Len() != 0 {
		t.Errorf("cancellation printed: stdout %q stderr %q", h.stdout.String(), h.stderr.String())
	}
}

// A worktree created just before the abort landed is still a worktree: it is
// never rolled back, and nothing after it runs.
func TestRunCancellationLeavesAWorktreeAloneAndPrintsNothing(t *testing.T) {
	h := newHarness(t).withKey("lin_api_key")
	h.writeConfig(map[string]any{})
	h.launch = func(deps tui.LauncherDeps) (tui.LauncherOutcome, error) {
		issue := testIssue("ENG-1")
		result, err := deps.ExecuteFlow(context.Background(), h.repoStructOrDeps(deps), issue, domain.Branch{Name: issue.WorktreeKey}, nil)
		if err != nil {
			return tui.LauncherOutcome{}, err
		}
		return tui.LauncherOutcome{Result: &result, Cancelled: true}, nil
	}

	if code := h.run(); code != 130 {
		t.Fatalf("code = %d, want 130", code)
	}
	if _, err := os.Stat(filepath.Join(h.worktreeFor("ENG-1"), "README.md")); err != nil {
		t.Errorf("a cancellation removed the worktree: %v", err)
	}
	if h.stdout.Len() != 0 {
		t.Error("a cancelled run still printed a path")
	}
}

// Escape on the error view: the message was already on screen, so the caller
// exits 1 without repeating it.
func TestRunAClosedErrorViewExits1Silently(t *testing.T) {
	h := newHarness(t).withKey("lin_api_key")
	h.writeConfig(map[string]any{})
	h.launch = func(tui.LauncherDeps) (tui.LauncherOutcome, error) {
		return tui.LauncherOutcome{}, nil
	}

	code := h.run()

	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if h.stdout.Len() != 0 || h.stderr.Len() != 0 {
		t.Errorf("printed: stdout %q stderr %q", h.stdout.String(), h.stderr.String())
	}
}

// --- what the launcher is given ----------------------------------------------

func TestRunSearchesTheWorkspaceWithoutProjectOrTeamSelection(t *testing.T) {
	h := newHarness(t).withKey("lin_api_key")
	h.writeConfig(map[string]any{})
	h.http.response = `{"data":{"searchIssues":{"nodes":[{"id":"issue-1","identifier":"DEMO-4009","title":"Dynamic prompt","url":"https://linear.app/acme/issue/DEMO-4009","state":{"name":"Todo","type":"unstarted"},"team":{"id":"team-demo","key":"DEMO","name":"Developer Experience"},"project":{"id":"project-cli","name":"CLI Reliability"}}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}`
	h.launch = func(deps tui.LauncherDeps) (tui.LauncherOutcome, error) {
		if deps.SearchIssues == nil {
			t.Fatal("launcher has no workspace issue search")
		}
		issues, err := deps.SearchIssues(context.Background(), "dynamic prompt")
		if err != nil {
			t.Fatal(err)
		}
		if len(issues) != 1 || issues[0].WorktreeKey != "DEMO-4009" {
			t.Fatalf("issues = %+v", issues)
		}
		project, hasProject := issues[0].Scope("linear_project")
		team, hasTeam := issues[0].Scope("linear_team")
		if !hasProject || project.ID != "project-cli" || !hasTeam || team.ID != "team-demo" {
			t.Fatalf("issue routing metadata = %+v", issues[0])
		}
		return tui.LauncherOutcome{Cancelled: true}, nil
	}

	if code := h.run(); code != 130 {
		t.Fatalf("code = %d, stderr = %q", code, h.stderr.String())
	}
	if body := h.http.bodies[0]; !strings.Contains(body, `searchIssues(`) || !strings.Contains(body, `"term":"dynamic prompt"`) {
		t.Errorf("request = %s", body)
	}
}

func TestRunUsesAndUpdatesTheRepositoryAssociatedWithAnIssue(t *testing.T) {
	h := newHarness(t).withKey("lin_api_key")
	projectRepo := newRepo(t)
	teamRepo := newRepo(t)
	h.writeConfig(map[string]any{
		"repos": map[string]any{
			"projects": []map[string]any{{"projectId": "project-cli", "path": projectRepo, "usedAt": 1}},
			"teams":    []map[string]any{{"teamId": "team-demo", "path": teamRepo, "usedAt": 1}},
		},
	})
	h.launch = func(deps tui.LauncherDeps) (tui.LauncherOutcome, error) {
		projectIssue := testIssue("DEMO-4009")
		projectIssue.Scopes = []issueprovider.Scope{
			{Kind: "linear_project", ID: "project-cli"},
			{Kind: "linear_team", ID: "team-demo"},
		}
		repo, ok := deps.RepoForIssue(projectIssue)
		if !ok || repo.Root != projectRepo {
			t.Fatalf("project repo = %+v, %v", repo, ok)
		}

		projectless := testIssue("DEMO-4007")
		projectless.Scopes = []issueprovider.Scope{{Kind: "linear_team", ID: "team-demo"}}
		repo, ok = deps.RepoForIssue(projectless)
		if !ok || repo.Root != teamRepo {
			t.Fatalf("team repo = %+v, %v", repo, ok)
		}

		deps.RecordRepoUse(projectless, domain.Repo{Root: projectRepo, Name: filepath.Base(projectRepo)})
		repo, ok = deps.RepoForIssue(projectless)
		if !ok || repo.Root != projectRepo {
			t.Fatalf("updated team repo = %+v, %v", repo, ok)
		}
		return tui.LauncherOutcome{Cancelled: true}, nil
	}

	if code := h.run(); code != 130 {
		t.Fatalf("code = %d", code)
	}
}

func TestRunIssueFlagResolvesTheIssueDirectlyAndSkipsBothPickers(t *testing.T) {
	h := newHarness(t).withKey("lin_api_key")
	h.writeConfig(map[string]any{})
	h.http.response = issueResponse

	code := h.run("--issue", "ENG-3971", "--branch", "ENG-3971")

	if code != 0 {
		t.Fatalf("code = %d (stderr %q)", code, h.stderr.String())
	}
	if h.launched != nil {
		t.Error("--issue still opened the launcher")
	}
	if h.http.requests() != 1 {
		t.Errorf("%d requests, want the one issue lookup", h.http.requests())
	}
	metadata, err := worktree.ReadMetadata(context.Background(), h.worktreeFor("ENG-3971"), nil)
	if err != nil || metadata == nil {
		t.Fatalf("metadata = %v, %v", metadata, err)
	}
	if metadata.Title != "Improve command completion output" {
		t.Errorf("metadata = %+v", metadata)
	}
}

func TestDirectModeRequiresANameBeforeCreatingANewBranch(t *testing.T) {
	h := newHarness(t).withKey("lin_api_key")
	h.writeConfig(map[string]any{})
	h.http.response = issueResponse

	if code := h.run("--issue", "ENG-3971"); code != 1 {
		t.Fatalf("code = %d, stderr = %q", code, h.stderr.String())
	}
	want := "error: no existing branch matches ENG-3971\n" +
		"next: re-run with --branch <name>, or configure branchNaming for this repository\n"
	if h.stderr.String() != want {
		t.Fatalf("stderr = %q, want %q", h.stderr.String(), want)
	}
}

func TestDirectModeExpandsARepositoryBranchTemplate(t *testing.T) {
	h := newHarness(t).withKey("lin_api_key")
	h.writeConfig(map[string]any{
		"branchNaming": map[string]any{
			"variables": map[string]any{"username": "mehdi"},
			"byRepository": map[string]any{
				h.repo: map[string]any{"template": "{username}/{ticket_lower}-{slug}"},
			},
		},
	})
	h.http.response = issueResponse

	if code := h.run("--issue", "ENG-3971"); code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, h.stderr.String())
	}
	if got := git(t, h.worktreeFor("ENG-3971"), "rev-parse", "--abbrev-ref", "HEAD"); got != "mehdi/eng-3971-improve-command-completion-output" {
		t.Fatalf("branch = %q", got)
	}
}

func TestDirectModeReusesOneMatchingExistingBranch(t *testing.T) {
	h := newHarness(t).withKey("lin_api_key")
	h.writeConfig(map[string]any{})
	h.http.response = issueResponse
	git(t, h.repo, "branch", "mehdi/eng-3971-started")

	if code := h.run("--issue", "ENG-3971"); code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, h.stderr.String())
	}
	if got := git(t, h.worktreeFor("ENG-3971"), "rev-parse", "--abbrev-ref", "HEAD"); got != "mehdi/eng-3971-started" {
		t.Fatalf("branch = %q", got)
	}
}

func TestRunIssueFlagInfersARepositoryOutsideGit(t *testing.T) {
	cases := []struct {
		name     string
		response string
		repos    map[string]any
	}{
		{
			name:     "project association",
			response: issueResponse,
			repos: map[string]any{"projects": []map[string]any{{
				"projectId": "project-tools", "path": "REPO", "usedAt": 1,
			}}},
		},
		{
			name:     "projectless team association",
			response: `{"data":{"issues":{"nodes":[{"id":"issue-ENG-3971","identifier":"ENG-3971","title":"Projectless","url":"https://linear.app/acme/issue/ENG-3971","state":{"name":"Todo","type":"unstarted"},"team":{"id":"team-eng","key":"ENG","name":"Engineering"},"project":null}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}`,
			repos: map[string]any{"teams": []map[string]any{{
				"teamId": "team-eng", "path": "REPO", "usedAt": 1,
			}}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t).withKey("lin_api_key")
			h.dir = realPath(t, t.TempDir())
			for _, key := range []string{"projects", "teams"} {
				if rows, ok := tc.repos[key].([]map[string]any); ok {
					for _, row := range rows {
						row["path"] = h.repo
					}
				}
			}
			h.writeConfig(map[string]any{"repos": tc.repos})
			h.http.response = tc.response

			if code := h.run("--issue", "ENG-3971", "--branch", "ENG-3971"); code != 0 {
				t.Fatalf("code = %d, stderr = %q", code, h.stderr.String())
			}
			if h.stdout.String() != h.worktreeFor("ENG-3971")+"\n" {
				t.Errorf("stdout = %q", h.stdout.String())
			}
		})
	}
}

type customIssueProvider struct{}

func (customIssueProvider) ID() issueprovider.ID { return "tickets" }
func (customIssueProvider) DisplayName() string  { return "Tickets" }
func (customIssueProvider) ValidateReference(reference string) error {
	if reference != "T-42" {
		return errors.New("expected T-42")
	}
	return nil
}
func (customIssueProvider) Resolve(context.Context, string) (issueprovider.WorkItem, error) {
	return issueprovider.WorkItem{
		Provider: "tickets", ExternalID: "42", Reference: "T-42", WorktreeKey: "T-42",
		Title: "Custom provider issue", URL: "https://tickets.example.com/T-42", BranchKeys: []string{"T-42"},
	}, nil
}
func (customIssueProvider) Search(context.Context, string) ([]issueprovider.WorkItem, error) {
	return nil, nil
}

func TestCompileTimeProviderExtensionUsesTheSameFlow(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(map[string]any{"worktreeRoot": h.worktreeRoot, "issueProvider": "tickets"})
	h.providers = []issueprovider.Provider{customIssueProvider{}}

	code := h.run("--issue", "T-42", "--branch", "alex/t-42-custom")
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, h.stderr.String())
	}
	metadata, err := worktree.ReadMetadata(context.Background(), h.worktreeFor("T-42"), nil)
	if err != nil || metadata == nil || metadata.Provider != "tickets" {
		t.Fatalf("metadata = %+v, %v", metadata, err)
	}

	h.stdout.Reset()
	h.stderr.Reset()
	h.env[providerEnvVar] = "tickets"
	if code := h.run("doctor"); code != 0 || !strings.Contains(h.stdout.String(), "Tickets provider: available as a compiled extension") {
		t.Fatalf("doctor code = %d, stdout = %q, stderr = %q", code, h.stdout.String(), h.stderr.String())
	}
}

func TestInteractiveGitHubSearchUsesTheProviderWithoutLinearOnboarding(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(map[string]any{})
	h.http.response = `{"items":[{"id":42,"node_id":"I_kw42","number":42,"title":"Repair cache invalidation","html_url":"https://github.com/acme/api/issues/42","state":"open","repository_url":"https://api.github.com/repos/acme/api"}]}`
	h.launch = func(deps tui.LauncherDeps) (tui.LauncherOutcome, error) {
		if deps.ProviderName != "GitHub" || deps.BrowseCollections || deps.Credential != nil {
			t.Fatalf("provider launcher deps = %+v", deps)
		}
		issues, err := deps.SearchIssues(context.Background(), "cache")
		if err != nil {
			t.Fatal(err)
		}
		if len(issues) != 1 || issues[0].DisplayReference() != "acme/api#42" {
			t.Fatalf("issues = %+v", issues)
		}
		return tui.LauncherOutcome{Cancelled: true}, nil
	}

	if code := h.run("--provider", "github"); code != 130 {
		t.Fatalf("code = %d, stderr = %q", code, h.stderr.String())
	}
	if h.credential.calls != 0 {
		t.Fatal("GitHub search attempted Linear credential resolution")
	}
}

func TestDirectGitHubIssueUsesTheSameWorktreeFlow(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(map[string]any{"worktreeRoot": h.worktreeRoot})
	h.env["GITHUB_TOKEN"] = "github_test"
	h.http.response = `{"id":42,"node_id":"I_kw42","number":42,"title":"Repair cache invalidation","html_url":"https://github.com/acme/api/issues/42","state":"open"}`

	code := h.run("--issue", "github:acme/api#42", "--branch", "alex/gh-42-cache")
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, h.stderr.String())
	}
	wantPath := h.worktreeFor("GH-acme-api-42")
	if h.stdout.String() != wantPath+"\n" {
		t.Fatalf("stdout = %q, want %q", h.stdout.String(), wantPath+"\n")
	}
	metadata, err := worktree.ReadMetadata(context.Background(), wantPath, nil)
	if err != nil || metadata == nil {
		t.Fatalf("metadata = %+v, %v", metadata, err)
	}
	if metadata.Provider != "github" || metadata.Reference != "acme/api#42" || metadata.Branch != "alex/gh-42-cache" {
		t.Fatalf("metadata = %+v", metadata)
	}
}

func TestDirectJiraIssueUsesTheSameWorktreeFlow(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(map[string]any{"worktreeRoot": h.worktreeRoot})
	h.env["JIRA_BASE_URL"] = "https://jira.example.com"
	h.env["JIRA_EMAIL"] = "alex@example.com"
	h.env["JIRA_API_TOKEN"] = "jira_test"
	h.http.response = `{"id":"10042","key":"OPS-42","fields":{"summary":"Repair cache invalidation","status":{"name":"In Progress","statusCategory":{"key":"indeterminate"}},"project":{"id":"10000","key":"OPS","name":"Operations"}}}`

	code := h.run("--provider", "jira", "--issue", "OPS-42", "--branch", "alex/ops-42-cache")
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, h.stderr.String())
	}
	wantPath := h.worktreeFor("OPS-42")
	if h.stdout.String() != wantPath+"\n" {
		t.Fatalf("stdout = %q, want %q", h.stdout.String(), wantPath+"\n")
	}
	metadata, err := worktree.ReadMetadata(context.Background(), wantPath, nil)
	if err != nil || metadata == nil || metadata.Provider != "jira" || metadata.Reference != "OPS-42" {
		t.Fatalf("metadata = %+v, %v", metadata, err)
	}
}

func TestRunAMalformedIssueIsAUsageErrorBeforeAnythingIsRead(t *testing.T) {
	h := newHarness(t)
	h.repo = realPath(t, t.TempDir()) // not even a repository: the flag loses first

	code := h.run("--issue", "not-an-identifier-3971-x")

	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	want := `error: --issue takes an identifier like ENG-3971, not "not-an-identifier-3971-x"` + "\n\n" + HelpText()
	if h.stderr.String() != want {
		t.Errorf("stderr = %q, want %q", h.stderr.String(), want)
	}
	if h.http.requests() != 0 {
		t.Error("a malformed --issue reached the network")
	}
}

// --- helpers -----------------------------------------------------------------

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

// Automatic pruning remains tied to the repository selected for the issue.
func TestRunPrunesFinishedWorktreesWhenPruneMergedIsConfigured(t *testing.T) {
	h := newHarness(t).withKey("lin_api_key")
	h.writeConfig(map[string]any{"pruneMerged": true})
	finished := mergedWorktree(t, h, "ENG-4000")

	h.picks(testIssue("ENG-3971"))
	if code := h.run(); code != 0 {
		t.Fatalf("code = %d (stderr %q)", code, h.stderr.String())
	}

	if _, err := os.Stat(finished); !os.IsNotExist(err) {
		t.Errorf("a run with pruneMerged set left the finished worktree behind (err = %v)", err)
	}
	// The run's own worktree is untouched, and stdout still carries only its path.
	opened := h.worktreeFor("ENG-3971")
	if _, err := os.Stat(opened); err != nil {
		t.Fatalf("the run's own worktree was pruned: %v", err)
	}
	if h.stdout.String() != opened+"\n" {
		t.Errorf("stdout = %q, want exactly the worktree path", h.stdout.String())
	}
}

// And with it unset, a run deletes nothing.
func TestAutomaticPruneTargetsTheChosenRepositoryNotTheCurrentOne(t *testing.T) {
	h := newHarness(t).withKey("lin_api_key")
	h.writeConfig(map[string]any{"pruneMerged": true})
	currentFinished := mergedWorktree(t, h, "ENG-4000")
	chosen := newRepo(t)
	h.launch = func(deps tui.LauncherDeps) (tui.LauncherOutcome, error) {
		issue := testIssue("ENG-3971")
		result, err := deps.ExecuteFlow(context.Background(), domain.Repo{Root: chosen, Name: filepath.Base(chosen)}, issue, domain.Branch{Name: issue.WorktreeKey}, nil)
		return tui.LauncherOutcome{Result: &result}, err
	}

	if code := h.run(); code != 0 {
		t.Fatalf("code = %d (stderr %q)", code, h.stderr.String())
	}
	if _, err := os.Stat(currentFinished); err != nil {
		t.Fatalf("automatic pruning touched the current repo instead of the chosen one: %v", err)
	}
}

func TestRunLeavesFinishedWorktreesAloneByDefault(t *testing.T) {
	h := newHarness(t).withKey("lin_api_key")
	h.writeConfig(map[string]any{})
	finished := mergedWorktree(t, h, "ENG-4000")

	h.picks(testIssue("ENG-3971"))
	if code := h.run(); code != 0 {
		t.Fatalf("code = %d (stderr %q)", code, h.stderr.String())
	}
	if _, err := os.Stat(finished); err != nil {
		t.Fatalf("a run deleted a worktree with pruneMerged unset: %v", err)
	}
}
