package gitrepo

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/snaylaker/lw/internal/domain"
	"github.com/snaylaker/lw/internal/lwerr"
)

// requireGit keeps the suite honest on a machine without git rather than
// failing tests that are about this package.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

// runGit never reads the developer's own git configuration, so a test cannot
// depend on the machine it runs on.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"HOME="+dir,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func initRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "init", "--quiet", dir)
}

func commitEmpty(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "--allow-empty", "-m", "init", "--quiet")
}

// tempDir resolves symlinks because git reports the real path of a toplevel and
// macOS hands out /var/folders paths that live under /private.
func tempDir(t *testing.T) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestGitChildrenNeverInheritProviderSecrets(t *testing.T) {
	env := withoutProviderSecrets([]string{
		"PATH=/bin", "LINEAR_API_KEY=secret", "linear_api_key=also-secret",
		"GITHUB_TOKEN=secret", "gh_token=secret", "JIRA_API_TOKEN=secret", "HOME=/tmp",
	})
	if got, want := strings.Join(env, "|"), "PATH=/bin|HOME=/tmp"; got != want {
		t.Errorf("environment = %q, want %q", got, want)
	}
}

func TestResolveFindsToplevelFromASubdirectory(t *testing.T) {
	requireGit(t)
	dir := filepath.Join(tempDir(t), "acme-api")
	initRepo(t, dir)
	commitEmpty(t, dir)
	sub := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	repo, err := Resolve(context.Background(), sub, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if repo.Root != dir {
		t.Errorf("root = %q, want %q", repo.Root, dir)
	}
	if repo.Name != "acme-api" {
		t.Errorf("name = %q, want %q", repo.Name, "acme-api")
	}
}

func TestResolveRejectsAPlainDirectoryWithANextAction(t *testing.T) {
	requireGit(t)
	dir := tempDir(t)

	_, err := Resolve(context.Background(), dir, nil)
	launcherErr, ok := lwerr.As(err)
	if !ok {
		t.Fatalf("err = %v, want *lwerr.Error", err)
	}
	if launcherErr.Kind != lwerr.NotARepo {
		t.Errorf("kind = %q, want %q", launcherErr.Kind, lwerr.NotARepo)
	}
	if !strings.Contains(launcherErr.NextAction, "--repo") {
		t.Errorf("next action = %q, want it to mention --repo", launcherErr.NextAction)
	}
}

func TestResolveRejectsANonexistentDirectory(t *testing.T) {
	requireGit(t)

	_, err := Resolve(context.Background(), "/definitely/not/a/real/dir", nil)
	if !lwerr.Is(err, lwerr.NotARepo) {
		t.Fatalf("err = %v, want kind %q", err, lwerr.NotARepo)
	}
}

func TestValidateClassifiesARepoWithNoCommitsAsUnborn(t *testing.T) {
	requireGit(t)
	dir := filepath.Join(tempDir(t), "fresh")
	initRepo(t, dir)

	validation := Validate(context.Background(), dir, nil)
	if validation.Status != StatusUnbornHead {
		t.Fatalf("status = %q, want %q", validation.Status, StatusUnbornHead)
	}
	if validation.Repo.Root != dir {
		t.Errorf("root = %q, want %q", validation.Repo.Root, dir)
	}
}

func TestResolveReportsAnUnbornRepoWithItsOwnMessage(t *testing.T) {
	requireGit(t)
	dir := filepath.Join(tempDir(t), "fresh")
	initRepo(t, dir)

	_, err := Resolve(context.Background(), dir, nil)
	launcherErr, ok := lwerr.As(err)
	if !ok {
		t.Fatalf("err = %v, want *lwerr.Error", err)
	}
	if !strings.Contains(launcherErr.Message, "has no commits yet") {
		t.Errorf("message = %q, want it to mention no commits", launcherErr.Message)
	}
	if !strings.Contains(launcherErr.NextAction, "initial commit") {
		t.Errorf("next action = %q, want it to mention an initial commit", launcherErr.NextAction)
	}
}

func TestValidateClassifiesACommittedRepoAndAPlainDirectory(t *testing.T) {
	requireGit(t)
	dir := tempDir(t)
	repoDir := filepath.Join(dir, "repo")
	initRepo(t, repoDir)
	commitEmpty(t, repoDir)
	plain := filepath.Join(dir, "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}

	ok := Validate(context.Background(), repoDir, nil)
	if ok.Status != StatusOK || ok.Repo.Root != repoDir || ok.Repo.Name != "repo" {
		t.Errorf("validation = %+v, want ok for %q named repo", ok, repoDir)
	}
	none := Validate(context.Background(), plain, nil)
	if none.Status != StatusNotARepo {
		t.Errorf("status = %q, want %q", none.Status, StatusNotARepo)
	}
}

func TestLinkedWorktreeResolvesToTheMainCheckout(t *testing.T) {
	requireGit(t)
	dir := tempDir(t)
	main := filepath.Join(dir, "acme-api")
	initRepo(t, main)
	commitEmpty(t, main)
	worktree := filepath.Join(dir, "worktrees", "ENG-3971")
	if err := os.MkdirAll(filepath.Dir(worktree), 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, main, "worktree", "add", "--quiet", "-b", "ENG-3971", worktree)
	sub := filepath.Join(worktree, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, from := range []string{worktree, sub, main} {
		repo, err := Resolve(context.Background(), from, nil)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", from, err)
		}
		if repo.Root != main {
			t.Errorf("Resolve(%q) root = %q, want %q", from, repo.Root, main)
		}
		if repo.Name != "acme-api" {
			t.Errorf("Resolve(%q) name = %q, want %q", from, repo.Name, "acme-api")
		}
	}
}

func TestWorktreeOfABareRepoKeepsItsOwnToplevel(t *testing.T) {
	run := func(_ context.Context, _, _ string, args []string) (ExecResult, error) {
		switch {
		case contains(args, "--show-toplevel"):
			return ExecResult{Stdout: "/wt/ENG-1\n"}, nil
		case contains(args, "--git-common-dir"):
			return ExecResult{Stdout: "/repos/app.git/worktrees/ENG-1\n/repos/app.git\n"}, nil
		}
		return ExecResult{Stdout: "abc123\n"}, nil
	}

	validation := Validate(context.Background(), "/wt/ENG-1", run)
	if validation.Status != StatusOK || validation.Repo.Root != "/wt/ENG-1" || validation.Repo.Name != "ENG-1" {
		t.Fatalf("validation = %+v, want ok for /wt/ENG-1 named ENG-1", validation)
	}
}

func TestAFailingWorktreeProbeKeepsThePlainToplevel(t *testing.T) {
	run := func(_ context.Context, _, _ string, args []string) (ExecResult, error) {
		switch {
		case contains(args, "--show-toplevel"):
			return ExecResult{Stdout: "/plain/repo\n"}, nil
		case contains(args, "--git-common-dir"):
			return ExecResult{}, errors.New("git exploded")
		}
		return ExecResult{Stdout: "abc123\n"}, nil
	}

	validation := Validate(context.Background(), "/plain/repo", run)
	if validation.Status != StatusOK || validation.Repo.Root != "/plain/repo" || validation.Repo.Name != "repo" {
		t.Fatalf("validation = %+v, want ok for /plain/repo named repo", validation)
	}
}

// SPEC §4 quotes both failures literally, message and next action:
//
//	not a repository -> `not inside a git repository`,
//	  next: `run lw from inside a repository, or pass --repo <path>`
//	no commits -> `<name> has no commits yet`,
//	  next: `make an initial commit, then re-run`
func TestValidationErrorMapsBothFailureShapes(t *testing.T) {
	notRepo := ValidationError(Validation{Status: StatusNotARepo, Dir: "/tmp/nope"})
	if notRepo.Kind != lwerr.NotARepo {
		t.Errorf("kind = %q, want %q", notRepo.Kind, lwerr.NotARepo)
	}
	if notRepo.Message != "not inside a git repository" {
		t.Errorf("message = %q", notRepo.Message)
	}
	if notRepo.NextAction != "run lw from inside a repository, or pass --repo <path>" {
		t.Errorf("next action = %q", notRepo.NextAction)
	}

	unborn := ValidationError(Validation{
		Status: StatusUnbornHead,
		Repo:   domain.Repo{Root: "/tmp/fresh", Name: "fresh"},
	})
	if unborn.Kind != lwerr.NotARepo {
		t.Errorf("kind = %q, want %q", unborn.Kind, lwerr.NotARepo)
	}
	if unborn.Message != "fresh has no commits yet" {
		t.Errorf("message = %q", unborn.Message)
	}
	if unborn.NextAction != "make an initial commit, then re-run" {
		t.Errorf("next action = %q", unborn.NextAction)
	}
}

// The literals again, this time out of the real resolver over a real directory
// that is not a repository — the path a user actually takes.
func TestResolveOutsideARepositorySaysExactlyWhatSection4Says(t *testing.T) {
	requireGit(t)

	_, err := Resolve(context.Background(), tempDir(t), nil)
	launcherErr, ok := lwerr.As(err)
	if !ok || launcherErr.Kind != lwerr.NotARepo {
		t.Fatalf("err = %v, want a not_a_repo *lwerr.Error", err)
	}
	if launcherErr.Message != "not inside a git repository" {
		t.Errorf("message = %q", launcherErr.Message)
	}
	if launcherErr.NextAction != "run lw from inside a repository, or pass --repo <path>" {
		t.Errorf("next action = %q", launcherErr.NextAction)
	}
}

// And out of the real resolver over a real repository with no commit.
func TestResolveWithoutCommitsSaysExactlyWhatSection4Says(t *testing.T) {
	requireGit(t)
	dir := filepath.Join(tempDir(t), "acme-api")
	initRepo(t, dir)

	_, err := Resolve(context.Background(), dir, nil)
	launcherErr, ok := lwerr.As(err)
	if !ok || launcherErr.Kind != lwerr.NotARepo {
		t.Fatalf("err = %v, want a not_a_repo *lwerr.Error", err)
	}
	if launcherErr.Message != "acme-api has no commits yet" {
		t.Errorf("message = %q", launcherErr.Message)
	}
	if launcherErr.NextAction != "make an initial commit, then re-run" {
		t.Errorf("next action = %q", launcherErr.NextAction)
	}
}

func TestAGitSpawnFailureDegradesToNotARepo(t *testing.T) {
	cause := errors.New("spawn failed")
	run := func(_ context.Context, _, _ string, _ []string) (ExecResult, error) {
		return ExecResult{}, cause
	}

	validation := Validate(context.Background(), "/anywhere", run)
	if validation.Status != StatusNotARepo {
		t.Fatalf("status = %q, want %q", validation.Status, StatusNotARepo)
	}
	if !errors.Is(ValidationError(validation), cause) {
		t.Errorf("the spawn failure is not carried as the error cause")
	}
}

func TestHeadVerificationFailureIsUnbornEvenWhenToplevelResolved(t *testing.T) {
	run := func(_ context.Context, _, _ string, args []string) (ExecResult, error) {
		if contains(args, "--show-toplevel") {
			return ExecResult{Stdout: "/fake/root\n"}, nil
		}
		return ExecResult{ExitCode: 1}, nil
	}

	validation := Validate(context.Background(), "/fake/root/sub", run)
	if validation.Status != StatusUnbornHead {
		t.Fatalf("status = %q, want %q", validation.Status, StatusUnbornHead)
	}
	if validation.Repo.Name != "root" {
		t.Errorf("name = %q, want %q", validation.Repo.Name, "root")
	}
}

func TestValidateRunsTheExactArgvInTheGivenDirectory(t *testing.T) {
	var calls [][]string
	var dirs []string
	run := func(_ context.Context, dir, name string, args []string) (ExecResult, error) {
		if name != "git" {
			t.Errorf("command = %q, want git", name)
		}
		calls = append(calls, args)
		dirs = append(dirs, dir)
		if contains(args, "--show-toplevel") {
			return ExecResult{Stdout: "/repo\n"}, nil
		}
		return ExecResult{Stdout: "abc123\n"}, nil
	}

	Validate(context.Background(), "/repo/sub", run)

	want := [][]string{
		{"rev-parse", "--show-toplevel"},
		{"rev-parse", "--path-format=absolute", "--git-dir", "--git-common-dir"},
		{"rev-parse", "--verify", "--quiet", "HEAD"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("argv = %v, want %v", calls, want)
	}
	for _, dir := range dirs {
		if dir != "/repo/sub" {
			t.Errorf("cwd = %q, want the directory as given", dir)
		}
	}
}

func TestDefaultRunnerReportsANonZeroExitWithoutAnError(t *testing.T) {
	requireGit(t)
	dir := tempDir(t)

	result, err := DefaultRunner(context.Background(), dir, "git", []string{"rev-parse", "--show-toplevel"})
	if err != nil {
		t.Fatalf("err = %v, want nil for a command that ran and failed", err)
	}
	if result.ExitCode == 0 {
		t.Errorf("exit code = 0, want non-zero outside a repository")
	}
}

func TestDefaultRunnerErrorsWhenTheCommandCannotStart(t *testing.T) {
	_, err := DefaultRunner(context.Background(), t.TempDir(), "definitely-not-a-real-binary", nil)
	if err == nil {
		t.Fatal("err = nil, want a spawn failure")
	}
}

func TestSourceUsesTheWorkingDirectoryRepository(t *testing.T) {
	requireGit(t)
	dir := filepath.Join(tempDir(t), "acme-api")
	initRepo(t, dir)
	commitEmpty(t, dir)
	sub := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	repo, err := Source(context.Background(), SourceOptions{Dir: sub})
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	if repo.Root != dir || repo.Name != "acme-api" {
		t.Errorf("repo = %+v, want %q named acme-api", repo, dir)
	}
}

func TestSourceFlagWinsOverTheWorkingDirectory(t *testing.T) {
	requireGit(t)
	root := tempDir(t)
	here := filepath.Join(root, "here")
	initRepo(t, here)
	commitEmpty(t, here)
	other := filepath.Join(root, "other")
	initRepo(t, other)
	commitEmpty(t, other)

	repo, err := Source(context.Background(), SourceOptions{Flag: other, Dir: here})
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	if repo.Root != other || repo.Name != "other" {
		t.Errorf("repo = %+v, want %q named other", repo, other)
	}

	// A relative flag is taken against the working directory.
	repo, err = Source(context.Background(), SourceOptions{Flag: filepath.Join("..", "other"), Dir: here})
	if err != nil {
		t.Fatalf("Source with a relative flag: %v", err)
	}
	if repo.Root != other {
		t.Errorf("relative flag root = %q, want %q", repo.Root, other)
	}
}

func TestSourceFromInsideALinkedWorktreeUsesTheMainCheckout(t *testing.T) {
	requireGit(t)
	dir := tempDir(t)
	main := filepath.Join(dir, "acme-api")
	initRepo(t, main)
	commitEmpty(t, main)
	worktree := filepath.Join(dir, "worktrees", "ENG-3971")
	if err := os.MkdirAll(filepath.Dir(worktree), 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, main, "worktree", "add", "--quiet", "-b", "ENG-3971", worktree)

	repo, err := Source(context.Background(), SourceOptions{Dir: worktree})
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	if repo.Root != main {
		t.Errorf("root = %q, want the main checkout %q", repo.Root, main)
	}
}

func TestSourceOutsideAnyRepositoryFailsWithANextAction(t *testing.T) {
	requireGit(t)

	_, err := Source(context.Background(), SourceOptions{Dir: tempDir(t)})
	launcherErr, ok := lwerr.As(err)
	if !ok || launcherErr.Kind != lwerr.NotARepo {
		t.Fatalf("err = %v, want a not_a_repo *lwerr.Error", err)
	}
	if launcherErr.NextAction != NotARepoNextAction {
		t.Errorf("next action = %q", launcherErr.NextAction)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// A missing git binary is not "you are standing in the wrong directory": the
// next action must be installable, not a dead end. SPEC §4, §10.
func TestGitMissingIsNotReportedAsNotARepo(t *testing.T) {
	run := func(ctx context.Context, dir, name string, args []string) (ExecResult, error) {
		return ExecResult{}, &exec.Error{Name: name, Err: exec.ErrNotFound}
	}

	validation := Validate(context.Background(), "/anywhere", run)
	if validation.Status != StatusGitMissing {
		t.Fatalf("status = %q, want %q", validation.Status, StatusGitMissing)
	}

	err := ValidationError(validation)
	if err.Message != GitMissingMessage {
		t.Errorf("message = %q, want %q", err.Message, GitMissingMessage)
	}
	if err.NextAction != GitMissingNextAction {
		t.Errorf("next action = %q, want %q", err.NextAction, GitMissingNextAction)
	}
	if err.Message == NotARepoMessage {
		t.Error("a missing git binary must not claim the directory is not a repository")
	}
}

// A git that runs and reports "not a repository" keeps the original message.
func TestARealGitStillReportsNotARepo(t *testing.T) {
	run := func(ctx context.Context, dir, name string, args []string) (ExecResult, error) {
		return ExecResult{ExitCode: 128, Stderr: "fatal: not a git repository"}, nil
	}

	validation := Validate(context.Background(), "/anywhere", run)
	if validation.Status != StatusNotARepo {
		t.Fatalf("status = %q, want %q", validation.Status, StatusNotARepo)
	}
	if got := ValidationError(validation).Message; got != NotARepoMessage {
		t.Errorf("message = %q, want %q", got, NotARepoMessage)
	}
}
