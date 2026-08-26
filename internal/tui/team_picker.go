package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/snaylaker/lw/internal/domain"
)

type TeamPickerOptions struct {
	OnSelect    func(domain.Team)
	PinnedIDs   []string
	OnTogglePin func(domain.Team) ([]string, error)
}

// TeamPicker gives people who do not remember a team key a browsable route to
// the same active issue list available by typing that key in workspace search.
type TeamPicker struct {
	list        *SearchableList
	teams       []domain.Team
	teamsByID   map[string]domain.Team
	pinnedIDs   []string
	pinned      map[string]bool
	onTogglePin func(domain.Team) ([]string, error)
	status      string
	note        string
}

func NewTeamPicker(options TeamPickerOptions) *TeamPicker {
	picker := &TeamPicker{
		teamsByID:   map[string]domain.Team{},
		onTogglePin: options.OnTogglePin,
	}
	picker.setPinned(options.PinnedIDs)
	picker.list = NewSearchableList(SearchableListOptions{
		Placeholder: "team name or key",
		EmptyText:   "no active teams",
		LoadingText: "loading Linear teams…",
		OnSelect: func(item SearchableItem) {
			if team, ok := picker.teamsByID[item.ID]; ok && options.OnSelect != nil {
				options.OnSelect(team)
			}
		},
	})
	return picker
}

func (p *TeamPicker) Query() string         { return p.list.Query() }
func (p *TeamPicker) SetQuery(query string) { p.list.SetQuery(query) }
func (p *TeamPicker) SetLoading()           { p.status = ""; p.list.SetLoading(true) }
func (p *TeamPicker) FocusInput() tea.Cmd   { return p.list.FocusInput() }
func (p *TeamPicker) SetWidth(width int)    { p.list.SetWidth(width) }
func (p *TeamPicker) Update(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyMsg); ok {
		info := describeKey(key)
		if info.ctrl && !info.alt && info.name == "p" {
			item, selected := p.list.SelectedItem()
			team, found := p.teamsByID[item.ID]
			if !selected || !found || p.onTogglePin == nil {
				return nil
			}
			ids, err := p.onTogglePin(team)
			if err != nil {
				p.note = "could not update pin"
				p.paintStatus()
				return nil
			}
			p.note = ""
			p.setPinned(ids)
			p.SetTeams(p.teams)
			return nil
		}
	}
	return p.list.Update(msg)
}
func (p *TeamPicker) Destroy()                            {}
func (p *TeamPicker) Rows() []SearchableItem              { return p.list.Rows() }
func (p *TeamPicker) Highlighted() (SearchableItem, bool) { return p.list.SelectedItem() }

func (p *TeamPicker) SetTeams(teams []domain.Team) {
	p.teams = append(p.teams[:0], teams...)
	p.teamsByID = make(map[string]domain.Team, len(teams))
	for _, team := range teams {
		p.teamsByID[team.ID] = team
	}
	ranked := make([]domain.Team, 0, len(teams))
	for _, id := range p.pinnedIDs {
		if team, ok := p.teamsByID[id]; ok {
			ranked = append(ranked, team)
		}
	}
	for _, team := range teams {
		if !p.pinned[team.ID] {
			ranked = append(ranked, team)
		}
	}
	rows := make([]SearchableItem, 0, len(ranked))
	for _, team := range ranked {
		label := TruncateGraphemes(team.Name, 80)
		if p.pinned[team.ID] {
			label = pinMarker + label
		}
		rows = append(rows, SearchableItem{ID: team.ID, Label: label, Hint: team.Key})
	}
	p.list.SetItems(rows)
	p.paintStatus()
}

func (p *TeamPicker) setPinned(ids []string) {
	p.pinnedIDs = append(p.pinnedIDs[:0], ids...)
	p.pinned = make(map[string]bool, len(ids))
	for _, id := range ids {
		p.pinned[id] = true
	}
}

func (p *TeamPicker) paintStatus() {
	count := 0
	for _, team := range p.teams {
		if p.pinned[team.ID] {
			count++
		}
	}
	if len(p.teams) == 1 {
		p.status = "1 team"
	} else {
		p.status = fmt.Sprintf("%d teams", len(p.teams))
	}
	if count > 0 {
		p.status += fmt.Sprintf(" · %d pinned", count)
	}
	if p.note != "" {
		p.status += " · " + p.note
	}
}

func (p *TeamPicker) View() string {
	return strings.Join([]string{
		styleForeground.Copy().Bold(true).Render("Find a Linear team"),
		browserTabs(browserTeams),
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
