package tui

import (
	"math"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SearchableItem is one row of a list.
type SearchableItem struct {
	ID    string
	Label string
	Hint  string
}

// FilterFunc narrows (and may reorder) the source rows for a query.
type FilterFunc func(query string, items []SearchableItem) []SearchableItem

type SearchableListOptions struct {
	Placeholder string
	// EmptyText is shown when the source list itself is empty, before any filtering.
	EmptyText   string
	LoadingText string
	Filter      FilterFunc
	OnSelect    func(SearchableItem)
}

const (
	visibleRows    = 6
	fastScrollStep = 5

	defaultPlaceholder = "type to search"
	defaultEmptyText   = "nothing to pick"
	defaultLoadingText = "loading…"
	// noMatchesText is a literal, not configurable.
	noMatchesText = "no matches"

	inputMaxLength = 1000
)

// DefaultFilter is a case-insensitive substring match over label and hint. The
// query is never compiled into a regex, and the incoming order is kept.
func DefaultFilter(query string, items []SearchableItem) []SearchableItem {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return append([]SearchableItem(nil), items...)
	}
	out := make([]SearchableItem, 0, len(items))
	for _, item := range items {
		if strings.Contains(strings.ToLower(item.Label), needle) ||
			(item.Hint != "" && strings.Contains(strings.ToLower(item.Hint), needle)) {
			out = append(out, item)
		}
	}
	return out
}

// SearchableList is a search input plus an option list. Focus lives on the
// input; navigation keys are intercepted before the input sees them, which is
// why j and k always type into the query instead of moving the selection.
type SearchableList struct {
	input       *input
	filterFn    FilterFunc
	onSelect    func(SearchableItem)
	emptyText   string
	loadingText string

	items    []SearchableItem
	filtered []SearchableItem
	selected int
	scroll   int
	loading  bool

	status        string
	statusVisible bool
	listVisible   bool
	width         int
}

func NewSearchableList(options SearchableListOptions) *SearchableList {
	placeholder := options.Placeholder
	if placeholder == "" {
		placeholder = defaultPlaceholder
	}
	list := &SearchableList{
		input:       newInput(placeholder),
		filterFn:    options.Filter,
		onSelect:    options.OnSelect,
		emptyText:   options.EmptyText,
		loadingText: options.LoadingText,
	}
	if list.filterFn == nil {
		list.filterFn = DefaultFilter
	}
	if list.emptyText == "" {
		list.emptyText = defaultEmptyText
	}
	if list.loadingText == "" {
		list.loadingText = defaultLoadingText
	}
	list.refresh()
	return list
}

// Query is the raw input value, not trimmed.
func (l *SearchableList) Query() string { return l.input.Value() }

func (l *SearchableList) ClearQuery() { l.SetQuery("") }

// SetQuery replaces the input without simulating key presses. It is used when
// returning from repository selection to restore the workspace search.
func (l *SearchableList) SetQuery(query string) {
	l.input.SetValue(query)
	l.selected = 0
	l.refresh()
}

func (l *SearchableList) SetEmptyText(text string) {
	l.emptyText = text
	l.refresh()
}

// SetItems replaces the source rows, clears loading and resets the highlight to
// the first row.
func (l *SearchableList) SetItems(items []SearchableItem) {
	l.items = append([]SearchableItem(nil), items...)
	l.loading = false
	l.selected = 0
	l.refresh()
}

func (l *SearchableList) SetLoading(loading bool) {
	l.loading = loading
	l.refresh()
}

func (l *SearchableList) FocusInput() tea.Cmd {
	l.input.Focus()
	return nil
}

func (l *SearchableList) SetWidth(width int) { l.width = width }

// Rows is what the list is showing: the source rows after the filter, in
// display order. It is the state every frame is drawn from, so a test can
// assert ordering and filtering without reading a rendered frame (SPEC §12).
func (l *SearchableList) Rows() []SearchableItem {
	return append([]SearchableItem(nil), l.filtered...)
}

// SelectedIndex is where the highlight sits in Rows, -1 when no row is
// highlighted at all.
func (l *SearchableList) SelectedIndex() int {
	if l.selected < 0 || l.selected >= len(l.filtered) {
		return -1
	}
	return l.selected
}

// StatusText is the single line shown in place of the rows — the loading text,
// the empty text, or "no matches". It is empty while rows are showing.
func (l *SearchableList) StatusText() string {
	if !l.statusVisible {
		return ""
	}
	return l.status
}

// SelectedItem is the highlighted row, if the list has any.
func (l *SearchableList) SelectedItem() (SearchableItem, bool) {
	if l.selected < 0 || l.selected >= len(l.filtered) {
		return SearchableItem{}, false
	}
	return l.filtered[l.selected], true
}

