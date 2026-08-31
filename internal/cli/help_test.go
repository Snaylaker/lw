package cli

import (
	"strings"
	"testing"
)

// SPEC §2 lists the commands. Every one of them has to be in the help text, or
// a usage error points at a page that does not mention what the user wanted.
func TestHelpTextListsEveryCommand(t *testing.T) {
	for _, literal := range []string{
		"lw run [flags] -- <command> [args...]",
		"lw doctor",
		"lw context [--json]",
		"lw summary <text>",
	} {
		if !strings.Contains(HelpText(), literal) {
			t.Errorf("help text is missing %q", literal)
		}
	}
}

func TestHelpTextListsEveryFlag(t *testing.T) {
	for _, literal := range []string{
		"--repo <path>",
		"--issue <IDENT>",
		"--json",
		"--version",
		"--help",
	} {
		if !strings.Contains(HelpText(), literal) {
			t.Errorf("help text is missing %q", literal)
		}
	}
}

// Every flag the parser accepts is documented, so the two never drift apart.
func TestHelpTextDocumentsEveryParsedFlag(t *testing.T) {
	for name := range flagSpecs {
		if !strings.Contains(HelpText(), name) {
			t.Errorf("help text is missing the accepted flag %q", name)
		}
	}
}

func TestHelpTextExplainsCredentialOnboardingAndAdvancedSources(t *testing.T) {
	for _, literal := range []string{"Read-only", "system keychain", "credentialCommand", "LINEAR_API_KEY", "lw logout"} {
		if !strings.Contains(HelpText(), literal) {
			t.Errorf("help text is missing %q", literal)
		}
	}
	if strings.Contains(HelpText(), "lw auth") {
		t.Error("there is no lw auth command")
	}
}

func TestHelpTextExplainsDirectAgentLaunching(t *testing.T) {
	for _, literal := range []string{"lw run -- claude", "without a shell", "LINEAR_API_KEY is removed"} {
		if !strings.Contains(HelpText(), literal) {
			t.Errorf("help text is missing %q", literal)
		}
	}
}

func TestHelpTextStatesTheExitCodes(t *testing.T) {
	if !strings.Contains(HelpText(), "0 success · 1 error · 2 usage · 130 cancelled.") {
		t.Errorf("help text does not state the exit codes:\n%s", HelpText())
	}
}

func TestHelpTextEndsInANewline(t *testing.T) {
	if !strings.HasSuffix(HelpText(), "\n") {
		t.Error("help text does not end in a newline")
	}
	if strings.HasSuffix(HelpText(), "\n\n") {
		t.Error("help text ends in a blank line")
	}
}

func TestVersionDefaultsToDev(t *testing.T) {
	if Version != "dev" {
		t.Errorf("Version = %q, want %q for a source build", Version, "dev")
	}
}
