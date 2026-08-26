package tui

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/snaylaker/lw/internal/domain"
)

// quitAfterPaint renders one line and then quits, which is enough to prove where
// a program's output went.
type quitAfterPaint struct{}

func (quitAfterPaint) Init() tea.Cmd {
	return tea.Tick(time.Millisecond, func(time.Time) tea.Msg { return tea.Quit() })
}

func (m quitAfterPaint) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(tea.QuitMsg); ok {
		return m, tea.Quit
	}
	return m, nil
}

func (quitAfterPaint) View() string { return "PAINTED-BY-THE-UI" }

// Lip Gloss must inspect the writer Bubble Tea renders to. If it falls back to
// captured stdout, `cd "$(lw)"` incorrectly disables color in the stderr TUI.
func TestPackageStylesUseTheStderrBoundRendererAtRenderTime(t *testing.T) {
	// A buffer is not a TTY, so force the minimum ANSI profile exactly as a
	// terminal would provide. This catches styles created with NewStyle during
	// package initialization: those stay bound to stdout and ignore the renderer
	// installed for stderr when lw runs inside command substitution.
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR", "1")
	t.Setenv("CLICOLOR_FORCE", "1")
	var painted bytes.Buffer
	restore := installLipglossRenderer(&painted)
	defer restore()

	if got := styleFocus.Render("focused"); !strings.Contains(got, "\x1b[") {
		t.Fatalf("focused style has no ANSI color through the TUI renderer: %q", got)
	}
	if got := styleCursor.Render("cursor"); !strings.Contains(got, "\x1b[") {
		t.Fatalf("cursor style has no ANSI styling through the TUI renderer: %q", got)
	}
}

func TestLipGlossUsesTheSameWriterAsBubbleTea(t *testing.T) {
	previous := lipgloss.DefaultRenderer()
	var painted bytes.Buffer

	restore := installLipglossRenderer(&painted)
	if got := lipgloss.DefaultRenderer().Output().Writer(); got != io.Writer(&painted) {
		restore()
		t.Fatalf("Lip Gloss output = %T, want the TUI writer", got)
	}
	restore()

	if got := lipgloss.DefaultRenderer(); got != previous {
		t.Fatal("the process-global Lip Gloss renderer was not restored")
	}
}

// SPEC §3: stdout carries exactly one line, the worktree path, so `cd "$(lw)"`
// works. The full-screen UI must therefore render somewhere else.
//
// This runs a real bubbletea program through newProgram — the one construction
// path RunLauncher uses — and proves the frame lands in the writer it was given.
// Drop tea.WithOutput from newProgram and bubbletea falls back to os.Stdout, the
// buffer stays empty, and this fails. That is the regression that would silently
// break every command substitution.
func TestTheUIRendersToTheWriterItIsGivenAndNotStdout(t *testing.T) {
	var painted bytes.Buffer

	program := newProgram(quitAfterPaint{}, &painted, tea.WithInput(strings.NewReader("")))
	if _, err := program.Run(); err != nil {
		t.Fatalf("program.Run: %v", err)
	}

	if !strings.Contains(painted.String(), "PAINTED-BY-THE-UI") {
		t.Fatalf("the UI did not render into the writer it was handed (got %q); "+
			"bubbletea defaults to os.Stdout, so this means the frame went to stdout", painted.String())
	}
}

// The whole point, driven through the real launcher body: RunLauncher's own code
// path renders into the writer it was handed. Replace newProgram(model, out)
// with a bare tea.NewProgram(model) inside runLauncher and bubbletea falls back
// to os.Stdout, this buffer stays empty, and the test fails — which is the
// regression that would put the entire UI inside `$(lw)`.
func TestTheLauncherBodyRendersIntoTheWriterItIsGiven(t *testing.T) {
	var painted bytes.Buffer

	deps := LauncherDeps{
		Repo: domain.Repo{Root: "/repos/acme-api", Name: "acme-api"},
		SearchIssues: func(context.Context, string) ([]domain.Issue, error) {
			return nil, nil
		},
		ExecuteFlow: func(context.Context, domain.Repo, domain.Issue, func(domain.StageUpdate)) (domain.FlowResult, error) {
			return domain.FlowResult{}, nil
		},
	}

	// Ctrl+C on the first screen cancels, so the program paints and exits.
	if _, err := runLauncher(deps, &painted, tea.WithInput(strings.NewReader("\x03"))); err != nil {
		t.Fatalf("runLauncher: %v", err)
	}

	if painted.Len() == 0 {
		t.Fatal("the launcher rendered nothing into the writer it was handed; " +
			"bubbletea defaults to os.Stdout, so the UI went to stdout")
	}
	if !strings.Contains(painted.String(), "Find a Linear issue") {
		t.Errorf("the launcher's own frames did not reach the writer: %q", painted.String())
	}
}

// And the writer RunLauncher hands it is stderr, never stdout.
func TestTheLauncherRendersToStderr(t *testing.T) {
	if uiOutput == os.Stdout {
		t.Fatal("the UI renders to stdout; `cd \"$(lw)\"` would capture escape codes instead of the worktree path")
	}
	if uiOutput != io.Writer(os.Stderr) {
		t.Fatalf("uiOutput = %v, want os.Stderr", uiOutput)
	}
}
