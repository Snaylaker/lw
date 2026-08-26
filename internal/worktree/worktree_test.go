package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snaylaker/lw/internal/domain"
	"github.com/snaylaker/lw/internal/gitrepo"
	"github.com/snaylaker/lw/internal/lwerr"
)

// isolateGit keeps every test off the machine's git configuration: a global
// commit.gpgsign or a template hook would otherwise decide whether the suite
// passes.
func isolateGit(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	t.Setenv("GIT_AUTHOR_NAME", "lw test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@lw.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "lw test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@lw.invalid")
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, output)
	}
	return strings.TrimSpace(string(output))
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newRepo is a real repository with one commit, resolved exactly as a run
// resolves the directory the user stands in.
func newRepo(t *testing.T) domain.Repo {
	t.Helper()
	isolateGit(t)
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	write(t, filepath.Join(dir, "README.md"), "source\n")
	git(t, dir, "add", "README.md")
	git(t, dir, "commit", "-q", "-m", "initial")
	repo, err := gitrepo.Resolve(context.Background(), dir, nil)
	if err != nil {
		t.Fatalf("resolving the test repository: %v", err)
	}
	return repo
}

func issue(identifier string) domain.Issue {
	return domain.Issue{
		ID:         "i-" + identifier,
		Identifier: identifier,
		Title:      "Something to do in " + identifier,
		URL:        "https://linear.app/acme/issue/" + identifier,
		TeamKey:    strings.SplitN(identifier, "-", 2)[0],
	}
}

// sameDir compares two paths the way the operating system does: a temporary
// directory reaches the test through a symlink on macOS.
func sameDir(t *testing.T, a, b string) bool {
	t.Helper()
	realA, errA := filepath.EvalSymlinks(a)
	realB, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		t.Fatalf("resolving %q / %q: %v %v", a, b, errA, errB)
	}
	return realA == realB
}

type recorder struct{ updates []domain.StageUpdate }

func (r *recorder) record(update domain.StageUpdate) { r.updates = append(r.updates, update) }

func (r *recorder) states() []string {
	states := make([]string, 0, len(r.updates))
	for _, update := range r.updates {
		states = append(states, string(update.Stage)+":"+string(update.State))
	}
	return states
}

func open(t *testing.T, repo domain.Repo, root, identifier string, stage func(domain.StageUpdate)) (Result, error) {
	t.Helper()
	return Open(context.Background(), Options{
		Repo:    repo,
		Issue:   issue(identifier),
		Root:    root,
		OnStage: stage,
	})
}

func TestPathGroupsWorktreesByRepository(t *testing.T) {
	if got := Path("/root", "acme-api", "ENG-3971"); got != "/root/acme-api/ENG-3971" {
		t.Fatalf("Path = %q", got)
	}
}

func TestOpenCreatesTheBranchAndTheCheckout(t *testing.T) {
	repo := newRepo(t)
	root := t.TempDir()
	stages := &recorder{}

	result, err := open(t, repo, root, "ENG-1", stages.record)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !result.Created {
		t.Fatal("a first open must report Created")
	}
	if want := Path(root, repo.Name, "ENG-1"); result.Path != want {
		t.Fatalf("path = %q, want %q", result.Path, want)
	}
	if branch := git(t, result.Path, "rev-parse", "--abbrev-ref", "HEAD"); branch != "ENG-1" {
		t.Fatalf("branch = %q", branch)
	}
	if _, err := os.Stat(filepath.Join(result.Path, "README.md")); err != nil {
		t.Fatalf("the worktree is not a checkout of the repository: %v", err)
	}
	want := []string{"preparing:active", "preparing:done", "creating-worktree:active", "creating-worktree:done"}
	if got := strings.Join(stages.states(), ","); got != strings.Join(want, ",") {
		t.Fatalf("stages = %s", got)
	}

	metadata, err := ReadMetadata(context.Background(), result.Path, nil)
	if err != nil || metadata == nil {
		t.Fatalf("ReadMetadata = %v, %v", metadata, err)
	}
	if metadata.Identifier != "ENG-1" || metadata.Team != "ENG" {
		t.Fatalf("metadata = %+v", *metadata)
	}
}