// SelectItemByID puts the highlight back on a row after SetItems wiped it. An
// id that vanished from the new rows leaves the selection at the top, silently.
func (l *SearchableList) SelectItemByID(id string) {
	if id == "" {
		return
	}
	for i, item := range l.filtered {
		if item.ID == id {
			if i > 0 {
				l.setSelectedIndex(i)
			}
			return
		}
	}
}

func (l *SearchableList) Update(msg tea.Msg) tea.Cmd {
	if key, isKey := msg.(tea.KeyMsg); isKey {
		if l.handleNavKey(describeKey(key)) {
			return nil
		}
	}
	before := l.input.Value()
	l.input.Update(msg)
	// Every mutation of the search box resets the highlight to the first row.
	if l.input.Value() != before {
		l.selected = 0
		l.refresh()
	}
	return nil
}

// handleNavKey reports whether the key was consumed by the list; anything else
// falls through to the input and is typed into the query.
func (l *SearchableList) handleNavKey(key keyInfo) bool {
	if key.ctrl || key.alt {
		return false
	}
	switch key.name {
	case "up":
		l.move(-1)
		return true
	case "down":
		l.move(1)
		return true
	case "pageup":
		l.move(-fastScrollStep)
		return true
	case "pagedown":
		l.move(fastScrollStep)
		return true
	case "return", "linefeed":
		if item, ok := l.SelectedItem(); ok && !l.loading && l.onSelect != nil {
			l.onSelect(item)
		}
		return true
	default:
		return false
	}
}

// move wraps in both directions.
func (l *SearchableList) move(delta int) {
	n := len(l.filtered)
	if n == 0 {
		return
	}
	next := (l.selected+delta)%n + n
	l.setSelectedIndex(next % n)
}

func (l *SearchableList) setSelectedIndex(index int) {
	l.selected = index
	l.clampScroll()
}

// clampScroll keeps the selected row near the middle of the window.
func (l *SearchableList) clampScroll() {
	maxScroll := len(l.filtered) - visibleRows
	if maxScroll <= 0 {
		l.scroll = 0
		return
	}
	scroll := l.selected - visibleRows/2
	if scroll < 0 {
		scroll = 0
	}
	if scroll > maxScroll {
		scroll = maxScroll
	}
	l.scroll = scroll
}

// refresh recomputes the three display states: loading, no rows, rows.
func (l *SearchableList) refresh() {
	if l.loading {
		l.status = l.loadingText
		l.statusVisible = true
		l.listVisible = false
		l.filtered = nil
		return
	}
	filtered := l.filterFn(l.input.Value(), l.items)
	if len(filtered) == 0 {
		if len(l.items) == 0 {
			l.status = l.emptyText
		} else {
			l.status = noMatchesText
		}
		l.statusVisible = true
		l.listVisible = false
		l.filtered = nil
		return
	}
	l.status = ""
	l.statusVisible = false
	l.filtered = filtered
	l.listVisible = true
	// New rows clamp the highlight; only SetItems and typing reset it to 0.
	if l.selected > len(filtered)-1 {
		l.selected = len(filtered) - 1
	}
	if l.selected < 0 {
		l.selected = 0
	}
	l.clampScroll()
}

func (l *SearchableList) View() string {
	lines := []string{styleFocus.Render("❯ ") + l.input.View()}
	if l.statusVisible {
		lines = append(lines, styleMuted.Render(l.status))
	} else {
		lines = append(lines, "")
	}
	if !l.listVisible {
		return strings.Join(lines, "\n")
	}

	rowLines := make([]string, 0, visibleRows*2)
	end := l.scroll + visibleRows
	if end > len(l.filtered) {
		end = len(l.filtered)
	}
	for i := l.scroll; i < end; i++ {
		item := l.filtered[i]
		marker := "  "
		name := styleForeground.Render(item.Label)
		description := styleMuted.Render(item.Hint)
		if i == l.selected {
			marker = "▶ "
			name = styleSelected.Render(item.Label)
		}
		rowLines = append(rowLines, marker+name, "  "+description)
	}
	if len(l.filtered) > visibleRows {
		rowLines = l.withScrollIndicator(rowLines)
	}
	return strings.Join(append(lines, rowLines...), "\n")
}

// withScrollIndicator draws the scrollbar block in the last column, at the
// position the scroll offset has reached.
func (l *SearchableList) withScrollIndicator(rows []string) []string {
	if l.width <= 1 || len(rows) == 0 {
		return rows
	}
	maxScroll := len(l.filtered) - visibleRows
	thumb := 0
	if maxScroll > 0 {
		thumb = int(math.Round(float64(l.scroll) / float64(maxScroll) * float64(len(rows)-1)))
	}
	line := rows[thumb]
	pad := l.width - 1 - lipgloss.Width(line)
	if pad < 0 {
		pad = 0
	}
	rows[thumb] = line + strings.Repeat(" ", pad) + styleScrollbar.Render("█")
	return rows
}
