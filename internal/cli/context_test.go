package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snaylaker/lw/internal/worktree"
)

// openWorktree drives a whole run so the worktree under test is one this tool
// actually created, metadata and all.
func openWorktree(t *testing.T, h *harness, identifier string) string {
	t.Helper()
	h.writeConfig(map[string]any{})
	h.withKey("lin_api_key")
	h.picks(testIssue(identifier))
	if code := h.run(); code != 0 {
		t.Fatalf("opening the worktree: code = %d (stderr %q)", code, h.stderr.String())
	}
	h.stdout.Reset()
	h.stderr.Reset()
	return h.worktreeFor(identifier)
}

// The contract a session hook needs: it runs everywhere, so everywhere that is
// not a worktree lw created has to be silent and successful.
func TestContextPrintsNothingOutsideAWorktreeThisToolCreated(t *testing.T) {
	cases := map[string]func(t *testing.T, h *harness) string{
		"not a git repository": func(t *testing.T, h *harness) string { return realPath(t, t.TempDir()) },
		"a repository with no worktree metadata": func(t *testing.T, h *harness) string {
			return h.repo
		},
		"a subdirectory of one": func(t *testing.T, h *harness) string {
			nested := filepath.Join(h.repo, "src", "billing")
			if err := os.MkdirAll(nested, 0o755); err != nil {
				t.Fatal(err)
			}
			return nested
		},
	}
	for name, locate := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.dir = locate(t, h)

			for _, argv := range [][]string{{"context"}, {"context", "--json"}} {
				h.stdout.Reset()
				h.stderr.Reset()
				if code := h.run(argv...); code != 0 {
					t.Errorf("%q = %d, want 0", argv, code)
				}
				if h.stdout.Len() != 0 || h.stderr.Len() != 0 {
					t.Errorf("%q printed: stdout %q stderr %q", argv, h.stdout.String(), h.stderr.String())
				}
			}
		})
	}
}

func TestContextPrintsTheTicketAndTheReadOnlyNotice(t *testing.T) {
	h := newHarness(t)
	h.dir = openWorktree(t, h, "ENG-3971")

	if code := h.run("context"); code != 0 {
		t.Fatalf("code = %d (stderr %q)", code, h.stderr.String())
	}
	want := "Ticket: ENG-3971 — Improve command completion output\n" +
		"https://linear.app/acme/issue/ENG-3971\n" +
		"This context is read-only; it never writes to Linear.\n"
	if h.stdout.String() != want {
		t.Errorf("stdout = %q, want %q", h.stdout.String(), want)
	}
}

// The summary line appears only once there is a summary.
func TestContextPrintsTheSummaryOnlyWhenItIsSet(t *testing.T) {
	h := newHarness(t)
	h.dir = openWorktree(t, h, "ENG-3971")

	if strings.Contains(h.stdout.String(), "Summary:") {
		t.Fatal("the harness left output behind")
	}
	if code := h.run("summary", "root cause is the dedupe window"); code != 0 {
		t.Fatalf("summary: code = %d (stderr %q)", code, h.stderr.String())
	}
	h.stdout.Reset()

	if code := h.run("context"); code != 0 {
		t.Fatalf("context: code = %d", code)
	}
	want := "Ticket: ENG-3971 — Improve command completion output\n" +
		"https://linear.app/acme/issue/ENG-3971\n" +
		"Summary: root cause is the dedupe window\n" +
		"This context is read-only; it never writes to Linear.\n"
	if h.stdout.String() != want {
		t.Errorf("stdout = %q, want %q", h.stdout.String(), want)
	}
}

