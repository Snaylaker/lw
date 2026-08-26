package tui

import "strings"

const pinMarker = "★ "

type browserView string

const (
	browserIssues   browserView = "Issues"
	browserProjects browserView = "Projects"
	browserTeams    browserView = "Teams"
)

type shortcut struct {
	key   string
	label string
}

// browserTabs gives the three discovery modes a stable visual location. The
// active mode is colored, bold and underlined instead of burying navigation in
// a long sentence.
func browserTabs(active browserView) string {
	labels := []browserView{browserIssues, browserProjects, browserTeams}
	parts := make([]string, 0, len(labels)+1)
	for _, label := range labels {
		if label == active {
			parts = append(parts, styleFocus.Copy().Bold(true).Underline(true).Render("● "+string(label)))
		} else {
			parts = append(parts, styleMuted.Render(string(label)))
		}
	}
	parts = append(parts, renderShortcut(shortcut{key: "Tab", label: "next view"}))
	return strings.Join(parts, "  ")
}

func shortcutLine(items ...shortcut) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, renderShortcut(item))
	}
	return strings.Join(parts, styleMuted.Render("  ·  "))
}

func renderShortcut(item shortcut) string {
	keycap := styleMuted.Render("[") + styleForeground.Copy().Bold(true).Render(item.key) + styleMuted.Render("]")
	return keycap + " " + styleMuted.Render(item.label)
}
