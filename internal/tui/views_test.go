package tui

import (
	"testing"

	"github.com/snaylaker/lw/internal/domain"
	"github.com/snaylaker/lw/internal/lwerr"
)

// SPEC §3 quotes the stages, lower-case and in this order.
func TestTheStageLabelsAreTheOnesSection3Names(t *testing.T) {
	want := []string{"preparing", "creating worktree"}
	if len(stageOrder) != len(want) {
		t.Fatalf("stageOrder = %v, want the two stages of SPEC §3", stageOrder)
	}
	for i, label := range want {
		if got := stageLabels[stageOrder[i]]; got != label {
			t.Errorf("stage %d = %q, want %q", i, got, label)
		}
	}
}

func TestProgressViewStartsWithEveryStagePending(t *testing.T) {
	frame := plain(NewProgressView().View())
	mustContain(t, frame, "Opening worktree")
	mustContain(t, frame, "○ preparing")
	mustContain(t, frame, "○ creating worktree")
}

func TestProgressViewGlyphsAndDetail(t *testing.T) {
	view := NewProgressView()
	view.ApplyUpdate(domain.StageUpdate{Stage: domain.StagePreparing, State: domain.StateActive})
	mustContain(t, plain(view.View()), "◐ preparing")

	view.ApplyUpdate(domain.StageUpdate{Stage: domain.StagePreparing, State: domain.StateDone})
	mustContain(t, plain(view.View()), "● preparing")

	view.ApplyUpdate(domain.StageUpdate{
		Stage:  domain.StageCreatingWorktree,
		State:  domain.StateDone,
		Detail: "/tmp/worktrees/ENG-100",
	})
	mustContain(t, plain(view.View()), "● creating worktree — /tmp/worktrees/ENG-100")

	// The detail is sticky across an update that omits it.
	view.ApplyUpdate(domain.StageUpdate{Stage: domain.StageCreatingWorktree, State: domain.StateFailed})
	mustContain(t, plain(view.View()), "✗ creating worktree — /tmp/worktrees/ENG-100")
}

func TestProgressViewIgnoresAnUnknownStage(t *testing.T) {
	view := NewProgressView()
	view.ApplyUpdate(domain.StageUpdate{Stage: domain.StageID("nope"), State: domain.StateFailed})
	mustContain(t, plain(view.View()), "○ preparing")
}

func TestProgressViewShowsCancelling(t *testing.T) {
	view := NewProgressView()
	view.ShowCancelling()
	mustContain(t, plain(view.View()), "cancelling…")
}

func TestProgressViewResultLines(t *testing.T) {
	view := NewProgressView()
	view.ShowResult(domain.FlowResult{CheckoutPath: "/tmp/worktrees/ENG-100", Created: true})
	mustContain(t, plain(view.View()), "Worktree ready at /tmp/worktrees/ENG-100")

	// A reused checkout is the same promise, differently reached.
	view.ShowResult(domain.FlowResult{CheckoutPath: "/tmp/worktrees/ENG-100"})
	mustContain(t, plain(view.View()), "Worktree ready at /tmp/worktrees/ENG-100 (reused)")

}

func TestErrorViewShowsMessageNextActionAndHints(t *testing.T) {
	// The words are SPEC §4's, which is what gitrepo actually produces.
	err := lwerr.New(lwerr.NotARepo, "not inside a git repository", "run lw from inside a repository, or pass --repo <path>")
	frame := plain(NewErrorView(ErrorViewOptions{Error: err}).View())
	mustContain(t, frame, "Error")
	mustContain(t, frame, "not inside a git repository")
	mustContain(t, frame, "Next: run lw from inside a repository, or pass --repo <path>")
	mustContain(t, frame, "[Esc] close")
	mustNotContain(t, frame, "[r] retry")
}

func TestErrorViewOffersRetryWhenRetryable(t *testing.T) {
	err := lwerr.New(lwerr.LinearUnavailable, "Linear API unreachable", "Check your network")
	frame := plain(NewErrorView(ErrorViewOptions{Error: err, Retryable: true}).View())
	mustContain(t, frame, "Linear API unreachable")
	mustContain(t, frame, "Next: Check your network")
	mustContain(t, frame, "[r] retry · [Esc] close")
}

func TestSameRows(t *testing.T) {
	a := []SearchableItem{{ID: "1", Label: "One", Hint: "x"}}
	if !SameRows(a, []SearchableItem{{ID: "1", Label: "One", Hint: "x"}}) {
		t.Fatal("identical rows must compare equal")
	}
	if SameRows(a, []SearchableItem{{ID: "1", Label: "One", Hint: "y"}}) {
		t.Fatal("a changed hint is a changed row")
	}
	if SameRows(a, nil) {
		t.Fatal("a different length is a changed list")
	}
}

func TestTintBlendsTowardsTheOverlay(t *testing.T) {
	got := Tint(RGBA{0, 0, 0, 255}, RGBA{255, 255, 255, 0}, 0.35)
	want := RGBA{89, 89, 89, 255}
	if got != want {
		t.Fatalf("Tint = %+v, want %+v", got, want)
	}
	if got := Tint(RGBA{10, 20, 30, 128}, RGBA{200, 200, 200, 255}, 0); got != (RGBA{10, 20, 30, 128}) {
		t.Fatalf("alpha 0 keeps the base, got %+v", got)
	}
}
