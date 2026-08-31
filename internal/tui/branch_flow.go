package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/snaylaker/lw/internal/domain"
	"github.com/snaylaker/lw/internal/lwerr"
)

type branchResolvedMsg struct {
	token      int
	resolution domain.BranchResolution
	err        error
}

type branchChosenMsg struct {
	token  int
	branch domain.Branch
	err    error
}

type branchLoadingView struct {
	issue domain.Issue
	repo  domain.Repo
}

func (v *branchLoadingView) Update(tea.Msg) tea.Cmd { return nil }
func (v *branchLoadingView) Destroy()               {}
func (v *branchLoadingView) SetWidth(int)           {}
func (v *branchLoadingView) View() string {
	return styleForeground.Copy().Bold(true).Render("Resolve a branch for "+v.issue.Identifier) + "\n\n" +
		styleMuted.Render("Fetching and checking branches in "+v.repo.Name+"…")
}

type BranchPicker struct {
	list     *SearchableList
	branches map[string]domain.Branch
	issue    domain.Issue
}

func NewBranchPicker(issue domain.Issue, candidates []domain.Branch, onSelect func(domain.Branch)) *BranchPicker {
	picker := &BranchPicker{branches: map[string]domain.Branch{}, issue: issue}
	picker.list = NewSearchableList(SearchableListOptions{
		Placeholder: "search branches",
		EmptyText:   "no matching branches",
		OnSelect: func(item SearchableItem) {
			if selected, ok := picker.branches[item.ID]; ok && onSelect != nil {
				onSelect(selected)
			}
		},
	})
	rows := make([]SearchableItem, 0, len(candidates))
	for _, candidate := range candidates {
		picker.branches[candidate.Name] = candidate
		hint := "local"
		if !candidate.ExistingLocal {
			hint = "origin"
		}
		rows = append(rows, SearchableItem{ID: candidate.Name, Label: candidate.Name, Hint: hint})
	}
	picker.list.SetItems(rows)
	return picker
}

func (p *BranchPicker) Update(msg tea.Msg) tea.Cmd { return p.list.Update(msg) }
func (p *BranchPicker) Destroy()                   {}
func (p *BranchPicker) SetWidth(width int)         { p.list.SetWidth(width) }
func (p *BranchPicker) FocusInput() tea.Cmd        { return p.list.FocusInput() }
func (p *BranchPicker) View() string {
	return strings.Join([]string{
		styleForeground.Copy().Bold(true).Render("Choose a branch for " + p.issue.Identifier),
		styleMuted.Render("Several existing branches contain this ticket."),
		styleMuted.Render("type to search · ↑/↓ move · Enter select · Esc back"),
		"",
		p.list.View(),
	}, "\n")
}

type BranchInput struct {
	input      *input
	issue      domain.Issue
	repo       domain.Repo
	onSubmit   func(string)
	problem    string
	submitting bool
}

func NewBranchInput(issue domain.Issue, repo domain.Repo, suggested string, onSubmit func(string)) *BranchInput {
	field := newInput("enter a branch name")
	field.SetValue(suggested)
	field.Focus()
	field.SelectAll()
	return &BranchInput{input: field, issue: issue, repo: repo, onSubmit: onSubmit}
}

func (p *BranchInput) SetProblem(problem string) { p.problem, p.submitting = problem, false }
func (p *BranchInput) Update(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyMsg); ok {
		info := describeKey(key)
		if !info.ctrl && !info.alt && (info.name == "return" || info.name == "linefeed") {
			value := strings.TrimSpace(p.input.Value())
			if value != "" && p.onSubmit != nil && !p.submitting {
				p.problem = ""
				p.submitting = true
				p.onSubmit(value)
			}
			return nil
		}
	}
	p.input.Update(msg)
	return nil
}
func (p *BranchInput) Destroy()     {}
func (p *BranchInput) SetWidth(int) {}
func (p *BranchInput) View() string {
	lines := []string{
		styleForeground.Copy().Bold(true).Render("Name the branch for " + p.issue.Identifier),
		styleMuted.Render("Repo: " + p.repo.Name + " — " + p.repo.Root),
		styleMuted.Render("Linear's suggestion is selected. Type to replace it, or use ←/→ to edit."),
		styleMuted.Render("Ctrl+U clear · Enter continue · Esc back"),
		"",
		styleFocus.Render("❯ ") + p.input.View(),
	}
	if p.submitting {
		lines = append(lines, styleMuted.Render("checking branch…"))
	}
	if p.problem != "" {
		lines = append(lines, styleDestruct.Render(p.problem))
	}
	return strings.Join(lines, "\n")
}

func (m *Launcher) prepareBranch(issue domain.Issue) tea.Cmd {
	repo := m.chosenRepo()
	if m.deps.ResolveBranch == nil {
		return m.startFlow(issue, domain.Branch{Name: issue.Identifier})
	}
	m.screen = ScreenBranchLoading
	m.show(&branchLoadingView{issue: issue, repo: repo})
	ctx, token := m.beginLoad()
	resolve := m.deps.ResolveBranch
	return func() tea.Msg {
		resolution, err := resolve(ctx, repo, issue)
		return branchResolvedMsg{token: token, resolution: resolution, err: err}
	}
}

func (m *Launcher) onBranchResolved(msg branchResolvedMsg) tea.Cmd {
	if msg.token != m.loadToken || m.screen != ScreenBranchLoading {
		return nil
	}
	m.cancelLoad = nil
	if msg.err != nil {
		m.handleFailure(msg.err, nil)
		return nil
	}
	issue := *m.currentIssue
	if msg.resolution.Selected != nil {
		return m.startFlow(issue, *msg.resolution.Selected)
	}
	if len(msg.resolution.Candidates) > 0 {
		m.screen = ScreenBranches
		picker := NewBranchPicker(issue, msg.resolution.Candidates, func(selected domain.Branch) {
			m.enqueue(m.startFlow(issue, selected))
		})
		m.show(picker)
		m.branchPicker = picker
		return picker.FocusInput()
	}
	m.screen = ScreenBranchInput
	input := NewBranchInput(issue, m.chosenRepo(), msg.resolution.Suggested, func(value string) {
		m.enqueue(m.chooseBranch(value))
	})
	m.show(input)
	m.branchInput = input
	return nil
}

func (m *Launcher) chooseBranch(name string) tea.Cmd {
	if m.deps.ChooseBranch == nil {
		return m.startFlow(*m.currentIssue, domain.Branch{Name: name})
	}
	ctx, token := m.beginLoad()
	choose := m.deps.ChooseBranch
	repo := m.chosenRepo()
	return func() tea.Msg {
		selected, err := choose(ctx, repo, name)
		return branchChosenMsg{token: token, branch: selected, err: err}
	}
}

func (m *Launcher) onBranchChosen(msg branchChosenMsg) tea.Cmd {
	if msg.token != m.loadToken || m.screen != ScreenBranchInput || m.branchInput == nil {
		return nil
	}
	m.cancelLoad = nil
	if msg.err != nil {
		if userErr, ok := lwerr.As(msg.err); ok && userErr.Kind == lwerr.WorktreeConflict {
			m.branchInput.SetProblem(userErr.Message)
			return nil
		}
		m.handleFailure(msg.err, nil)
		return nil
	}
	return m.startFlow(*m.currentIssue, msg.branch)
}
