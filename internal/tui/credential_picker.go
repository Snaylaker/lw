package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/snaylaker/lw/internal/credential"
)

type CredentialPickerOptions struct {
	OnSubmit func(string, credential.Store) tea.Cmd
}

// CredentialPicker is the first onboarding screen when lw cannot find a key.
// The input is always masked and Destroy clears its backing buffer.
type CredentialPicker struct {
	input        *input
	onSubmit     func(string, credential.Store) tea.Cmd
	problem      string
	fallbackPath string
	connecting   bool
}

func NewCredentialPicker(options CredentialPickerOptions) *CredentialPicker {
	field := newSecretInput("paste a Read-only Linear API key")
	field.Focus()
	return &CredentialPicker{input: field, onSubmit: options.OnSubmit}
}

func (p *CredentialPicker) Value() string { return p.input.Value() }

func (p *CredentialPicker) SetProblem(problem string) {
	p.connecting = false
	p.problem = problem
	p.fallbackPath = ""
	p.input.SetValue("")
}

func (p *CredentialPicker) SetFileFallback(path string) {
	p.connecting = false
	p.problem = ""
	p.fallbackPath = path
}

func (p *CredentialPicker) Update(msg tea.Msg) tea.Cmd {
	if p.connecting {
		return nil
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		info := describeKey(key)
		if !info.ctrl && !info.alt && (info.name == "return" || info.name == "linefeed") {
			value := strings.TrimSpace(p.input.Value())
			if value == "" || p.onSubmit == nil {
				return nil
			}
			p.problem = ""
			p.connecting = true
			target := credential.StoreKeyring
			if p.fallbackPath != "" {
				target = credential.StoreFile
			}
			return p.onSubmit(value, target)
		}
	}
	// Once the validated key reaches the consent screen, keep it immutable.
	// Enter can approve it and Escape can cancel through the launcher's global
	// handler; no second validation or duplicate secret buffer is needed.
	if p.fallbackPath == "" {
		p.input.Update(msg)
	}
	return nil
}

func (p *CredentialPicker) Destroy()     { p.input.SetValue("") }
func (p *CredentialPicker) SetWidth(int) {}

func (p *CredentialPicker) View() string {
	lines := []string{
		styleForeground.Render("Connect to Linear"),
		styleMuted.Render("lw only reads data from Linear."),
		styleMuted.Render("Create a Personal API key with Read permission:"),
		styleMuted.Render("Settings → Account → Security → Personal API keys"),
		styleMuted.Render("https://linear.app/settings/account/security"),
	}
	if p.fallbackPath == "" {
		lines = append(lines,
			styleMuted.Render("The key will be saved in your system keychain."),
			styleMuted.Render("If no keychain is available, lw asks before using an owner-only file."),
			styleMuted.Render("Enter connect · Esc cancel"),
		)
	} else {
		lines = append(lines,
			styleWarning.Render("The system keychain is unavailable."),
			styleMuted.Render("Your key was validated but has not been saved."),
			styleMuted.Render("Press Enter to use this owner-only credential file:"),
			styleForeground.Render(p.fallbackPath),
			styleMuted.Render("Enter approve · Esc cancel"),
		)
	}
	lines = append(lines, "", styleFocus.Render("❯ ")+p.input.View())
	if p.connecting {
		lines = append(lines, styleMuted.Render("Connecting…"))
	}
	if p.problem != "" {
		lines = append(lines, styleDestruct.Render(p.problem))
	}
	return strings.Join(lines, "\n")
}

// CredentialSavedView reports the actual persistence destination before the
// onboarding flow advances.
type CredentialSavedView struct {
	location   credential.Location
	onContinue func() tea.Cmd
}

func NewCredentialSavedView(location credential.Location, onContinue func() tea.Cmd) *CredentialSavedView {
	return &CredentialSavedView{location: location, onContinue: onContinue}
}
func (v *CredentialSavedView) Update(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyMsg); ok {
		info := describeKey(key)
		if !info.ctrl && !info.alt && (info.name == "return" || info.name == "linefeed") && v.onContinue != nil {
			return v.onContinue()
		}
	}
	return nil
}
func (v *CredentialSavedView) Destroy()     {}
func (v *CredentialSavedView) SetWidth(int) {}
func (v *CredentialSavedView) View() string {
	lines := []string{styleSuccess.Render("✓ Connected to Linear")}
	switch v.location.Store() {
	case credential.StoreKeyring:
		lines = append(lines,
			styleForeground.Render("API key saved in your system keychain."),
			styleMuted.Render("Service: ")+styleForeground.Render(credential.KeyringService),
			styleMuted.Render("Account: ")+styleForeground.Render(credential.KeyringAccount),
		)
	case credential.StoreFile:
		lines = append(lines,
			styleForeground.Render("API key saved in this owner-only credential file:"),
			styleForeground.Render(v.location.Path()),
		)
	}
	lines = append(lines, styleMuted.Render("Enter continue · Esc cancel"))
	return strings.Join(lines, "\n")
}
