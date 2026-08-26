// Package tui holds workspace issue search, repository/onboarding pickers,
// progress and error views, and the launcher state machine that sequences them.
package tui

import (
	"fmt"
	"math"

	"github.com/charmbracelet/lipgloss"
)

// Colors is the semantic palette every screen reads.
type Colors struct {
	Background        lipgloss.TerminalColor
	Foreground        lipgloss.TerminalColor
	MutedForeground   lipgloss.TerminalColor
	Focus             lipgloss.TerminalColor
	PrimaryForeground lipgloss.TerminalColor
	Destructive       lipgloss.TerminalColor
	Success           lipgloss.TerminalColor
	Warning           lipgloss.TerminalColor
}

type Borders struct {
	Style lipgloss.Border
}

type Tokens struct {
	Colors  Colors
	Borders Borders
}

// Theme is ANSI-indexed on purpose: every screen inherits whatever palette (and
// transparency) the terminal user configured. NoColor means "terminal default
// intent" — the background and foreground are never written.
var Theme = Tokens{
	Colors: Colors{
		Background:        lipgloss.NoColor{},
		Foreground:        lipgloss.NoColor{},
		MutedForeground:   lipgloss.ANSIColor(7),  // white
		Focus:             lipgloss.ANSIColor(12), // bright blue
		PrimaryForeground: lipgloss.ANSIColor(15), // bright white
		Destructive:       lipgloss.ANSIColor(1),  // red
		Success:           lipgloss.ANSIColor(2),  // green
		Warning:           lipgloss.ANSIColor(3),  // yellow
	},
	Borders: Borders{Style: lipgloss.NormalBorder()},
}

// RGBA is the colour snapshot Tint blends. Indexed and default-intent colours
// have no channels of their own, so they are resolved to a snapshot first.
type RGBA struct {
	R, G, B, A uint8
}

// Hex renders the snapshot as a lipgloss colour.
func (c RGBA) Hex() lipgloss.Color {
	return lipgloss.Color(fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B))
}

// ansi16 is the xterm snapshot of the first sixteen palette entries. The
// terminal still paints the indices themselves; the snapshot only exists so a
// blend has channels to work with.
var ansi16 = [16]RGBA{
	{0x00, 0x00, 0x00, 0xFF}, {0xCD, 0x00, 0x00, 0xFF}, {0x00, 0xCD, 0x00, 0xFF}, {0xCD, 0xCD, 0x00, 0xFF},
	{0x00, 0x00, 0xEE, 0xFF}, {0xCD, 0x00, 0xCD, 0xFF}, {0x00, 0xCD, 0xCD, 0xFF}, {0xE5, 0xE5, 0xE5, 0xFF},
	{0x7F, 0x7F, 0x7F, 0xFF}, {0xFF, 0x00, 0x00, 0xFF}, {0x00, 0xFF, 0x00, 0xFF}, {0xFF, 0xFF, 0x00, 0xFF},
	{0x5C, 0x5C, 0xFF, 0xFF}, {0xFF, 0x00, 0xFF, 0xFF}, {0x00, 0xFF, 0xFF, 0xFF}, {0xFF, 0xFF, 0xFF, 0xFF},
}

var (
	defaultBackgroundSnapshot = RGBA{0x00, 0x00, 0x00, 0xFF}
	defaultForegroundSnapshot = RGBA{0xE5, 0xE5, 0xE5, 0xFF}
)

// Tint blends overlay into base by alpha (0–1). The overlay's own alpha is
// discarded; the result keeps the base's.
func Tint(base, overlay RGBA, alpha float64) RGBA {
	channel := func(a, b uint8) uint8 {
		af := float64(a) / 255
		bf := float64(b) / 255
		return uint8(math.Round((af + (bf-af)*alpha) * 255))
	}
	return RGBA{
		R: channel(base.R, overlay.R),
		G: channel(base.G, overlay.G),
		B: channel(base.B, overlay.B),
		A: base.A,
	}
}

const tintAlpha = 0.35

var (
	// selectedBackground = tint(background, focus, 0.35)
	selectedBackground = Tint(defaultBackgroundSnapshot, ansi16[12], tintAlpha)
	// focusedTextColor = tint(foreground, focus, 0.35)
	focusedTextColor = Tint(defaultForegroundSnapshot, ansi16[12], tintAlpha)
)

var (
	// Keep package-level styles renderer-free. lipgloss.NewStyle binds to the
	// renderer that exists during package initialization (normally stdout),
	// which is wrong when stdout is captured by $(lw) and the TUI uses stderr.
	// A zero Style resolves the current stderr-bound renderer when Render runs.
	styleForeground = (lipgloss.Style{}).Foreground(Theme.Colors.Foreground)
	styleMuted      = (lipgloss.Style{}).Foreground(Theme.Colors.MutedForeground)
	styleFocus      = (lipgloss.Style{}).Foreground(Theme.Colors.Focus)
	styleDestruct   = (lipgloss.Style{}).Foreground(Theme.Colors.Destructive)
	styleSuccess    = (lipgloss.Style{}).Foreground(Theme.Colors.Success)
	styleWarning    = (lipgloss.Style{}).Foreground(Theme.Colors.Warning)
	styleSelected   = (lipgloss.Style{}).
			Foreground(Theme.Colors.PrimaryForeground).
			Background(selectedBackground.Hex())
	styleScrollbar = (lipgloss.Style{}).Foreground(lipgloss.Color("#666666"))
)
