package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mergedWorktree adds a worktree for identifier, gives it lw.json so prune will
// claim it, and merges its branch into the default branch.
func mergedWorktree(t *testing.T, h *harness, identifier string) string {
	t.Helper()
	path := h.worktreeFor(identifier)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, h.repo, "worktree", "add", "-q", "-b", identifier, path)

	write(t, filepath.Join(path, identifier+".txt"), "work\n")
	git(t, path, "add", "-A")
	git(t, path, "commit", "-q", "-m", "work on "+identifier)
	git(t, h.repo, "merge", "-q", "--no-ff", identifier, "-m", "merge "+identifier)

	gitDir := strings.TrimSpace(git(t, path, "rev-parse", "--absolute-git-dir"))
	payload := `{"identifier":"` + identifier + `","title":"Shipped ` + identifier + `"}`
	if err := os.WriteFile(filepath.Join(gitDir, "lw.json"), []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// SPEC §5: prune reports and removes nothing without --yes. Deleting a checkout
// is not undoable, so the destructive form is the one you have to ask for.
func TestPruneReportsWithoutRemovingAnything(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(map[string]any{})
	path := mergedWorktree(t, h, "ENG-4000")

	if code := h.run("prune", "--no-fetch"); code != 0 {
		t.Fatalf("code = %d (stderr %q)", code, h.stderr.String())
	}
	out := h.stdout.String()
	for _, want := range []string{"ENG-4000", "Shipped ENG-4000", "merged", "lw prune --yes"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout = %q, want it to mention %q", out, want)
		}
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("prune removed a worktree without --yes: %v", err)
	}
}

func TestPruneYesRemovesTheWorktreeAndItsBranch(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(map[string]any{})
	path := mergedWorktree(t, h, "ENG-4000")

	if code := h.run("prune", "--no-fetch", "--yes"); code != 0 {
		t.Fatalf("code = %d (stderr %q)", code, h.stderr.String())
	}
	if !strings.Contains(h.stdout.String(), "removed ENG-4000") {
		t.Errorf("stdout = %q", h.stdout.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the worktree survived --yes (err = %v)", err)
	}
	if strings.TrimSpace(git(t, h.repo, "branch", "--list", "ENG-4000")) != "" {
		t.Error("the branch survived --yes")
	}
}

func TestPruneWithNothingFinishedSaysSo(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(map[string]any{})

	if code := h.run("prune", "--no-fetch"); code != 0 {
		t.Fatalf("code = %d (stderr %q)", code, h.stderr.String())
	}
	if h.stdout.String() != "Nothing to prune.\n" {
		t.Errorf("stdout = %q", h.stdout.String())
	}
}

// SPEC §5: pruneMerged runs automatically before opening the next worktree. Its report goes
// to stderr, because stdout carries the worktree path alone.
func TestPruneMergedConfiguredRemovesFinishedWorkOnARun(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(map[string]any{"pruneMerged": true})
	path := mergedWorktree(t, h, "ENG-4000")

	pruneMergedIfConfigured(t.Context(), h.repoStruct(t), h.execEnv(t))

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("pruneMerged was configured but the finished worktree survived (err = %v)", err)
	}
	if h.stdout.Len() != 0 {
		t.Errorf("the automatic pass printed %q on stdout; stdout carries the path alone", h.stdout.String())
	}
	if !strings.Contains(h.stderr.String(), "pruned ENG-4000") {
		t.Errorf("stderr = %q, want it to report what it removed", h.stderr.String())
	}
}

// Off by default: nothing may be deleted unasked.
func TestPruneMergedOffLeavesFinishedWorkAlone(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(map[string]any{})
	path := mergedWorktree(t, h, "ENG-4000")

	pruneMergedIfConfigured(t.Context(), h.repoStruct(t), h.execEnv(t))

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("a finished worktree was removed with pruneMerged unset: %v", err)
	}
}

func TestPruneAutoSavesThePreferenceAndNoAutoClearsIt(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(map[string]any{})

	if code := h.run("prune", "--auto"); code != 0 {
		t.Fatalf("code = %d (stderr %q)", code, h.stderr.String())
	}
	if !storedPruneMerged(t, h) {
		t.Fatal("--auto did not save pruneMerged")
	}
	if !strings.Contains(h.stdout.String(), "now on") {
		t.Errorf("stdout = %q", h.stdout.String())
	}

	h.stdout.Reset()
	if code := h.run("prune", "--auto"); code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(h.stdout.String(), "already on") {
		t.Errorf("a second --auto claimed a write it did not make: %q", h.stdout.String())
	}

	h.stdout.Reset()
	if code := h.run("prune", "--no-auto"); code != 0 {
		t.Fatalf("code = %d", code)
	}
	if storedPruneMerged(t, h) {
		t.Error("--no-auto did not clear pruneMerged")
	}
}

