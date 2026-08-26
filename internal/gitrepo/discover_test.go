package gitrepo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/snaylaker/lw/internal/domain"
)

func execGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_AUTHOR_NAME=lw test",
		"GIT_AUTHOR_EMAIL=test@lw.invalid",
		"GIT_COMMITTER_NAME=lw test",
		"GIT_COMMITTER_EMAIL=test@lw.invalid",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func mkRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	execGit(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("source\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	execGit(t, dir, "add", "README.md")
	execGit(t, dir, "commit", "-q", "-m", "initial")
}

func discover(t *testing.T, roots ...string) []domain.Repo {
	t.Helper()
	return Discover(context.Background(), roots, nil)
}

// SPEC §4: roots are scanned one level deep — no recursive walk, so a vendored
// checkout inside a repository is never offered.
func TestDiscoverFindsCheckoutsOneLevelDeepOnly(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, filepath.Join(root, "alpha"))
	mkRepo(t, filepath.Join(root, "beta"))
	mkRepo(t, filepath.Join(root, "alpha", "vendor", "nested"))
	if err := os.MkdirAll(filepath.Join(root, "not-a-repo", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	found := discover(t, root)
	if len(found) != 2 {
		t.Fatalf("found %+v, want alpha and beta only", found)
	}
	if found[0].Name != "alpha" || found[1].Name != "beta" {
		t.Errorf("found %+v, want sorted by name", found)
	}
}

// A stale entry in config.json must not stop the picker opening.
func TestDiscoverSkipsRootsThatDoNotExist(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, filepath.Join(root, "alpha"))

	found := discover(t, filepath.Join(root, "nope"), root, "", "   ")
	if len(found) != 1 || found[0].Name != "alpha" {
		t.Fatalf("found %+v, want alpha despite the missing root", found)
	}
}

// The same repository reachable through two roots is offered once.
func TestDiscoverDeduplicatesByResolvedRepository(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, filepath.Join(root, "alpha"))

	found := discover(t, root, root)
	if len(found) != 1 {
		t.Fatalf("found %+v, want one entry", found)
	}
}

// A linked worktree is resolved to the main checkout before it reaches the picker.
func TestDiscoverMapsALinkedWorktreeToItsMainCheckout(t *testing.T) {
	base := t.TempDir()
	main := filepath.Join(base, "main", "alpha")
	root := filepath.Join(base, "linked")
	linked := filepath.Join(root, "ENG-1")
	mkRepo(t, main)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	execGit(t, main, "worktree", "add", "-q", "-b", "ENG-1", linked)

	main, err := filepath.EvalSymlinks(main)
	if err != nil {
		t.Fatal(err)
	}
	found := discover(t, root)
	if len(found) != 1 || found[0].Name != "alpha" || found[0].Root != main {
		t.Fatalf("found %+v, want the main checkout %q", found, main)
	}
}

// The same repository reached through a symlinked root must not be offered twice.
func TestDiscoverCanonicalisesSoSymlinkedRootsDoNotDuplicate(t *testing.T) {
	real := t.TempDir()
	mkRepo(t, filepath.Join(real, "alpha"))

	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	found := discover(t, real, link)
	if len(found) != 1 {
		t.Fatalf("found %+v, want one entry for one repository", found)
	}
}
