package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/snaylaker/lw/internal/domain"
)

type ProjectPickerOptions struct {
	OnSelect    func(domain.Project)
	PinnedIDs   []string
	OnTogglePin func(domain.Project) ([]string, error)
}

// ProjectPicker is the optional local project browser. Linear supplies one
// bounded page; typing filters that page without another network request.
type ProjectPicker struct {
	list         *SearchableList
	projects     []domain.Project
	projectsByID map[string]domain.Project
	pinnedIDs    []string
	pinned       map[string]bool
	onTogglePin  func(domain.Project) ([]string, error)
	status       string
	note         string
}

func NewProjectPicker(options ProjectPickerOptions) *ProjectPicker {
	picker := &ProjectPicker{
		projectsByID: map[string]domain.Project{},
		onTogglePin:  options.OnTogglePin,
	}
	picker.setPinned(options.PinnedIDs)
	picker.list = NewSearchableList(SearchableListOptions{
		Placeholder: "project name",
		EmptyText:   "no active projects",
		LoadingText: "loading Linear projects…",
		OnSelect: func(item SearchableItem) {
			if project, ok := picker.projectsByID[item.ID]; ok && options.OnSelect != nil {
				options.OnSelect(project)
			}
		},
	})
	return picker
}

func (p *ProjectPicker) Query() string         { return p.list.Query() }
func (p *ProjectPicker) SetQuery(query string) { p.list.SetQuery(query) }
func (p *ProjectPicker) SetLoading()           { p.status = ""; p.list.SetLoading(true) }
func (p *ProjectPicker) FocusInput() tea.Cmd   { return p.list.FocusInput() }
func (p *ProjectPicker) SetWidth(width int)    { p.list.SetWidth(width) }
func (p *ProjectPicker) Update(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyMsg); ok {
		info := describeKey(key)
		if info.ctrl && !info.alt && info.name == "p" {
			item, selected := p.list.SelectedItem()
			project, found := p.projectsByID[item.ID]
			if !selected || !found || p.onTogglePin == nil {
				return nil
			}
			ids, err := p.onTogglePin(project)
			if err != nil {
				p.note = "could not update pin"
				p.paintStatus()
				return nil
			}
			p.note = ""
			p.setPinned(ids)
			p.SetProjects(p.projects)
			return nil
		}
	}
	return p.list.Update(msg)
}
func (p *ProjectPicker) Destroy()                            {}
func (p *ProjectPicker) Rows() []SearchableItem              { return p.list.Rows() }
func (p *ProjectPicker) Highlighted() (SearchableItem, bool) { return p.list.SelectedItem() }
func (p *ProjectPicker) StatusLine() string                  { return p.status }

func (p *ProjectPicker) SetProjects(projects []domain.Project) {
	p.projects = append(p.projects[:0], projects...)
	p.projectsByID = make(map[string]domain.Project, len(projects))
	for _, project := range projects {
		p.projectsByID[project.ID] = project
	}
	ranked := make([]domain.Project, 0, len(projects))
	for _, id := range p.pinnedIDs {
		if project, ok := p.projectsByID[id]; ok {
			ranked = append(ranked, project)
		}
	}
	for _, project := range projects {
		if !p.pinned[project.ID] {
			ranked = append(ranked, project)
		}
	}
	rows := make([]SearchableItem, 0, len(ranked))
	for _, project := range ranked {
		label := TruncateGraphemes(project.Name, 80)
		if p.pinned[project.ID] {
			label = pinMarker + label
		}
		rows = append(rows, SearchableItem{ID: project.ID, Label: label, Hint: project.StatusName})
	}
	p.list.SetItems(rows)
	p.paintStatus()
}

func (p *ProjectPicker) setPinned(ids []string) {
	p.pinnedIDs = append(p.pinnedIDs[:0], ids...)
	p.pinned = make(map[string]bool, len(ids))
	for _, id := range ids {
		p.pinned[id] = true
	}
}

func (p *ProjectPicker) paintStatus() {
	count := 0
	for _, project := range p.projects {
		if p.pinned[project.ID] {
			count++
		}
	}
	if len(p.projects) == 1 {
		p.status = "1 project"
	} else {
		p.status = fmt.Sprintf("%d projects", len(p.projects))
	}
	if count > 0 {
		p.status += fmt.Sprintf(" · %d pinned", count)
	}
	if p.note != "" {
		p.status += " · " + p.note
	}
}

func (p *ProjectPicker) View() string {
	return strings.Join([]string{
		styleForeground.Copy().Bold(true).Render("Find a Linear project"),
		browserTabs(browserProjects),
		shortcutLine(
			shortcut{key: "↑↓", label: "move"},
			shortcut{key: "Enter", label: "open issues"},
			shortcut{key: "Ctrl+P", label: "pin / unpin"},
		),
		shortcutLine(
			shortcut{key: "Ctrl+R", label: "reload"},
			shortcut{key: "Esc", label: "cancel"},
		),
		styleMuted.Render(p.status),
		"",
		p.list.View(),
	}, "\n")
}