func TestOpenReusesAnExistingWorktree(t *testing.T) {
	repo := newRepo(t)
	root := t.TempDir()

	first, err := open(t, repo, root, "ENG-1", nil)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	write(t, filepath.Join(first.Path, "work-in-progress.txt"), "keep me\n")

	stages := &recorder{}
	second, err := open(t, repo, root, "ENG-1", stages.record)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	if second.Created {
		t.Fatal("reopening an issue must not report Created")
	}
	if second.Path != first.Path {
		t.Fatalf("path = %q, want %q", second.Path, first.Path)
	}
	if _, err := os.Stat(filepath.Join(first.Path, "work-in-progress.txt")); err != nil {
		t.Fatalf("reuse destroyed uncommitted work: %v", err)
	}
	want := []string{"preparing:active", "preparing:done", "creating-worktree:skipped"}
	if got := strings.Join(stages.states(), ","); got != strings.Join(want, ",") {
		t.Fatalf("stages = %s", got)
	}
}

func TestOpenRefreshesMetadataOfAReusedWorktree(t *testing.T) {
	repo := newRepo(t)
	root := t.TempDir()

	first, err := open(t, repo, root, "ENG-1", nil)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	renamed := issue("ENG-1")
	renamed.Title = "Retitled in Linear"
	if _, err := Open(context.Background(), Options{Repo: repo, Issue: renamed, Root: root}); err != nil {
		t.Fatalf("second Open: %v", err)
	}

	metadata, err := ReadMetadata(context.Background(), first.Path, nil)
	if err != nil || metadata == nil {
		t.Fatalf("ReadMetadata = %v, %v", metadata, err)
	}
	if metadata.Title != "Retitled in Linear" {
		t.Fatalf("title = %q", metadata.Title)
	}
}

func TestOpenChecksOutAnExistingBranch(t *testing.T) {
	repo := newRepo(t)
	root := t.TempDir()
	git(t, repo.Root, "checkout", "-q", "-b", "ENG-2")
	write(t, filepath.Join(repo.Root, "started.txt"), "earlier work\n")
	git(t, repo.Root, "add", "started.txt")
	git(t, repo.Root, "commit", "-q", "-m", "earlier work")
	git(t, repo.Root, "checkout", "-q", "main")

	result, err := open(t, repo, root, "ENG-2", nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !result.Created {
		t.Fatal("a new worktree on an existing branch is still Created")
	}
	if _, err := os.Stat(filepath.Join(result.Path, "started.txt")); err != nil {
		t.Fatalf("the existing branch was not checked out: %v", err)
	}
}

func TestOpenRepairsAStaleRegistration(t *testing.T) {
	repo := newRepo(t)
	root := t.TempDir()

	first, err := open(t, repo, root, "ENG-1", nil)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	// The directory disappears — a deleted worktree, an unmounted disk — while
	// git keeps the registration that blocks both the path and the branch.
	if err := os.RemoveAll(first.Path); err != nil {
		t.Fatal(err)
	}

	second, err := open(t, repo, root, "ENG-1", nil)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	if !second.Created {
		t.Fatal("a repaired worktree is a created one")
	}
	if second.Path != first.Path {
		t.Fatalf("path = %q, want %q", second.Path, first.Path)
	}
	if branch := git(t, second.Path, "rev-parse", "--abbrev-ref", "HEAD"); branch != "ENG-1" {
		t.Fatalf("branch = %q", branch)
	}
}

// SPEC §5 quotes this failure literally: message `<path> already exists and is
// not a worktree of <repo>`, next `remove it, or set worktreeRoot to another
// location`.
func TestOpenReportsAConflictingPath(t *testing.T) {
	repo := newRepo(t)
	root := t.TempDir()
	path := Path(root, repo.Name, "ENG-1")
	write(t, filepath.Join(path, "someone-elses.txt"), "not ours\n")

	_, err := open(t, repo, root, "ENG-1", nil)
	if !lwerr.Is(err, lwerr.WorktreeConflict) {
		t.Fatalf("error = %v, want a worktree_conflict", err)
	}
	conflict, _ := lwerr.As(err)
	if conflict.Message != path+" already exists and is not a worktree of "+repo.Name {
		t.Errorf("message = %q", conflict.Message)
	}
	if conflict.NextAction != "remove it, or set worktreeRoot to another location" {
		t.Errorf("next action = %q", conflict.NextAction)
	}
}

// SPEC §10: a cancellation prints nothing and exits 130. Ctrl+C lands while git
// is running, so git fails — and the failure git reports must not become an
// internal error the user is asked to act on. Both git calls Open makes are
// covered: the listing it starts with, and the `worktree add` itself.
func TestOpenReportsACancellationRatherThanGitsFailure(t *testing.T) {
	repo := newRepo(t)

	t.Run("cancelled before the listing", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := Open(ctx, Options{Repo: repo, Issue: issue("ENG-1"), Root: t.TempDir()})
		assertSilentCancellation(t, err)
	})

	t.Run("cancelled at git worktree add", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		// Real git for everything up to the add, then the abort lands.
		run := func(c context.Context, dir, name string, args []string) (gitrepo.ExecResult, error) {
			if len(args) > 1 && args[0] == "worktree" && args[1] == "add" {
				cancel()
			}
			return gitrepo.DefaultRunner(c, dir, name, args)
		}
		_, err := Open(ctx, Options{
			Repo:  repo,
			Issue: issue("ENG-2"),
			Root:  t.TempDir(),
			Run:   run,
		})
		assertSilentCancellation(t, err)
	})
}

