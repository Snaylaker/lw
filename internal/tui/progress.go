package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/snaylaker/lw/internal/domain"
)

// stageOrder is fixed, top to bottom.
var stageOrder = []domain.StageID{
	domain.StagePreparing,
	domain.StageCreatingWorktree,
}

// stageLabels are SPEC §3's own words for the stages, lower-case as it quotes
// them.
var stageLabels = map[domain.StageID]string{
	domain.StagePreparing:        "preparing",
	domain.StageCreatingWorktree: "creating worktree",
}

var stateGlyphs = map[domain.StageState]string{
	domain.StatePending: "○",
	domain.StateActive:  "◐",
	domain.StateDone:    "●",
	domain.StateFailed:  "✗",
	domain.StateSkipped: "-",
}

type stageRow struct {
	state  domain.StageState
	detail string
}

// ProgressView is the flow screen: one row per stage plus a footer that carries
// the cancellation notice and the final report.
type ProgressView struct {
	rows        map[domain.StageID]*stageRow
	footer      string
	footerStyle lipgloss.Style
}

func NewProgressView() *ProgressView {
	view := &ProgressView{rows: map[domain.StageID]*stageRow{}, footerStyle: styleForeground}
	for _, stage := range stageOrder {
		view.rows[stage] = &stageRow{state: domain.StatePending}
	}
	return view
}

// ApplyUpdate ignores an unknown stage silently. A detail already set is sticky
// across later updates that omit it.
func (v *ProgressView) ApplyUpdate(update domain.StageUpdate) {
	row, ok := v.rows[update.Stage]
	if !ok {
		return
	}
	row.state = update.State
	if update.Detail != "" {
		row.detail = update.Detail
	}
}

// ShowCancelling reports the abort that has been signalled; the flow still has
// to settle before the TUI closes.
func (v *ProgressView) ShowCancelling() {
	v.footer = "cancelling…"
	v.footerStyle = styleWarning
}

// ShowResult is the final summary: the worktree exists, and where it is.
func (v *ProgressView) ShowResult(result domain.FlowResult) {
	where := "Worktree ready at " + result.CheckoutPath
	if !result.Created {
		where += " (reused)"
	}
	v.footer = where
	v.footerStyle = styleSuccess
}

func (v *ProgressView) Update(tea.Msg) tea.Cmd { return nil }

func (v *ProgressView) Destroy() {}

func (v *ProgressView) SetWidth(int) {}

func (v *ProgressView) View() string {
	lines := []string{styleForeground.Render("Opening worktree"), ""}
	for _, stage := range stageOrder {
		lines = append(lines, v.renderRow(stage))
	}
	lines = append(lines, "", v.footerStyle.Render(v.footer))
	return strings.Join(lines, "\n")
}

func (v *ProgressView) renderRow(stage domain.StageID) string {
	row := v.rows[stage]
	detail := ""
	if row.detail != "" {
		detail = " — " + row.detail
	}
	content := stateGlyphs[row.state] + " " + stageLabels[stage] + detail
	switch row.state {
	case domain.StateFailed:
		return styleDestruct.Render(content)
	case domain.StateDone:
		return styleSuccess.Render(content)
	case domain.StateActive:
		return styleForeground.Render(content)
	default:
		return styleMuted.Render(content)
	}
}
