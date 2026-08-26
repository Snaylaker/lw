package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/snaylaker/lw/internal/lwerr"
)

func TestReportPrintsTheOneErrorFormat(t *testing.T) {
	var stderr bytes.Buffer
	code := Report(lwerr.New(lwerr.LinearUnavailable, "Linear is unreachable.", "check your connection and re-run"), &stderr)

	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	want := "error: Linear is unreachable.\nnext: check your connection and re-run\n"
	if stderr.String() != want {
		t.Errorf("stderr = %q, want %q", stderr.String(), want)
	}
}

func TestReportIsSilentOnCancellation(t *testing.T) {
	var stderr bytes.Buffer
	code := Report(lwerr.NewCancelled(), &stderr)

	if code != 130 {
		t.Errorf("code = %d, want 130", code)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want nothing printed", stderr.String())
	}
}

func TestReportPrintsTheHelpAfterAUsageMessage(t *testing.T) {
	var stderr bytes.Buffer
	code := Report(UsageError("unknown flag --nope"), &stderr)

	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	want := "error: unknown flag --nope\n\n" + helpText
	if stderr.String() != want {
		t.Errorf("stderr = %q, want %q", stderr.String(), want)
	}
	// The next action is not printed: the help text is the next action.
	if strings.Contains(stderr.String(), "next: ") {
		t.Error("a usage error printed a next: line as well as the help text")
	}
}

func TestReportNilIsSuccess(t *testing.T) {
	var stderr bytes.Buffer
	if code := Report(nil, &stderr); code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want nothing printed", stderr.String())
	}
}

// SPEC §10's two lines are not conditional on the error's provenance: a plain
// error still prints a next action.
func TestReportHandlesAPlainError(t *testing.T) {
	var stderr bytes.Buffer
	code := Report(errors.New("boom"), &stderr)

	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	want := "error: boom\nnext: " + lwerr.FallbackNextAction + "\n"
	if stderr.String() != want {
		t.Errorf("stderr = %q, want %q", stderr.String(), want)
	}
}

// A wrapped cancellation is still a cancellation: 130, and nothing printed.
func TestReportUnwrapsToFindCancellation(t *testing.T) {
	var stderr bytes.Buffer
	wrapped := lwerr.Wrap(lwerr.NewCancelled(), lwerr.Cancelled, "operation cancelled", "nothing to do")
	if code := Report(wrapped, &stderr); code != 130 {
		t.Errorf("code = %d, want 130", code)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want nothing printed", stderr.String())
	}
}
