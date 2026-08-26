package tui

import (
	"strings"
	"testing"
)

func TestRootPickerExplainsTheFolderAndPrefillsTheSuggestion(t *testing.T) {
	picker := NewRootPicker(RootPickerOptions{Suggested: "/home/alex/Work"})
	if picker.Value() != "/home/alex/Work" {
		t.Errorf("value = %q", picker.Value())
	}
	view := plain(picker.View())
	for _, want := range []string{
		"Where are your repositories?",
		"folder that contains your Git repositories",
		"for ~/Work/api and ~/Work/web, enter ~/Work",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
}

func TestRootPickerSubmitsTheFolderOnEnter(t *testing.T) {
	var submitted string
	picker := NewRootPicker(RootPickerOptions{
		Suggested: "/home/alex/Work",
		OnSubmit:  func(value string) { submitted = value },
	})
	picker.Update(enter())
	if submitted != "/home/alex/Work" {
		t.Errorf("submitted = %q", submitted)
	}
}