// Verbatim means verbatim: the same bytes lw.json holds, so a program reading
// the command and a program reading the file cannot disagree.
func TestContextJSONPrintsTheMetadataObjectVerbatim(t *testing.T) {
	h := newHarness(t)
	h.dir = openWorktree(t, h, "ENG-3971")

	if code := h.run("context", "--json"); code != 0 {
		t.Fatalf("code = %d (stderr %q)", code, h.stderr.String())
	}
	gitDir, err := worktree.GitDir(context.Background(), h.dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	onDisk := readFile(t, worktree.MetadataPath(gitDir))
	if h.stdout.String() != onDisk {
		t.Errorf("stdout = %q, want the file's own bytes %q", h.stdout.String(), onDisk)
	}
	if !strings.Contains(onDisk, `"identifier": "ENG-3971"`) {
		t.Errorf("the metadata file is not what it should be:\n%s", onDisk)
	}
	// SPEC §5 lists five fields and §9 says this command prints the metadata
	// object verbatim, so all five are here — `summary` as "" on a fresh
	// worktree, not missing.
	var fields map[string]any
	if err := json.Unmarshal([]byte(h.stdout.String()), &fields); err != nil {
		t.Fatalf("unmarshalling %s: %v", h.stdout.String(), err)
	}
	want := map[string]any{
		"identifier": "ENG-3971",
		"title":      "Improve command completion output",
		"url":        "https://linear.app/acme/issue/ENG-3971",
		"team":       "ENG",
		"summary":    "",
	}
	if len(fields) != len(want) {
		t.Fatalf("printed %v, want exactly the five fields of SPEC §5", fields)
	}
	for key, value := range want {
		if fields[key] != value {
			t.Errorf("%s = %v, want %v", key, fields[key], value)
		}
	}
}

// A file that is there and unusable is a real problem: hiding it would only
// make it harder to find, so this is the one case context is not silent.
func TestContextReportsUnusableMetadata(t *testing.T) {
	h := newHarness(t)
	h.dir = openWorktree(t, h, "ENG-1")
	gitDir, err := worktree.GitDir(context.Background(), h.dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(worktree.MetadataPath(gitDir), []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}

	code := h.run("context")

	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if h.stdout.Len() != 0 {
		t.Errorf("stdout = %q, want the failure only on stderr", h.stdout.String())
	}
	if !strings.HasPrefix(h.stderr.String(), "error: the worktree metadata at ") ||
		!strings.Contains(h.stderr.String(), "\nnext: ") {
		t.Errorf("stderr = %q", h.stderr.String())
	}
}

// SPEC §5: `lw context` reaps before it reports, so a worktree whose branch was
// deleted goes quiet instead of naming a ticket that has no branch. Removing the
// reap call from runContext must fail here.
func TestContextReapsMetadataWhoseBranchIsGone(t *testing.T) {
	h := newHarness(t).withKey("lin_api_key")
	h.writeConfig(map[string]any{})
	h.picks(testIssue("ENG-3971"))
	if code := h.run(); code != 0 {
		t.Fatalf("code = %d (stderr %q)", code, h.stderr.String())
	}
	path := h.worktreeFor("ENG-3971")

	h.dir = path
	h.stdout.Reset()
	if code := h.run("context"); code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(h.stdout.String(), "ENG-3971") {
		t.Fatalf("stdout = %q, want the ticket while the branch exists", h.stdout.String())
	}

	// Let go of the branch, then delete it, exactly as a user would.
	git(t, path, "switch", "-q", "--detach")
	git(t, h.repo, "branch", "-q", "-D", "ENG-3971")

	h.stdout.Reset()
	if code := h.run("context"); code != 0 {
		t.Fatalf("code = %d after the branch went", code)
	}
	if h.stdout.Len() != 0 {
		t.Errorf("stdout = %q, want silence once the branch is gone", h.stdout.String())
	}
	gitDir := strings.TrimSpace(git(t, path, "rev-parse", "--absolute-git-dir"))
	if _, err := os.Stat(filepath.Join(gitDir, "lw.json")); !os.IsNotExist(err) {
		t.Errorf("the orphaned metadata survived lw context (err = %v)", err)
	}
}
