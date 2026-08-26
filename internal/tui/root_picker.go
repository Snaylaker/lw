package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type RootPickerOptions struct {
	Suggested string
	OnSubmit  func(string)
}

// RootPicker is a one-time setup screen. User-facing copy deliberately says
// "folder containing repositories" instead of exposing the config term "root".
type RootPicker struct {
	input    *input
	onSubmit func(string)
	problem  string
}

func NewRootPicker(options RootPickerOptions) *RootPicker {
	field := newInput("for example: ~/Work")
	field.SetValue(options.Suggested)
	field.Focus()
	return &RootPicker{input: field, onSubmit: options.OnSubmit}
}

func (p *RootPicker) Value() string { return p.input.Value() }

func (p *RootPicker) SetProblem(problem string) { p.problem = problem }

func (p *RootPicker) Update(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyMsg); ok {
		info := describeKey(key)
		if !info.ctrl && !info.alt && (info.name == "return" || info.name == "linefeed") {
			value := strings.TrimSpace(p.input.Value())
			if value != "" && p.onSubmit != nil {
				p.onSubmit(value)
			}
			return nil
		}
	}
	p.input.Update(msg)
	return nil
}

func (p *RootPicker) Destroy()     {}
func (p *RootPicker) SetWidth(int) {}

func (p *RootPicker) View() string {
	lines := []string{
		styleForeground.Render("Where are your repositories?"),
		styleMuted.Render("Enter the folder that contains your Git repositories."),
		styleMuted.Render("Example: for ~/Work/api and ~/Work/web, enter ~/Work."),
		styleMuted.Render("Enter continue · Esc cancel"),
		"",
		styleFocus.Render("❯ ") + p.input.View(),
	}
	if p.problem != "" {
		lines = append(lines, styleDestruct.Render(p.problem))
	}
	return strings.Join(lines, "\n")
}
