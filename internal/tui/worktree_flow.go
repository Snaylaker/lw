package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/snaylaker/lw/internal/domain"
)

// flowScreen is the progress view plus the header that names the repository.
type flowScreen struct {
	header   string
	progress *ProgressView
}

func (s *flowScreen) Update(msg tea.Msg) tea.Cmd { return s.progress.Update(msg) }
func (s *flowScreen) Destroy()                   {}
func (s *flowScreen) SetWidth(int)               {}
func (s *flowScreen) View() string {
	return styleForeground.Render(s.header) + "\n\n" + s.progress.View()
}

func (m *Launcher) startFlow(issue domain.Issue, branch domain.Branch) tea.Cmd {
	m.screen = ScreenProgress
	progress := NewProgressView()
	m.progress = progress
	repo := m.chosenRepo()
	m.show(&flowScreen{
		header:   "Repo: " + repo.Name + " — " + repo.Root,
		progress: progress,
	})

	// The flow has its own controller: it is aborted by Ctrl+C, never by a
	// screen change.
	ctx, cancel := context.WithCancel(context.Background())
	m.flowCancel = cancel
	m.flowRunning = true
	m.flowAborted = false
	m.flowToken++
	token := m.flowToken
	send := m.Send
	executeBranch := m.deps.ExecuteBranchFlow
	executeLegacy := m.deps.ExecuteFlow
	return func() tea.Msg {
		onStage := func(update domain.StageUpdate) {
			send(stageMsg{token: token, update: update})
		}
		var result domain.FlowResult
		var err error
		if executeBranch != nil {
			result, err = executeBranch(ctx, repo, issue, branch, onStage)
		} else {
			result, err = executeLegacy(ctx, repo, issue, onStage)
		}
		return flowFinishedMsg{token: token, result: result, err: err}
	}
}

func (m *Launcher) onStage(msg stageMsg) tea.Cmd {
	if m.settled || m.screen != ScreenProgress || msg.token != m.flowToken || m.flowAborted {
		return nil
	}
	if m.progress != nil {
		m.progress.ApplyUpdate(msg.update)
	}
	return nil
}

func (m *Launcher) onFlowFinished(msg flowFinishedMsg) tea.Cmd {
	if msg.token != m.flowToken {
		return nil
	}
	m.flowRunning = false
	m.flowCancel = nil
	if m.settled {
		return nil
	}
	if msg.err != nil {
		if m.flowAborted {
			m.settle(LauncherOutcome{Result: nil, Cancelled: true})
			return nil
		}
		m.handleFailure(msg.err, nil)
		return nil
	}
	result := msg.result
	if m.flowAborted {
		// The worktree was created before the abort landed; never roll back a
		// valid checkout — carry the result so the caller can report the path.
		m.settle(LauncherOutcome{Result: &result, Cancelled: true})
		return nil
	}
	return m.showDone(result)
}

// doneScreen is the success summary.
type doneScreen struct{ text string }

func (s *doneScreen) Update(tea.Msg) tea.Cmd { return nil }
func (s *doneScreen) Destroy()               {}
func (s *doneScreen) SetWidth(int)           {}
func (s *doneScreen) View() string           { return styleForeground.Render(s.text) }

func (m *Launcher) showDone(result domain.FlowResult) tea.Cmd {
	m.screen = ScreenDone
	note := ""
	if !result.Created {
		note = " (reused)"
	}
	m.show(&doneScreen{text: "✓ Worktree ready at " + result.CheckoutPath + note})
	// Parked here so the timer message itself carries nothing.
	m.doneResult = &result
	delay := m.deps.DoneClose
	if delay == 0 {
		delay = defaultDoneClose
	}
	return tea.Tick(delay, func(time.Time) tea.Msg { return doneTimerMsg{} })
}
