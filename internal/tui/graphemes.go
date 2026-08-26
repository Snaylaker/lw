package tui

import (
	"strings"

	"github.com/rivo/uniseg"
)

// TruncateGraphemes cuts text to maxGraphemes UAX #29 extended grapheme
// clusters, never splitting a surrogate pair or an emoji ZWJ cluster. A
// truncated result has exactly maxGraphemes clusters: max-1 plus the ellipsis.
func TruncateGraphemes(text string, maxGraphemes int) string {
	if maxGraphemes <= 0 {
		return ""
	}
	if uniseg.GraphemeClusterCount(text) <= maxGraphemes {
		return text
	}
	var b strings.Builder
	kept := 0
	graphemes := uniseg.NewGraphemes(text)
	for kept < maxGraphemes-1 && graphemes.Next() {
		b.WriteString(graphemes.Str())
		kept++
	}
	b.WriteString("…")
	return b.String()
}
