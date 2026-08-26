package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/snaylaker/lw/internal/worktree"
)

func TestSummaryRecordsTheTextAndPrintsNothing(t *testing.T) {
	h := newHarness(t)
	h.dir = openWorktree(t, h, "ENG-3971")

	code := h.run("summary", "root cause is the alert dedupe window")

	if code != 0 {
		t.Fatalf("code = %d (stderr %q)", code, h.stderr.String())
	}
	if h.stdout.Len() != 0 || h.stderr.Len() != 0 {
		t.Errorf("printed: stdout %q stderr %q", h.stdout.String(), h.stderr.String())
	}
	metadata, err := worktree.ReadMetadata(context.Background(), h.dir, nil)
	if err != nil || metadata == nil {
		t.Fatalf("metadata = %v, %v", metadata, err)
	}
	if metadata.Summary != "root cause is the alert dedupe window" {
		t.Errorf("summary = %q", metadata.Summary)
	}
	// The rest of the metadata is kept: only the one mutable field moves.
	if metadata.Identifier != "ENG-3971" || metadata.Title == "" || metadata.URL == "" {
		t.Errorf("metadata = %+v", metadata)
	}
}

// An unquoted sentence records what it looks like it records.
func TestSummaryJoinsItsArguments(t *testing.T) {
	h := newHarness(t)
	h.dir = openWorktree(t, h, "ENG-1")

	if code := h.run("summary", "narrowed", "it", "down"); code != 0 {
		t.Fatalf("code = %d (stderr %q)", code, h.stderr.String())
	}
	metadata, _ := worktree.ReadMetadata(context.Background(), h.dir, nil)
	if metadata == nil || metadata.Summary != "narrowed it down" {
		t.Errorf("summary = %+v", metadata)
	}
}

func TestSummaryWithoutTextIsAUsageError(t *testing.T) {
	h := newHarness(t)
	h.dir = openWorktree(t, h, "ENG-1")

	code := h.run("summary")

	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	want := "error: lw summary needs the text to record\n\n" + HelpText()
	if h.stderr.String() != want {
		t.Errorf("stderr = %q, want %q", h.stderr.String(), want)
	}
}

// There is nothing to summarise where no worktree metadata exists: the
// identifier would be a guess.
func TestSummaryOutsideAWorktreeIsReported(t *testing.T) {
	h := newHarness(t)

	code := h.run("summary", "text")

	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(h.stderr.String(), "carries no worktree metadata") ||
		!strings.Contains(h.stderr.String(), "\nnext: ") {
		t.Errorf("stderr = %q", h.stderr.String())
	}
}
