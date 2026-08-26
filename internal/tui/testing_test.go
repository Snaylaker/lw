package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

var ansiPattern = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

// plain is the frame as the assertions read it: styling is not a contract, the
// characters are.
func plain(view string) string { return ansiPattern.ReplaceAllString(view, "") }

func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func typedKey(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }
func enter() tea.KeyMsg                 { return typedKey(tea.KeyEnter) }
func escape() tea.KeyMsg                { return typedKey(tea.KeyEsc) }
func ctrlC() tea.KeyMsg                 { return typedKey(tea.KeyCtrlC) }
func ctrlR() tea.KeyMsg                 { return typedKey(tea.KeyCtrlR) }

type updater interface{ Update(tea.Msg) tea.Cmd }

func typeText(target updater, text string) {
	for _, r := range text {
		target.Update(runeKey(r))
	}
}

// rowLabels reduces a picker's state to what the rows say, in order. SPEC §12
// wants the TUI tested as state transitions, so ordering, filtering and
// truncation are asserted here rather than against a rendered frame.
func rowLabels(rows []SearchableItem) []string {
	labels := make([]string, 0, len(rows))
	for _, row := range rows {
		labels = append(labels, row.Label)
	}
	return labels
}

func mustRowLabels(t *testing.T, rows []SearchableItem, want ...string) {
	t.Helper()
	got := rowLabels(rows)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("rows = %q, want %q", got, want)
	}
}

// mustHighlight asserts which row Enter would choose.
type highlighter interface {
	Highlighted() (SearchableItem, bool)
}

func mustHighlight(t *testing.T, picker highlighter, wantID string) {
	t.Helper()
	row, ok := picker.Highlighted()
	if !ok {
		t.Fatalf("no row is highlighted, want %q", wantID)
	}
	if row.ID != wantID {
		t.Fatalf("highlighted %q, want %q", row.ID, wantID)
	}
}

func mustContain(t *testing.T, frame, want string) {
	t.Helper()
	if !strings.Contains(frame, want) {
		t.Fatalf("frame missing %q:\n%s", want, frame)
	}
}

func mustNotContain(t *testing.T, frame, unwanted string) {
	t.Helper()
	if strings.Contains(frame, unwanted) {
		t.Fatalf("frame must not contain %q:\n%s", unwanted, frame)
	}
}
