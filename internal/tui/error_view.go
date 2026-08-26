package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/snaylaker/lw/internal/lwerr"
)

type ErrorViewOptions struct {
	Error     *lwerr.Error
	Retryable bool
}

// ErrorView is the bordered failure box. Its hints line is the only place that
// says whether a retry is on offer.
type ErrorView struct {
	message string
	next    string
	hints   string
	width   int
}

func NewErrorView(options ErrorViewOptions) *ErrorView {
	hints := "[Esc] close"
	if options.Retryable {
		hints = "[r] retry · [Esc] close"
	}
	return &ErrorView{
		message: options.Error.Message,
		next:    "Next: " + options.Error.NextAction,
		hints:   hints,
	}
}

func (v *ErrorView) Update(tea.Msg) tea.Cmd { return nil }

func (v *ErrorView) Destroy() {}

func (v *ErrorView) SetWidth(width int) { v.width = width }

func (v *ErrorView) View() string {
	body := []string{
		styleForeground.Render(v.message),
		"",
		styleForeground.Render(v.next),
		"",
		styleMuted.Render(v.hints),
	}
	inner := 0
	for _, line := range body {
		if w := lipgloss.Width(line); w > inner {
			inner = w
		}
	}
	if v.width > 4 && v.width-4 > inner {
		inner = v.width - 4
	}

	border := Theme.Borders.Style
	edge := lipgloss.NewStyle().Foreground(Theme.Colors.Destructive)
	title := edge.Render(border.TopLeft + border.Top + "Error")
	fill := inner + 2 - lipgloss.Width("Error") - 1
	if fill < 0 {
		fill = 0
	}
	lines := []string{title + edge.Render(strings.Repeat(border.Top, fill)+border.TopRight)}
	for _, line := range body {
		pad := inner - lipgloss.Width(line)
		if pad < 0 {
			pad = 0
		}
		lines = append(lines, edge.Render(border.Left)+" "+line+strings.Repeat(" ", pad)+" "+edge.Render(border.Right))
	}
	lines = append(lines, edge.Render(border.BottomLeft+strings.Repeat(border.Bottom, inner+2)+border.BottomRight))
	return strings.Join(lines, "\n")
}
