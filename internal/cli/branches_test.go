package cli

import (
	"strings"
	"testing"

	"github.com/snaylaker/lw/internal/config"
)

func TestBranchesSetShowPreviewAndUnsetRule(t *testing.T) {
	h := newHarness(t).withKey("lin_api_key")
	h.writeConfig(map[string]any{"pruneMerged": true})
	git(t, h.repo, "remote", "add", "origin", "https://gitlab.example.com/group/api.git")

	if code := h.run("branches", "set-rule", "--username", "alex", "{username}/{ticket}/{slug}"); code != 0 {
		t.Fatalf("set-rule code = %d, stderr = %q", code, h.stderr.String())
	}
	if got := h.stdout.String(); got != "Saved branch rule for gitlab.example.com/group/api.\n" {
		t.Fatalf("set-rule stdout = %q", got)
	}
	stored, err := config.ReadStoredConfig(h.configPath())
	if err != nil {
		t.Fatal(err)
	}
	template, username, ok := config.BranchRuleFor(stored, "gitlab.example.com/group/api")
	if !ok || template != "{username}/{ticket}/{slug}" || username != "alex" || !stored.PruneMerged {
		t.Fatalf("stored config = %+v", stored)
	}
	if h.http.requests() != 0 {
		t.Fatal("set-rule contacted Linear")
	}

	h.stdout.Reset()
	h.stderr.Reset()
	if code := h.run("branches", "show-rule"); code != 0 {
		t.Fatalf("show-rule code = %d, stderr = %q", code, h.stderr.String())
	}
	wantShow := "repository: gitlab.example.com/group/api\n" +
		"template: {username}/{ticket}/{slug}\n" +
		"username: alex\n"
	if h.stdout.String() != wantShow {
		t.Fatalf("show-rule stdout = %q", h.stdout.String())
	}

	h.stdout.Reset()
	h.stderr.Reset()
	h.http.response = issueResponse
	if code := h.run("branches", "preview", "ENG-3971"); code != 0 {
		t.Fatalf("preview code = %d, stderr = %q", code, h.stderr.String())
	}
	if got := h.stdout.String(); got != "alex/ENG-3971/improve-command-completion-output\n" {
		t.Fatalf("preview stdout = %q", got)
	}
	if h.http.requests() != 1 {
		t.Fatalf("preview requests = %d, want one issue lookup", h.http.requests())
	}

	h.stdout.Reset()
	h.stderr.Reset()
	if code := h.run("branches", "unset-rule"); code != 0 {
		t.Fatalf("unset-rule code = %d, stderr = %q", code, h.stderr.String())
	}
	if got := h.stdout.String(); got != "Removed branch rule for gitlab.example.com/group/api.\n" {
		t.Fatalf("unset-rule stdout = %q", got)
	}
	stored, err = config.ReadStoredConfig(h.configPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := config.BranchRuleFor(stored, "gitlab.example.com/group/api"); ok {
		t.Fatal("unset-rule left the repository rule configured")
	}
}

func TestBranchesSetRuleRequiresAnExplicitUsernameValue(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(map[string]any{})

	if code := h.run("branches", "set-rule", "{username}/{ticket}/{slug}"); code != 1 {
		t.Fatalf("code = %d, stderr = %q", code, h.stderr.String())
	}
	if !strings.Contains(h.stderr.String(), "no username is configured") ||
		!strings.Contains(h.stderr.String(), "--username <gitlab-user-name>") {
		t.Fatalf("stderr = %q", h.stderr.String())
	}
	stored, err := config.ReadStoredConfig(h.configPath())
	if err != nil {
		t.Fatal(err)
	}
	if stored.BranchNaming != nil {
		t.Fatalf("invalid set-rule changed config: %+v", stored.BranchNaming)
	}
}

func TestBranchesSetRuleGitValidatesARepresentativeExpansion(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(map[string]any{})

	if code := h.run("branches", "set-rule", "{ticket}/bad name"); code != 1 {
		t.Fatalf("code = %d, stderr = %q", code, h.stderr.String())
	}
	if !strings.Contains(h.stderr.String(), "not a valid branch name") {
		t.Fatalf("stderr = %q", h.stderr.String())
	}
	stored, err := config.ReadStoredConfig(h.configPath())
	if err != nil {
		t.Fatal(err)
	}
	if stored.BranchNaming != nil {
		t.Fatalf("invalid set-rule changed config: %+v", stored.BranchNaming)
	}
}

func TestBranchesUsesTheAbsolutePathForALocalOnlyRepository(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(map[string]any{})

	if code := h.run("branches", "set-rule", "{ticket_lower}-{slug}"); code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, h.stderr.String())
	}
	stored, err := config.ReadStoredConfig(h.configPath())
	if err != nil {
		t.Fatal(err)
	}
	if template, _, ok := config.BranchRuleFor(stored, h.repo); !ok || template != "{ticket_lower}-{slug}" {
		t.Fatalf("local rule = %q, ok = %v", template, ok)
	}
}
