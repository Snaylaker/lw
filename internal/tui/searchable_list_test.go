package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

var listItems = []SearchableItem{
	{ID: "1", Label: "Alpha One", Hint: "started"},
	{ID: "2", Label: "Beta Two", Hint: "backlog"},
	{ID: "3", Label: "Gamma Three", Hint: "planned"},
}

func newTestList(t *testing.T) (*SearchableList, *[]SearchableItem) {
	t.Helper()
	selected := &[]SearchableItem{}
	list := NewSearchableList(SearchableListOptions{
		OnSelect: func(item SearchableItem) { *selected = append(*selected, item) },
	})
	list.SetItems(listItems)
	list.FocusInput()
	return list, selected
}

func selectedID(t *testing.T, list *SearchableList) string {
	t.Helper()
	item, ok := list.SelectedItem()
	if !ok {
		t.Fatalf("no item is highlighted")
	}
	return item.ID
}

func TestTypingNarrowsTheOptions(t *testing.T) {
	list, _ := newTestList(t)
	mustRowLabels(t, list.Rows(), "Alpha One", "Beta Two", "Gamma Three")

	typeText(list, "beta")
	mustRowLabels(t, list.Rows(), "Beta Two")
	if list.Query() != "beta" {
		t.Fatalf("query = %q, want %q", list.Query(), "beta")
	}
	// Every keystroke resets the highlight to the first filtered row.
	if got := selectedID(t, list); got != "2" {
		t.Fatalf("selected = %q, want %q", got, "2")
	}
}

func TestArrowKeysMoveTheSelectionAndWrap(t *testing.T) {
	list, _ := newTestList(t)
	if list.SelectedIndex() != 0 {
		t.Fatalf("index = %d, want the first row", list.SelectedIndex())
	}

	list.Update(typedKey(tea.KeyDown))
	if got := selectedID(t, list); got != "2" {
		t.Fatalf("selected = %q, want %q", got, "2")
	}

	// Up from the second row, then past the first: the highlight wraps.
	list.Update(typedKey(tea.KeyUp))
	list.Update(typedKey(tea.KeyUp))
	if got := selectedID(t, list); got != "3" {
		t.Fatalf("selected = %q, want the wrap to the last row", got)
	}
	if list.SelectedIndex() != 2 {
		t.Fatalf("index = %d, want 2", list.SelectedIndex())
	}
}

// The highlight is drawn with "▶ ", which SPEC §8 shows and no state carries.
func TestTheHighlightedRowIsMarkedInTheFrame(t *testing.T) {
	list, _ := newTestList(t)
	mustContain(t, plain(list.View()), "▶ Alpha One")
	list.Update(typedKey(tea.KeyDown))
	frame := plain(list.View())
	mustContain(t, frame, "▶ Beta Two")
	mustNotContain(t, frame, "▶ Alpha One")
}

func TestEnterEmitsTheSelectedItem(t *testing.T) {
	list, selected := newTestList(t)
	list.Update(typedKey(tea.KeyDown))
	list.Update(typedKey(tea.KeyEnter))
	if len(*selected) != 1 {
		t.Fatalf("selected %d items, want 1", len(*selected))
	}
	if (*selected)[0].ID != "2" {
		t.Fatalf("selected = %q, want %q", (*selected)[0].ID, "2")
	}
}

func TestJAndKAlwaysTypeIntoTheQueryNeverNavigate(t *testing.T) {
	list, _ := newTestList(t)

	list.Update(runeKey('j'))
	if list.Query() != "j" {
		t.Fatalf("query = %q, want %q", list.Query(), "j")
	}
	if list.StatusText() != noMatchesText {
		t.Fatalf("status = %q, want %q", list.StatusText(), noMatchesText)
	}

	list.Update(runeKey('k'))
	if list.Query() != "jk" {
		t.Fatalf("query = %q, want %q", list.Query(), "jk")
	}
}

func TestShowsNoMatchesWhenTheFilterComesUpEmpty(t *testing.T) {
	list, _ := newTestList(t)
	typeText(list, "zzz")
	if list.StatusText() != noMatchesText {
		t.Fatalf("status = %q, want %q", list.StatusText(), noMatchesText)
	}
	if len(list.Rows()) != 0 {
		t.Fatalf("rows = %v, want none", list.Rows())
	}
	if _, ok := list.SelectedItem(); ok {
		t.Fatalf("nothing may be highlighted with no rows")
	}
}

func TestShowsTheLoadingLineWhileLoading(t *testing.T) {
	list, _ := newTestList(t)
	list.SetLoading(true)
	if list.StatusText() != defaultLoadingText {
		t.Fatalf("status = %q, want %q", list.StatusText(), defaultLoadingText)
	}
	if len(list.Rows()) != 0 {
		t.Fatalf("rows = %v, want none while loading", list.Rows())
	}
}

