package cli

import (
	"errors"
	"strings"
	"testing"
)

func TestLogoutReportsDeletionFailureInsteadOfClaimingSuccess(t *testing.T) {
	h := newHarness(t)
	h.vault.key = "secret"
	h.vault.deleteErr = errors.New("keychain locked")

	if code := h.run("logout"); code != 1 {
		t.Fatalf("code = %d", code)
	}
	if strings.Contains(h.stdout.String(), "Removed") {
		t.Fatalf("false success = %q", h.stdout.String())
	}
	if !strings.Contains(h.stderr.String(), "could not be removed") {
		t.Errorf("stderr = %q", h.stderr.String())
	}
}

func TestLogoutRemovesOnlyTheSavedCredential(t *testing.T) {
	h := newHarness(t)
	h.vault.key = "secret"
	h.withKey("environment-owned")

	if code := h.run("logout"); code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, h.stderr.String())
	}
	if !h.vault.deleted || h.vault.key != "" {
		t.Fatal("saved credential was not deleted")
	}
	if h.env["LINEAR_API_KEY"] != "environment-owned" {
		t.Fatal("logout changed the user's environment source")
	}
	if !strings.Contains(h.stdout.String(), "Removed lw's saved Linear API key") {
		t.Errorf("stdout = %q", h.stdout.String())
	}
}
