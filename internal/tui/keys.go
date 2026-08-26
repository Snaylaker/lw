package tui

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

// keyInfo is the normalised shape the screens match on, mirroring the
// name/ctrl/meta/option/shift fields of a terminal key event.
type keyInfo struct {
	name  string
	ctrl  bool
	alt   bool
	shift bool
}

// describeKey normalises a Bubble Tea key into that shape. Bubble Tea reports
// no meta modifier, so meta and option both collapse onto alt.
func describeKey(msg tea.KeyMsg) keyInfo {
	info := keyInfo{alt: msg.Alt}
	switch msg.Type {
	case tea.KeyUp:
		info.name = "up"
	case tea.KeyDown:
		info.name = "down"
	case tea.KeyLeft:
		info.name = "left"
	case tea.KeyRight:
		info.name = "right"
	case tea.KeyPgUp:
		info.name = "pageup"
	case tea.KeyPgDown:
		info.name = "pagedown"
	case tea.KeyEnter:
		info.name = "return"
	case tea.KeyEsc:
		info.name = "escape"
	case tea.KeySpace:
		info.name = "space"
	case tea.KeyTab:
		info.name = "tab"
	case tea.KeyBackspace:
		info.name = "backspace"
	case tea.KeyDelete:
		info.name = "delete"
	case tea.KeyCtrlJ:
		// A bare line feed; the list treats it as Enter.
		info.name = "linefeed"
	case tea.KeyRunes:
		if len(msg.Runes) == 1 {
			r := msg.Runes[0]
			info.name = string(unicode.ToLower(r))
			info.shift = unicode.IsUpper(r)
		}
	default:
		name := msg.Type.String()
		if rest, found := strings.CutPrefix(name, "ctrl+"); found {
			info.ctrl = true
			info.name = rest
		} else {
			info.name = name
		}
	}
	return info
}
