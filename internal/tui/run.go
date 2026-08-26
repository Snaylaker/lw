package tui

import (
	"io"
	"os"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/snaylaker/lw/internal/lwerr"
)

// TerminalNextAction is what a user whose terminal cannot host the full-screen
// UI does instead: the pickers are the only part of a run that needs one.
const TerminalNextAction = "run lw from an interactive terminal, or pass --issue <IDENT>"

// uiOutput is where the full-screen UI renders, and it is stderr — never
// stdout. stdout carries exactly one thing, the worktree path (SPEC §3), so that
// `cd "$(lw)"` captures a path rather than a screenful of escape codes.
var uiOutput io.Writer = os.Stderr

// Lip Gloss has a process-global default renderer and initializes it from
// stdout. lw intentionally captures stdout for the resulting path and renders
// its TUI to stderr, so the default would incorrectly disable color in
// `cd "$(lw)"`. Bind the renderer to the same writer Bubble Tea actually uses.
//
// The lock covers the whole program because swapping Lip Gloss's global
// renderer is otherwise racy when tests run launchers in parallel.
var lipglossRendererMu sync.Mutex

func installLipglossRenderer(out io.Writer) func() {
	lipglossRendererMu.Lock()
	previous := lipgloss.DefaultRenderer()
	lipgloss.SetDefaultRenderer(lipgloss.NewRenderer(out))
	var once sync.Once
	return func() {
		once.Do(func() {
			lipgloss.SetDefaultRenderer(previous)
			lipglossRendererMu.Unlock()
		})
	}
}

// newProgram is the one place a bubbletea program is constructed, so the writer
// it renders to is decided once and can be exercised in a test.
//
// Passing out explicitly is load-bearing: bubbletea's own default is os.Stdout,
// so dropping the option here would silently send the entire UI into whatever
// captures `$(lw)`. Nothing may construct a program without going through this.
func newProgram(model tea.Model, out io.Writer, extra ...tea.ProgramOption) *tea.Program {
	return tea.NewProgram(model, append([]tea.ProgramOption{tea.WithOutput(out)}, extra...)...)
}

// RunLauncher drives the full launcher flow on the terminal. The outcome maps
// to the process exit code: cancelled is 130, a result is 0, and neither
// (Escape on the error view) is 1.
//
// It renders to uiOutput — stderr — and nowhere else. The body lives in
// runLauncher so a test can drive this exact code with a writer it can read,
// rather than asserting on the variable and hoping the call site agrees.
func RunLauncher(deps LauncherDeps) (LauncherOutcome, error) {
	return runLauncher(deps, uiOutput)
}

func runLauncher(deps LauncherDeps, out io.Writer, extra ...tea.ProgramOption) (LauncherOutcome, error) {
	restoreRenderer := installLipglossRenderer(out)
	defer restoreRenderer()

	model := NewLauncher(deps)
	program := newProgram(model, out, extra...)
	model.Send = program.Send
	if _, err := program.Run(); err != nil {
		return LauncherOutcome{}, terminalError(err)
	}
	if err := model.Err(); err != nil {
		return LauncherOutcome{}, err
	}
	return model.Outcome(), nil
}

// terminalError classifies a failure of the program loop itself. bubbletea's
// own errors — no TTY, an input it cannot read — are the one way a bare error
// could reach the reporter, and SPEC §10 has no such shape: every error carries
// a kind and a next action.
func terminalError(err error) error {
	if err == nil {
		return nil
	}
	if classified, ok := lwerr.As(err); ok {
		return classified
	}
	return lwerr.Wrap(err, lwerr.Internal,
		"the terminal UI could not start: "+err.Error(),
		TerminalNextAction,
	)
}
