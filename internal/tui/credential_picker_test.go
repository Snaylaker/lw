package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/snaylaker/lw/internal/credential"
)

func TestCredentialPickerExplainsReadOnlyAccessAndMasksTheKey(t *testing.T) {
	picker := NewCredentialPicker(CredentialPickerOptions{})
	for _, r := range "lin_api_secret" {
		picker.Update(runeKey(r))
	}
	view := plain(picker.View())
	for _, want := range []string{"Connect to Linear", "only reads data", "Read permission", "linear.app/settings/account/security", "system keychain"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "lin_api_secret") {
		t.Fatal("the key is visible in the rendered screen")
	}
	if !strings.Contains(view, "••••") {
		t.Fatal("the rendered screen does not show masked input")
	}
}

func TestCredentialPickerSubmitsOnceAndClearsAfterFailure(t *testing.T) {
	var submitted string
	picker := NewCredentialPicker(CredentialPickerOptions{
		OnSubmit: func(key string, _ credential.Store) tea.Cmd {
			submitted = key
			return nil
		},
	})
	for _, r := range "secret" {
		picker.Update(runeKey(r))
	}
	picker.Update(enter())
	picker.Update(enter())
	if submitted != "secret" {
		t.Errorf("submitted = %q", submitted)
	}
	picker.SetProblem("rejected")
	if picker.Value() != "" {
		t.Error("rejected key was retained")
	}
}

func TestCredentialPickerRequiresExplicitFileFallbackApproval(t *testing.T) {
	var target credential.Store
	picker := NewCredentialPicker(CredentialPickerOptions{
		OnSubmit: func(_ string, store credential.Store) tea.Cmd {
			target = store
			return nil
		},
	})
	for _, r := range "secret" {
		picker.Update(runeKey(r))
	}
	picker.SetFileFallback("/home/alex/.config/lw/credentials")
	view := plain(picker.View())
	for _, want := range []string{"keychain is unavailable", "has not been saved", "/home/alex/.config/lw/credentials", "Enter approve"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
	picker.Update(runeKey('x'))
	if picker.Value() != "secret" {
		t.Fatal("validated key changed while awaiting file consent")
	}
	picker.Update(enter())
	if target != credential.StoreFile {
		t.Fatalf("store = %v, want file", target)
	}
}
