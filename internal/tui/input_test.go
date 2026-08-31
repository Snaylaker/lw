package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSelectedPrefillIsReplacedByTypingOrPaste(t *testing.T) {
	field := newInput("branch")
	field.SetValue("linear/demo-4009-suggestion")
	field.Focus()
	field.SelectAll()

	field.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("team/demo-4009-custom")})
	if got := field.Value(); got != "team/demo-4009-custom" {
		t.Fatalf("value = %q", got)
	}
}

func TestSelectedPrefillCanBeKeptAndEditedWithArrows(t *testing.T) {
	field := newInput("branch")
	field.SetValue("suggestion")
	field.Focus()
	field.SelectAll()

	field.Update(typedKey(tea.KeyRight))
	field.Update(runeKey('x'))
	if got := field.Value(); got != "suggestionx" {
		t.Fatalf("value = %q", got)
	}
}

func TestBackspaceClearsASelectedPrefill(t *testing.T) {
	field := newInput("branch")
	field.SetValue("suggestion")
	field.Focus()
	field.SelectAll()

	field.Update(typedKey(tea.KeyBackspace))
	if got := field.Value(); got != "" {
		t.Fatalf("value = %q", got)
	}
}
