package worktree

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snaylaker/lw/internal/domain"
	"github.com/snaylaker/lw/internal/gitrepo"
)

// finishedRepo builds a repo with a worktree whose branch has been merged
// normally, and returns the repo plus the worktree path.
func finishedRepo(t *testing.T) (domain.Repo, string) {
	t.Helper()
	repo := newRepo(t)
	result, err := open(t, repo, t.TempDir(), "ENG-3971", nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	write(t, filepath.Join(result.Path, "feature.txt"), "work\n")
	git(t, result.Path, "add", "feature.txt")
	git(t, result.Path, "commit", "-q", "-m", "the work")

	git(t, repo.Root, "merge", "-q", "--no-ff", "ENG-3971", "-m", "merged")
	return repo, result.Path
}

func TestAnOrdinaryMergeIsFound(t *testing.T) {
	repo, path := finishedRepo(t)

	found, err := FindFinished(context.Background(), PruneOptions{Repo: repo})
	if err != nil {
		t.Fatalf("FindFinished: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("found %d candidates, want 1: %+v", len(found), found)
	}
	if found[0].Identifier != "ENG-3971" || found[0].Reason != ReasonMerged {
		t.Errorf("candidate = %+v, want ENG-3971 %q", found[0], ReasonMerged)
	}
	if !samePath(found[0].Path, path) {
		t.Errorf("path = %q, want %q", found[0].Path, path)
	}
}

// The branch we are standing in is never a candidate, whatever its state.
func TestTheCurrentWorktreeIsNeverACandidate(t *testing.T) {
	repo, path := finishedRepo(t)

	found, err := FindFinished(context.Background(), PruneOptions{Repo: repo, Current: path})
	if err != nil {
		t.Fatalf("FindFinished: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("found %+v; the worktree the user is standing in must be left alone", found)
	}
}

// Unmerged work is never offered up. This is the assertion that fails if the
// merge test is inverted or dropped.
func TestUnmergedWorkIsNotACandidate(t *testing.T) {
	repo := newRepo(t)
	result, err := open(t, repo, t.TempDir(), "ENG-3971", nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	write(t, filepath.Join(result.Path, "feature.txt"), "work\n")
	git(t, result.Path, "add", "feature.txt")
	git(t, result.Path, "commit", "-q", "-m", "unmerged work")

	found, err := FindFinished(context.Background(), PruneOptions{Repo: repo})
	if err != nil {
		t.Fatalf("FindFinished: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("found %+v; unmerged work must never be pruned", found)
	}
}

// A worktree someone added by hand carries no metadata and is not ours to
// remove, however merged its branch is.
func TestAForeignWorktreeIsNeverACandidate(t *testing.T) {
	repo, path := finishedRepo(t)

	gitDir, err := GitDir(context.Background(), path, nil)
	if err != nil {
		t.Fatalf("GitDir: %v", err)
	}
	if err := os.Remove(MetadataPath(gitDir)); err != nil {
		t.Fatalf("removing metadata: %v", err)
	}

	found, err := FindFinished(context.Background(), PruneOptions{Repo: repo})
	if err != nil {
		t.Fatalf("FindFinished: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("found %+v; a worktree without lw.json is not ours", found)
	}
}

func TestRemoveDeletesTheWorktreeAndItsBranch(t *testing.T) {
	repo, path := finishedRepo(t)

	found, err := FindFinished(context.Background(), PruneOptions{Repo: repo})
	if err != nil || len(found) != 1 {
		t.Fatalf("FindFinished = %+v, %v", found, err)
	}
	if err := Remove(context.Background(), repo, found[0], nil); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("worktree directory survived removal (err = %v)", err)
	}
	if branchExistsInTest(t, repo.Root, "ENG-3971") {
		t.Error("branch survived removal")
	}
}

// Uncommitted work must survive: prune reclaims finished directories, it does
// not throw away something the user forgot to commit.
func TestRemoveRefusesAWorktreeWithUncommittedWork(t *testing.T) {
	repo, path := finishedRepo(t)
	write(t, filepath.Join(path, "scratch.txt"), "not committed\n")
	git(t, path, "add", "scratch.txt")

	found, err := FindFinished(context.Background(), PruneOptions{Repo: repo})
	if err != nil || len(found) != 1 {
		t.Fatalf("FindFinished = %+v, %v", found, err)
	}
	if err := Remove(context.Background(), repo, found[0], nil); err == nil {
		t.Fatal("Remove succeeded; a dirty worktree must refuse to go")
	}
	if _, err := os.Stat(filepath.Join(path, "scratch.txt")); err != nil {
		t.Errorf("uncommitted work was destroyed: %v", err)
	}
}

func branchExistsInTest(t *testing.T, root, branch string) bool {
	t.Helper()
	return strings.TrimSpace(git(t, root, "branch", "--list", branch)) != ""
}

// SPEC §5: a squash merge — the GitHub default — lands the content without the
// branch's commit, so the branch is an ancestor of nothing and the merge test
// cannot see it. What a merged-and-tidied pull request reliably leaves behind is
// an upstream marked [gone]. That signal is what finds real pull requests;
// without it prune is blind to the common case.
//
// The first assertion is the load-bearing one: it proves the ancestor test does
// NOT find this branch, so nobody can "simplify" FindFinished back to a merge
// check and still pass.
func TestASquashMergedBranchIsFoundByItsGoneUpstream(t *testing.T) {
	repo := newRepo(t)

	remote := t.TempDir()
	git(t, remote, "init", "-q", "--bare", ".")
	git(t, repo.Root, "remote", "add", "origin", remote)
	git(t, repo.Root, "push", "-q", "-u", "origin", "main")

	result, err := open(t, repo, t.TempDir(), "ENG-3971", nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	write(t, filepath.Join(result.Path, "feature.txt"), "work\n")
	git(t, result.Path, "add", "feature.txt")
	git(t, result.Path, "commit", "-q", "-m", "the work")
	git(t, result.Path, "push", "-q", "-u", "origin", "ENG-3971")

	// The pull request is squash-merged, then its branch is deleted.
	git(t, repo.Root, "merge", "-q", "--squash", "ENG-3971")
	git(t, repo.Root, "commit", "-q", "-m", "squashed")
	git(t, remote, "branch", "-q", "-D", "ENG-3971")
	git(t, repo.Root, "fetch", "-q", "--prune")

	def, ok := defaultBranch(context.Background(), repo.Root, gitrepo.DefaultRunner)
	if !ok {
		t.Fatal("no default branch resolved")
	}
	if mergedInto(context.Background(), repo.Root, "ENG-3971", def, gitrepo.DefaultRunner) {
		t.Fatal("the squashed branch is an ancestor of the default branch; the fixture no longer models a squash merge")
	}

	found, err := FindFinished(context.Background(), PruneOptions{Repo: repo})
	if err != nil {
		t.Fatalf("FindFinished: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("found %d candidates, want 1 — a squash-merged PR was missed: %+v", len(found), found)
	}
	if found[0].Reason != ReasonUpstreamGone {
		t.Errorf("reason = %q, want %q", found[0].Reason, ReasonUpstreamGone)
	}
	if found[0].Identifier != "ENG-3971" {
		t.Errorf("identifier = %q", found[0].Identifier)
	}
}

// A branch whose upstream still exists is not finished, however much work sits
// on it. This is the assertion that fails if upstreamGone is inverted.
func TestALiveUpstreamIsNotFinished(t *testing.T) {
	repo := newRepo(t)
	remote := t.TempDir()
	git(t, remote, "init", "-q", "--bare", ".")
	git(t, repo.Root, "remote", "add", "origin", remote)
	git(t, repo.Root, "push", "-q", "-u", "origin", "main")

	result, err := open(t, repo, t.TempDir(), "ENG-3971", nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	write(t, filepath.Join(result.Path, "feature.txt"), "work\n")
	git(t, result.Path, "add", "feature.txt")
	git(t, result.Path, "commit", "-q", "-m", "the work")
	git(t, result.Path, "push", "-q", "-u", "origin", "ENG-3971")
	git(t, repo.Root, "fetch", "-q", "--prune")

	found, err := FindFinished(context.Background(), PruneOptions{Repo: repo})
	if err != nil {
		t.Fatalf("FindFinished: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("found %+v; an open pull request must never be pruned", found)
	}
}