// --auto and --no-auto contradict each other; picking one silently would be worse
// than saying so.
func TestPruneAutoAndNoAutoTogetherIsAUsageError(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(map[string]any{})

	if code := h.run("prune", "--auto", "--no-auto"); code != 2 {
		t.Fatalf("code = %d, want 2 (stderr %q)", code, h.stderr.String())
	}
	if storedPruneMerged(t, h) {
		t.Error("a contradictory invocation still wrote the preference")
	}
}

// --auto records a preference; it must not also delete anything in the same run.
func TestPruneAutoDoesNotAlsoPrune(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(map[string]any{})
	path := mergedWorktree(t, h, "ENG-4000")

	if code := h.run("prune", "--auto"); code != 0 {
		t.Fatalf("code = %d (stderr %q)", code, h.stderr.String())
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("--auto removed a worktree as well as saving the flag: %v", err)
	}
}

func storedPruneMerged(t *testing.T, h *harness) bool {
	t.Helper()
	data, err := os.ReadFile(h.configPath())
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		PruneMerged bool `json:"pruneMerged"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("config is not valid JSON: %v", err)
	}
	return stored.PruneMerged
}

// A repository with no remote cannot fetch. That warns and carries on judging
// from local refs — and the warning goes to stderr, because stdout carries the
// worktree path alone (SPEC §3).
func TestPruneFetchFailureWarnsOnStderrAndStillReports(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(map[string]any{})
	path := mergedWorktree(t, h, "ENG-4000")

	// A remote that does not exist: git fetch fails outright. (With no remote at
	// all git exits 0 and there is nothing to warn about.)
	git(t, h.repo, "remote", "add", "origin", filepath.Join(t.TempDir(), "no-such-remote"))

	// No --no-fetch, so the fetch is attempted and fails.
	if code := h.run("prune"); code != 0 {
		t.Fatalf("code = %d; a failed fetch must not fail the command (stderr %q)", code, h.stderr.String())
	}
	if !strings.Contains(h.stderr.String(), "could not fetch") {
		t.Errorf("stderr = %q, want the fetch warning", h.stderr.String())
	}
	if strings.Contains(h.stdout.String(), "could not fetch") {
		t.Errorf("the fetch warning reached stdout: %q", h.stdout.String())
	}
	// It still found the merged worktree from the refs already present.
	if !strings.Contains(h.stdout.String(), "ENG-4000") {
		t.Errorf("stdout = %q, want the candidate still reported", h.stdout.String())
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("a reporting run removed a worktree: %v", err)
	}
}

// SPEC §5: "one busy checkout never stops the others". A worktree with
// uncommitted work refuses to go; every other finished worktree must still be
// removed, and the exit code says something went wrong.
func TestPruneKeepsGoingWhenOneWorktreeRefusesToBeRemoved(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(map[string]any{})
	busy := mergedWorktree(t, h, "ENG-4000")
	clean := mergedWorktree(t, h, "ENG-4001")

	// Uncommitted work makes `git worktree remove` refuse, without --force.
	write(t, filepath.Join(busy, "scratch.txt"), "not committed\n")
	git(t, busy, "add", "scratch.txt")

	code := h.run("prune", "--no-fetch", "--yes")
	if code != 1 {
		t.Fatalf("code = %d, want 1 when a worktree could not be removed (stderr %q)", code, h.stderr.String())
	}
	if _, err := os.Stat(busy); err != nil {
		t.Errorf("the busy worktree was destroyed: %v", err)
	}
	if _, err := os.Stat(clean); !os.IsNotExist(err) {
		t.Errorf("one refusal stopped the others: ENG-4001 survived (err = %v)", err)
	}
	if !strings.Contains(h.stdout.String(), "removed ENG-4001") {
		t.Errorf("stdout = %q, want the clean worktree reported as removed", h.stdout.String())
	}
	if !strings.Contains(h.stdout.String(), "1 removed, 1 kept") {
		t.Errorf("stdout = %q, want the tally", h.stdout.String())
	}
}

// SPEC §5: --no-fetch keeps prune entirely offline. With a remote that cannot be
// reached, fetching warns; --no-fetch must not even try.
func TestPruneNoFetchDoesNotTouchTheRemote(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(map[string]any{})
	mergedWorktree(t, h, "ENG-4000")
	git(t, h.repo, "remote", "add", "origin", filepath.Join(t.TempDir(), "no-such-remote"))

	if code := h.run("prune", "--no-fetch"); code != 0 {
		t.Fatalf("code = %d (stderr %q)", code, h.stderr.String())
	}
	if strings.Contains(h.stderr.String(), "could not fetch") {
		t.Errorf("--no-fetch still attempted a fetch: stderr = %q", h.stderr.String())
	}
	// It still reports, from the refs already present.
	if !strings.Contains(h.stdout.String(), "ENG-4000") {
		t.Errorf("stdout = %q, want the candidate still found offline", h.stdout.String())
	}
}
