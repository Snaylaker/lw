package tui

// SameRows reports whether two row sets are identical. An unchanged list is
// left alone: SetItems would drop the highlight for nothing.
func SameRows(a, b []SearchableItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i, row := range a {
		if row.ID != b[i].ID || row.Label != b[i].Label || row.Hint != b[i].Hint {
			return false
		}
	}
	return true
}

// applyRows re-pushes rows only when they changed, keeping the highlight on the
// row the user was on.
func applyRows(list *SearchableList, current []SearchableItem, rows []SearchableItem, painted bool) ([]SearchableItem, bool) {
	if painted && SameRows(current, rows) {
		return current, painted
	}
	keep := ""
	if item, ok := list.SelectedItem(); ok {
		keep = item.ID
	}
	list.SetItems(rows)
	list.SelectItemByID(keep)
	return rows, true
}
