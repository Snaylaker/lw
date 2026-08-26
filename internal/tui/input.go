package tui

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// input is the single-line search box. It is written here rather than taken
// from bubbles/textinput so the editing bindings and the newline handling match
// a single-line terminal input.
type input struct {
	runes       []rune
	cursor      int
	focused     bool
	placeholder string
	maxLength   int
	secret      bool
}

func newInput(placeholder string) *input {
	return &input{placeholder: placeholder, maxLength: inputMaxLength}
}

func newSecretInput(placeholder string) *input {
	field := newInput(placeholder)
	field.secret = true
	return field
}

func (i *input) Value() string { return string(i.runes) }

func (i *input) SetValue(value string) {
	runes := []rune(value)
	if len(runes) > i.maxLength {
		runes = runes[:i.maxLength]
	}
	i.runes = append([]rune(nil), runes...)
	i.cursor = len(i.runes)
}

func (i *input) Focus() { i.focused = true }

// Update applies one key. Newlines never enter the buffer, from a keystroke or
// from a paste.
func (i *input) Update(msg tea.Msg) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return
	}
	info := describeKey(key)
	switch {
	case info.ctrl:
		i.handleCtrl(info.name)
		return
	case info.alt:
		return
	}
	switch info.name {
	case "left":
		i.moveCursor(-1)
	case "right":
		i.moveCursor(1)
	case "home":
		i.cursor = 0
	case "end":
		i.cursor = len(i.runes)
	case "backspace":
		i.deleteBackward()
	case "delete":
		i.deleteForward()
	case "space":
		i.insert([]rune{' '})
	case "return", "linefeed", "escape", "tab", "up", "down", "pageup", "pagedown":
		// Never text: submit and navigation keys leave the buffer alone.
	default:
		if key.Type == tea.KeyRunes {
			i.insert(key.Runes)
		}
	}
}

func (i *input) handleCtrl(name string) {
	switch name {
	case "a":
		i.cursor = 0
	case "e":
		i.cursor = len(i.runes)
	case "f":
		i.moveCursor(1)
	case "b":
		i.moveCursor(-1)
	case "w", "h":
		// ctrl+h is ctrl+backspace; like ctrl+w it deletes the word behind.
		i.deleteWordBackward()
	case "d":
		i.deleteForward()
	case "k":
		i.runes = append([]rune{}, i.runes[:i.cursor]...)
	case "u":
		i.runes = append([]rune{}, i.runes[i.cursor:]...)
		i.cursor = 0
	}
}

func (i *input) insert(runes []rune) {
	kept := make([]rune, 0, len(runes))
	for _, r := range runes {
		if r == '\n' || r == '\r' {
			continue
		}
		kept = append(kept, r)
	}
	if len(kept) == 0 {
		return
	}
	if len(i.runes)+len(kept) > i.maxLength {
		room := i.maxLength - len(i.runes)
		if room <= 0 {
			return
		}
		kept = kept[:room]
	}
	next := make([]rune, 0, len(i.runes)+len(kept))
	next = append(next, i.runes[:i.cursor]...)
	next = append(next, kept...)
	next = append(next, i.runes[i.cursor:]...)
	i.runes = next
	i.cursor += len(kept)
}

func (i *input) deleteBackward() {
	if i.cursor == 0 {
		return
	}
	i.runes = append(i.runes[:i.cursor-1], i.runes[i.cursor:]...)
	i.cursor--
}

func (i *input) deleteForward() {
	if i.cursor >= len(i.runes) {
		return
	}
	i.runes = append(i.runes[:i.cursor], i.runes[i.cursor+1:]...)
}

func (i *input) deleteWordBackward() {
	start := i.cursor
	for start > 0 && unicode.IsSpace(i.runes[start-1]) {
		start--
	}
	for start > 0 && !unicode.IsSpace(i.runes[start-1]) {
		start--
	}
	i.runes = append(i.runes[:start], i.runes[i.cursor:]...)
	i.cursor = start
}

func (i *input) moveCursor(delta int) {
	next := i.cursor + delta
	if next < 0 {
		next = 0
	}
	if next > len(i.runes) {
		next = len(i.runes)
	}
	i.cursor = next
}

// Renderer-free for the same reason as the package-level styles in theme.go:
// command substitution captures stdout while the interactive UI uses stderr.
var styleCursor = (lipgloss.Style{}).Reverse(true)

func (i *input) View() string {
	if len(i.runes) == 0 {
		if i.focused {
			return styleCursor.Render(" ") + styleMuted.Render(i.placeholder)
		}
		return styleMuted.Render(i.placeholder)
	}
	displayed := i.displayRunes()
	if !i.focused {
		return focusedText(string(displayed))
	}
	var b strings.Builder
	b.WriteString(focusedText(string(displayed[:i.cursor])))
	if i.cursor < len(displayed) {
		b.WriteString(styleCursor.Render(string(displayed[i.cursor])))
		b.WriteString(focusedText(string(displayed[i.cursor+1:])))
	} else {
		b.WriteString(styleCursor.Render(" "))
	}
	return b.String()
}

func (i *input) displayRunes() []rune {
	if !i.secret {
		return i.runes
	}
	masked := make([]rune, len(i.runes))
	for index := range masked {
		masked[index] = '•'
	}
	return masked
}

// focusedText paints typed text with the focus tint the input recipe applies.
func focusedText(text string) string {
	if text == "" {
		return ""
	}
	return lipgloss.NewStyle().Foreground(focusedTextColor.Hex()).Render(text)
}
