package cli

import (
	"strings"
	"testing"
)

// The doctor owns its report; what this proves is the wiring — that it is
// answered by the same injected world the run uses, not by the machine the
// test happens to run on.
func TestDoctorReportsTheRunsOwnWorld(t *testing.T) {
	h := newHarness(t).withKey("lin_api_secret")
	h.writeConfig(map[string]any{"worktreeRoot": "~/w"})

	code := h.run("doctor")

	report := h.stdout.String()
	if code != 0 && code != 1 {
		t.Fatalf("code = %d, want 0 or 1", code)
	}
	for _, want := range []string{
		"platform: ",
		"Linear credential: available via LINEAR_API_KEY",
		"config file readable: " + h.configPath(),
		"worktree root writable: ",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("the report is missing %q:\n%s", want, report)
		}
	}
	// SPEC §6: the key never appears in output.
	if strings.Contains(report, "lin_api_secret") {
		t.Errorf("the report echoed the key:\n%s", report)
	}
	if h.stderr.Len() != 0 {
		t.Errorf("stderr = %q", h.stderr.String())
	}
}

// A configured credentialCommand is what the report names, and it is run
// through the injected runner rather than a real shell.
func TestDoctorNamesTheCredentialSourceItWouldUse(t *testing.T) {
	h := newHarness(t).withKey("lin_api_from_env")
	h.credential.stdout = "lin_api_from_command\n"
	h.writeConfig(map[string]any{"credentialCommand": "print-the-key"})

	h.run("doctor")

	if h.credential.calls != 1 {
		t.Fatalf("the injected credential runner was called %d times, want 1", h.credential.calls)
	}
	if !strings.Contains(h.stdout.String(), "Linear credential: available via credentialCommand") {
		t.Errorf("report:\n%s", h.stdout.String())
	}
}
