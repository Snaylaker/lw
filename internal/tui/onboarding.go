package tui

import (
	"errors"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/snaylaker/lw/internal/credential"
	"github.com/snaylaker/lw/internal/lwerr"
)

// This file owns the conditional setup phase. Issue search and repository
// selection stay in the same Bubble Tea model, so onboarding continues without
// restarting the process.

func (m *Launcher) openCredential() tea.Cmd {
	m.screen = ScreenCredential
	picker := NewCredentialPicker(CredentialPickerOptions{
		OnSubmit: func(key string, target credential.Store) tea.Cmd {
			ctx, token := m.beginLoad()
			return func() tea.Msg {
				setup := m.deps.Credential
				if setup == nil || setup.Save == nil {
					return credentialSavedMsg{token: token, err: lwerr.New(lwerr.AuthRequired,
						"The Linear API key could not be saved.", "retry")}
				}
				location, err := setup.Save(ctx, key, target)
				return credentialSavedMsg{token: token, location: location, err: err}
			}
		},
	})
	m.show(picker)
	m.credentialPicker = picker
	return nil
}

func (m *Launcher) onCredentialSaved(msg credentialSavedMsg) tea.Cmd {
	if msg.token != m.loadToken || m.screen != ScreenCredential || m.credentialPicker == nil {
		return nil
	}
	m.cancelLoad = nil
	if errors.Is(msg.err, credential.ErrKeyringUnavailable) {
		m.credentialPicker.SetFileFallback(m.deps.Credential.File)
		return nil
	}
	if msg.err != nil {
		if classified, ok := lwerr.As(msg.err); ok {
			m.credentialPicker.SetProblem(classified.Message)
		} else {
			m.credentialPicker.SetProblem("Could not connect to Linear.")
		}
		return nil
	}
	m.deps.Credential = nil
	return m.openCredentialSaved(msg.location)
}

func (m *Launcher) openCredentialSaved(location credential.Location) tea.Cmd {
	m.screen = ScreenCredentialSaved
	m.show(NewCredentialSavedView(location, m.continueAfterCredential))
	return nil
}

func (m *Launcher) continueAfterCredential() tea.Cmd {
	if m.deps.NeedsRepoRoot && m.deps.PreselectRepo == nil {
		return m.openRoot(m.openIssues)
	}
	return m.openIssues()
}

func (m *Launcher) openRoot(next func() tea.Cmd) tea.Cmd {
	m.screen = ScreenRoot
	var picker *RootPicker
	picker = NewRootPicker(RootPickerOptions{
		Suggested: m.deps.SuggestedRepoRoot,
		OnSubmit: func(root string) {
			if m.deps.SetRepoRoot == nil {
				picker.SetProblem("repository folder could not be saved")
				return
			}
			if _, err := m.deps.SetRepoRoot(root); err != nil {
				picker.SetProblem(err.Error())
				return
			}
			if next != nil {
				m.enqueue(next())
			}
		},
	})
	m.show(picker)
	return nil
}