func TestEnterWhileLoadingSelectsNothing(t *testing.T) {
	list, selected := newTestList(t)
	list.SetLoading(true)
	list.Update(typedKey(tea.KeyEnter))
	if len(*selected) != 0 {
		t.Fatalf("selected %d items while loading, want 0", len(*selected))
	}
}

func TestEmptyTextOnlyWhenTheSourceListIsEmpty(t *testing.T) {
	list := NewSearchableList(SearchableListOptions{EmptyText: "no projects found"})
	if list.StatusText() != "no projects found" {
		t.Fatalf("status = %q", list.StatusText())
	}
	list.SetItems(listItems)
	typeText(list, "zzz")
	if list.StatusText() != noMatchesText {
		t.Fatalf("status = %q, want %q", list.StatusText(), noMatchesText)
	}
}

func TestDefaultPlaceholderAndTexts(t *testing.T) {
	list := NewSearchableList(SearchableListOptions{})
	// The prompt and the placeholder only exist as a frame.
	frame := plain(list.View())
	mustContain(t, frame, "❯ ")
	mustContain(t, frame, defaultPlaceholder)
	if list.StatusText() != defaultEmptyText {
		t.Fatalf("status = %q, want %q", list.StatusText(), defaultEmptyText)
	}
	list.SetItems(listItems)
	list.SetLoading(true)
	if list.StatusText() != defaultLoadingText {
		t.Fatalf("status = %q, want %q", list.StatusText(), defaultLoadingText)
	}
}

func TestNavigationKeysWithAModifierFallThroughToTheInput(t *testing.T) {
	list, _ := newTestList(t)
	// alt+down is not navigation; the list must not consume it.
	list.Update(tea.KeyMsg{Type: tea.KeyDown, Alt: true})
	if got := selectedID(t, list); got != "1" {
		t.Fatalf("selected = %q, want %q", got, "1")
	}
}

func TestPageKeysMoveByFive(t *testing.T) {
	rows := make([]SearchableItem, 0, 12)
	for i := 0; i < 12; i++ {
		rows = append(rows, SearchableItem{ID: string(rune('a' + i)), Label: "row-" + string(rune('a'+i))})
	}
	list := NewSearchableList(SearchableListOptions{})
	list.SetItems(rows)
	list.Update(typedKey(tea.KeyPgDown))
	if got := selectedID(t, list); got != "f" {
		t.Fatalf("selected = %q, want %q", got, "f")
	}
	list.Update(typedKey(tea.KeyPgUp))
	if got := selectedID(t, list); got != "a" {
		t.Fatalf("selected = %q, want %q", got, "a")
	}
}

func TestNewlinesNeverEnterTheQuery(t *testing.T) {
	list, _ := newTestList(t)
	list.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("al\npha"), Paste: true})
	if list.Query() != "alpha" {
		t.Fatalf("query = %q, want %q", list.Query(), "alpha")
	}
}

func TestSelectItemByIDRestoresTheHighlight(t *testing.T) {
	list, _ := newTestList(t)
	list.SetItems(listItems)
	list.SelectItemByID("3")
	if got := selectedID(t, list); got != "3" {
		t.Fatalf("selected = %q, want %q", got, "3")
	}
	// An id that vanished leaves the selection where it is, silently.
	list.SelectItemByID("gone")
	if got := selectedID(t, list); got != "3" {
		t.Fatalf("selected = %q, want %q", got, "3")
	}
}

func TestTruncateGraphemesNeverSplitsGraphemeClusters(t *testing.T) {
	cases := []struct {
		text string
		max  int
		want string
	}{
		{"héllo", 10, "héllo"},
		{"abcdef", 4, "abc…"},
		{"👩‍👩‍👦👩‍👩‍👦👩‍👩‍👦", 2, "👩‍👩‍👦…"},
		{"abc", 0, ""},
		{"abc", -1, ""},
	}
	for _, tc := range cases {
		if got := TruncateGraphemes(tc.text, tc.max); got != tc.want {
			t.Errorf("TruncateGraphemes(%q, %d) = %q, want %q", tc.text, tc.max, got, tc.want)
		}
	}
}

func TestDefaultFilterMatchesLabelAndHint(t *testing.T) {
	got := DefaultFilter("  BACK ", listItems)
	if len(got) != 1 || got[0].ID != "2" {
		t.Fatalf("filter matched %v, want the backlog row", got)
	}
	if len(DefaultFilter("", listItems)) != len(listItems) {
		t.Fatalf("an empty query keeps every row, in order")
	}
}