func assertSilentCancellation(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("a cancelled run must fail")
	}
	var out strings.Builder
	if code := lwerr.Report(err, &out); code != 130 {
		t.Fatalf("exit code = %d, want 130 (error %v)", code, err)
	}
	if out.String() != "" {
		t.Fatalf("a cancellation printed %q", out.String())
	}
}

func TestOpenAcceptsAnEmptyDirectoryAtThePath(t *testing.T) {
	repo := newRepo(t)
	root := t.TempDir()
	path := Path(root, repo.Name, "ENG-1")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := open(t, repo, root, "ENG-1", nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if result.Path != path || !result.Created {
		t.Fatalf("result = %+v", result)
	}
}

func TestOpenReusesTheWorktreeHoldingTheBranchElsewhere(t *testing.T) {
	repo := newRepo(t)
	first, err := open(t, repo, t.TempDir(), "ENG-1", nil)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}

	// The configured root moved. git still refuses to check the branch out
	// twice, so the worktree that holds it is the one to reuse.
	second, err := open(t, repo, t.TempDir(), "ENG-1", nil)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	if second.Created || !sameDir(t, second.Path, first.Path) {
		t.Fatalf("result = %+v, want a reuse of %q", second, first.Path)
	}
}

func TestOpenNeverHandsBackTheMainCheckout(t *testing.T) {
	repo := newRepo(t)
	root := t.TempDir()
	git(t, repo.Root, "checkout", "-q", "-b", "ENG-1")

	_, err := open(t, repo, root, "ENG-1", nil)
	if err == nil {
		t.Fatal("the branch is checked out in the main repository; git must be allowed to refuse")
	}
	if strings.Contains(err.Error(), "already exists and is not a worktree") {
		t.Fatalf("the main checkout was treated as a conflicting path: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(repo.Root, MetadataFileName)); statErr == nil {
		t.Fatal("nothing may be written into the user's own checkout")
	}
}

func TestOpenRejectsAnIdentifierThatIsNotAName(t *testing.T) {
	repo := newRepo(t)
	root := t.TempDir()
	for _, identifier := range []string{"", "..", "ENG/1"} {
		if _, err := open(t, repo, root, identifier, nil); err == nil {
			t.Fatalf("%q was accepted as a worktree name", identifier)
		}
	}
}

func TestParseListReadsThePorcelainRecords(t *testing.T) {
	output := "worktree /repo\nHEAD abc\nbranch refs/heads/main\n\n" +
		"worktree /root/repo/ENG-1\nHEAD def\nbranch refs/heads/ENG-1\n\n" +
		"worktree /root/repo/ENG-2\nHEAD 000\ndetached\nprunable gitdir file points to non-existent location\n\n" +
		"worktree /bare\nbare\n"
	entries := parseList(output)
	if len(entries) != 4 {
		t.Fatalf("entries = %+v", entries)
	}
	if entries[1].Path != "/root/repo/ENG-1" || entries[1].Branch != "ENG-1" {
		t.Fatalf("linked entry = %+v", entries[1])
	}
	if !entries[2].Prunable || entries[2].Branch != "" {
		t.Fatalf("prunable entry = %+v", entries[2])
	}
	if !entries[3].Bare {
		t.Fatalf("bare entry = %+v", entries[3])
	}
}
