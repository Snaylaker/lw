package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/snaylaker/lw/internal/lwerr"
)

// SPEC §10: no error is a dead end. A failure of the program loop — running lw
// with no TTY is the reachable one — must not leave the reporter with a bare
// error and no next action.
func TestTerminalErrorCarriesAKindAndANextAction(t *testing.T) {
	err := terminalError(errors.New("could not open a new TTY: open /dev/tty: device not configured"))

	classified, ok := lwerr.As(err)
	if !ok {
		t.Fatalf("terminalError = %v, want a *lwerr.Error", err)
	}
	if classified.Kind != lwerr.Internal {
		t.Errorf("kind = %q, want %q", classified.Kind, lwerr.Internal)
	}
	if !strings.Contains(classified.Message, "could not open a new TTY") {
		t.Errorf("message = %q, want it to keep what bubbletea said", classified.Message)
	}
	if classified.NextAction != TerminalNextAction {
		t.Errorf("next action = %q, want %q", classified.NextAction, TerminalNextAction)
	}

	// Two lines through the one reporter, and exit 1.
	var out strings.Builder
	if code := lwerr.Report(err, &out); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "error: ") || !strings.HasPrefix(lines[1], "next: ") {
		t.Errorf("report = %q, want SPEC §10's two lines", out.String())
	}
}

// An error already classified keeps its own words.
func TestTerminalErrorLeavesAClassifiedErrorAlone(t *testing.T) {
	cancelled := lwerr.NewCancelled()
	if got := terminalError(cancelled); got != error(cancelled) {
		t.Errorf("terminalError = %v, want the error it was given", got)
	}
	if terminalError(nil) != nil {
		t.Error("no failure, no error")
	}
}
