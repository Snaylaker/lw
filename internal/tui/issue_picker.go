package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/snaylaker/lw/internal/domain"
)

const issueSearchDebounce = 450 * time.Millisecond

// Two characters avoid spending Linear's search rate limit on a single key and
// still allow a team key such as DEMO to be the complete query.
func searchQueryReady(query string) bool {
	return utf8.RuneCountInString(strings.TrimSpace(query)) >= 2
}

type IssuePickerOptions struct {
	OnSelect func(domain.Issue)
	// Local turns the picker into a project issue browser: rows are loaded once
	// and typing filters them locally rather than starting workspace searches.
	Local bool
	Title string
}

// IssuePicker searches every visible active issue in the workspace. Linear
// ranks the results; the list therefore displays server order without applying
// another local text filter.
type IssuePicker struct {
	list       *SearchableList
	local      bool
	title      string
	issuesByID map[string]domain.Issue
	rows       []SearchableItem
	painted    bool
	status     string
	generation int
}

func NewIssuePicker(options IssuePickerOptions) *IssuePicker {
	title := options.Title
	if title == "" {
		title = "Find a Linear issue"
	}
	picker := &IssuePicker{issuesByID: map[string]domain.Issue{}, local: options.Local, title: title}
	filter := FilterFunc(func(_ string, items []SearchableItem) []SearchableItem {
		return append([]SearchableItem(nil), items...)
	})
	if options.Local {
		filter = DefaultFilter
	}
	picker.list = NewSearchableList(SearchableListOptions{
		Placeholder: "issue identifier or title",
		EmptyText:   "type at least 2 characters",
		LoadingText: "searching Linear…",
		// Search results may be semantic matches whose title does not literally
		// contain the query. Preserve Linear's relevance ranking verbatim.
		Filter: filter,
		OnSelect: func(item SearchableItem) {
			if issue, ok := picker.issuesByID[item.ID]; ok && options.OnSelect != nil {
				options.OnSelect(issue)
			}
		},
	})
	return picker
}

func (p *IssuePicker) Query() string { return p.list.Query() }

// SetQuery restores the user's search after returning from repository choice.
func (p *IssuePicker) SetQuery(query string) { p.list.SetQuery(query) }

func (p *IssuePicker) SetSearching() {
	p.status = ""
	p.list.SetLoading(true)
}

func (p *IssuePicker) SetLoading() { p.SetSearching() }

func (p *IssuePicker) SetIssues(issues []domain.Issue) {
	p.list.SetEmptyText("no matching active issues")
	p.issuesByID = make(map[string]domain.Issue, len(issues))
	rows := make([]SearchableItem, 0, len(issues))
	for _, issue := range issues {
		p.issuesByID[issue.ID] = issue
		hint := issue.StateName
		scope := issue.ProjectName
		if scope == "" {
			scope = issue.TeamName
			if scope == "" {
				scope = issue.TeamKey
			}
		}
		if hint != "" && scope != "" {
			hint += " · " + scope
		} else if scope != "" {
			hint = scope
		}
		rows = append(rows, SearchableItem{
			ID:    issue.ID,
			Label: issue.Identifier + " " + TruncateGraphemes(issue.Title, 80),
			Hint:  hint,
		})
	}
	p.rows, p.painted = applyRows(p.list, p.rows, rows, p.painted)
	if len(issues) == 1 {
		p.status = "1 result"
	} else {
		p.status = fmt.Sprintf("%d results", len(issues))
	}
}

func (p *IssuePicker) Rows() []SearchableItem              { return p.list.Rows() }
func (p *IssuePicker) Highlighted() (SearchableItem, bool) { return p.list.SelectedItem() }
func (p *IssuePicker) ListStatus() string                  { return p.list.StatusText() }
func (p *IssuePicker) StatusLine() string                  { return p.status }
func (p *IssuePicker) Breadcrumb() string                  { return p.title }
func (p *IssuePicker) FocusInput() tea.Cmd                 { return p.list.FocusInput() }
func (p *IssuePicker) SetWidth(width int)                  { p.list.SetWidth(width) }
func (p *IssuePicker) Destroy()                            {}

func (p *IssuePicker) Update(msg tea.Msg) tea.Cmd {
	before := p.list.Query()
	listCmd := p.list.Update(msg)
	if p.local {
		return listCmd
	}
	after := p.list.Query()
	if after == before {
		return listCmd
	}

	p.generation++
	generation := p.generation
	query := after
	p.status = ""
	if !searchQueryReady(query) {
		p.issuesByID = map[string]domain.Issue{}
		p.rows = nil
		p.painted = false
		p.list.SetEmptyText("type at least 2 characters")
		p.list.SetItems(nil)
		return listCmd
	}
	p.SetSearching()
	debounce := tea.Tick(issueSearchDebounce, func(time.Time) tea.Msg {
		return issueSearchDueMsg{query: query, generation: generation}
	})
	if listCmd == nil {
		return debounce
	}
	return tea.Batch(listCmd, debounce)
}

func (p *IssuePicker) View() string {
	actions := []shortcut{
		{key: "↑↓", label: "move"},
		{key: "Enter", label: "select"},
		{key: "Ctrl+R", label: "search again"},
		{key: "Esc", label: "cancel"},
	}
	if p.local {
		actions[2].label = "reload"
		actions[3].label = "back"
	}
	lines := []string{
		styleForeground.Copy().Bold(true).Render(p.title),
		browserTabs(browserIssues),
		shortcutLine(actions...),
		styleMuted.Render(p.status),
		"",
		p.list.View(),
	}
	return strings.Join(lines, "\n")
}
