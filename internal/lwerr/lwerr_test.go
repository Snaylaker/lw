package lwerr

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestReportPrintsTheMessageAndNextActionAndExitsOne(t *testing.T) {
	var out strings.Builder
	code := Report(New(NotARepo, "not inside a git repository", "run lw from inside a repository, or pass --repo <path>"), &out)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	want := "error: not inside a git repository\nnext: run lw from inside a repository, or pass --repo <path>\n"
	if out.String() != want {
		t.Errorf("output = %q, want %q", out.String(), want)
	}
}

// A cancellation is the user's own doing: exit 130 and say nothing.
func TestReportOfACancellationIsSilentAndExits130(t *testing.T) {
	var out strings.Builder
	if code := Report(NewCancelled(), &out); code != 130 {
		t.Errorf("exit code = %d, want 130", code)
	}
	if out.String() != "" {
		t.Errorf("output = %q, want nothing", out.String())
	}
}

// SPEC §10 makes both lines unconditional — "no error is a dead end" — so even
// an error nothing in this tool classified gets a next action.
func TestReportOfAPlainErrorStillCarriesANextLine(t *testing.T) {
	var out strings.Builder
	if code := Report(errors.New("boom"), &out); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	want := "error: boom\nnext: " + FallbackNextAction + "\n"
	if out.String() != want {
		t.Errorf("output = %q, want %q", out.String(), want)
	}
}

// The same rule for a *Error somebody built without one.
func TestReportFillsInAMissingNextAction(t *testing.T) {
	var out strings.Builder
	Report(New(Internal, "boom", ""), &out)
	want := "error: boom\nnext: " + FallbackNextAction + "\n"
	if out.String() != want {
		t.Errorf("output = %q, want %q", out.String(), want)
	}
}

func TestAsAndIsSeeThroughWrapping(t *testing.T) {
	cause := errors.New("no route to host")
	err := Wrap(cause, LinearUnavailable, "Linear is unreachable", "check your network")
	wrapped := fmt.Errorf("listing projects: %w", err)

	got, ok := As(wrapped)
	if !ok || got != err {
		t.Fatalf("As = (%v, %v), want the wrapped *Error", got, ok)
	}
	if !Is(wrapped, LinearUnavailable) || Is(wrapped, ConfigInvalid) {
		t.Error("Is must match on kind alone")
	}
	if !errors.Is(wrapped, cause) {
		t.Error("the cause must stay reachable")
	}
	if _, ok := As(cause); ok {
		t.Error("a plain error is not a *Error")
	}
}

// Section 12 enumerates the kinds. The set is the contract, not just the
// spelling of each member: an extra kind is a distinction the spec does not
// make, and a missing one is a failure the tool cannot describe.
func TestKindsAreExactlyThoseInSection10(t *testing.T) {
	want := []Kind{
		"auth_required",
		"linear_unavailable",
		"provider_unavailable",
		"not_a_repo",
		"config_invalid",
		"worktree_conflict",
		"cancelled",
		"internal",
	}
	if len(Kinds) != len(want) {
		t.Fatalf("Kinds = %v, want the %d kinds of SPEC §10", Kinds, len(want))
	}
	for i, kind := range want {
		if Kinds[i] != kind {
			t.Errorf("Kinds[%d] = %q, want %q", i, Kinds[i], kind)
		}
	}
	// An API key does not expire, so there is no kind for a stale one; a key
	// Linear refuses is auth_required, like having none.
	for _, kind := range Kinds {
		if kind == "auth_expired" {
			t.Error("auth_expired is not a kind in SPEC §10")
		}
	}
}
