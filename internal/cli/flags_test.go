package cli

import (
	"reflect"
	"slices"
	"testing"

	"github.com/snaylaker/lw/internal/lwerr"
)

func TestParseAcceptedInvocations(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want Options
	}{
		{"no arguments is interactive search", nil, Options{}},
		{"separate repo", []string{"--repo", "/src/acme-api"}, Options{Repo: "/src/acme-api"}},
		{"attached repo", []string{"--repo=/src/acme-api"}, Options{Repo: "/src/acme-api"}},
		{"fully direct", []string{"--repo", "/src/acme-api", "--issue", "DEMO-4009", "--branch", "alex/demo-4009-fix"}, Options{Repo: "/src/acme-api", Issue: "DEMO-4009", Branch: "alex/demo-4009-fix"}},
		{"attached issue", []string{"--issue=DEMO-4009"}, Options{Issue: "DEMO-4009"}},
		{"GitHub issue", []string{"--provider", "github", "--issue", "acme/api#42"}, Options{Provider: "github", Issue: "acme/api#42"}},
		{"Jira issue", []string{"--provider", "jira", "--issue", "OPS-42"}, Options{Provider: "jira", Issue: "OPS-42"}},
		{"launch", []string{"run", "--", "claude"}, Options{Command: "run", Args: []string{"claude"}}},
		{"launch flags and arguments", []string{"run", "--repo", "/src/acme-api", "--issue", "DEMO-4009", "--", "codex", "--full-auto"}, Options{Command: "run", Repo: "/src/acme-api", Issue: "DEMO-4009", Args: []string{"codex", "--full-auto"}}},
		{"launch without separator", []string{"run", "claude"}, Options{Command: "run", Args: []string{"claude"}}},
		{"launch help", []string{"run", "--help"}, Options{Command: "run", Help: true}},
		{"doctor", []string{"doctor"}, Options{Command: "doctor"}},
		{"set branch rule", []string{"branches", "set-rule", "--repo", "/src/api", "--username", "alex", "{username}/{ticket}/{slug}"}, Options{Command: "branches", Args: []string{"set-rule", "{username}/{ticket}/{slug}"}, Repo: "/src/api", Username: "alex"}},
		{"preview branch rule", []string{"branches", "preview", "ENG-3971"}, Options{Command: "branches", Args: []string{"preview", "ENG-3971"}}},
		{"context json", []string{"context", "--json"}, Options{Command: "context", JSON: true}},
		{"flag before command", []string{"--json", "context"}, Options{Command: "context", JSON: true}},
		{"summary", []string{"summary", "narrowed to retry"}, Options{Command: "summary", Args: []string{"narrowed to retry"}}},
		{"end flags", []string{"summary", "--", "--not-a-flag"}, Options{Command: "summary", Args: []string{"--not-a-flag"}}},
		{"version", []string{"--version"}, Options{Version: true}},
		{"help", []string{"--help"}, Options{Help: true}},
		{"subcommand help", []string{"doctor", "--help"}, Options{Command: "doctor", Help: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.argv)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.argv, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Parse(%q) = %#v, want %#v", tc.argv, got, tc.want)
			}
		})
	}
}

func TestRemovedProjectAndCacheFlagsFailLoudly(t *testing.T) {
	for _, argv := range [][]string{{"--project", "Billing"}, {"--refresh"}, {"projects"}} {
		if got, err := Parse(argv); err == nil {
			t.Errorf("Parse(%q) = %#v, want usage error", argv, got)
		}
	}
}

func TestParseUsageErrors(t *testing.T) {
	cases := []struct {
		argv    []string
		message string
	}{
		{[]string{"--nope"}, "unknown flag --nope"},
		{[]string{"-h"}, "unknown flag -h"},
		{[]string{"nope"}, "unknown command nope"},
		{[]string{"--repo"}, "--repo needs a value"},
		{[]string{"--repo", "--help"}, "--repo needs a value"},
		{[]string{"--repo="}, "--repo needs a value"},
		{[]string{"--branch"}, "--branch needs a value"},
		{[]string{"doctor", "--branch", "topic"}, "--branch is not a valid flag for lw doctor"},
		{[]string{"doctor", "--username", "alex"}, "--username is not a valid flag for lw doctor"},
		{[]string{"--json"}, "--json is not a valid flag for lw"},
		{[]string{"summary", "--json", "text"}, "--json is not a valid flag for lw summary"},
		{[]string{"doctor", "--repo", "/src"}, "--repo is not a valid flag for lw doctor"},
		{[]string{"run"}, "lw run needs a command after --"},
		{[]string{"run", "--repo", "/src"}, "lw run needs a command after --"},
		{[]string{"--issue", "3971"}, `--issue takes an identifier like ENG-3971, not "3971"`},
		{[]string{"--issue", "ENG-"}, `--issue takes an identifier like ENG-3971, not "ENG-"`},
		{[]string{"--issue", "ENG-39a"}, `--issue takes an identifier like ENG-3971, not "ENG-39a"`},
		{[]string{"--issue", "ENG_3971"}, `--issue takes an identifier like ENG-3971, not "ENG_3971"`},
		{[]string{"--issue", "ENG-3971 "}, `--issue takes an identifier like ENG-3971, not "ENG-3971 "`},
	}
	for _, tc := range cases {
		got, err := Parse(tc.argv)
		if err == nil {
			t.Fatalf("Parse(%q) = %#v, want usage error", tc.argv, got)
		}
		if err.Kind != UsageKind || err.Message != tc.message || err.NextAction == "" {
			t.Errorf("Parse(%q) error = %+v", tc.argv, err)
		}
		if !reflect.DeepEqual(got, Options{}) {
			t.Errorf("options = %#v", got)
		}
	}
}

func TestParseAcceptsEveryValidIssueIdentifier(t *testing.T) {
	for _, identifier := range []string{"ENG-3971", "eng-3971", "a1-2", "ENG-0009999", "X-1"} {
		opts, err := Parse([]string{"--issue", identifier})
		if err != nil || opts.Issue != identifier {
			t.Errorf("Parse(%q) = %#v, %v", identifier, opts, err)
		}
	}
}

func TestParseAppliesNothingOnFailure(t *testing.T) {
	got, err := Parse([]string{"--repo", "/src", "--nope"})
	if err == nil || !reflect.DeepEqual(got, Options{}) {
		t.Fatalf("Parse = %#v, %v", got, err)
	}
}

func TestParseUsageErrorsAreLwErrors(t *testing.T) {
	_, err := Parse([]string{"--nope"})
	if err == nil || !lwerr.Is(err, UsageKind) {
		t.Fatalf("error = %v", err)
	}
}

func TestParseHasNoLoginCommand(t *testing.T) {
	for _, argv := range [][]string{{"auth"}, {"login"}} {
		if got, err := Parse(argv); err == nil {
			t.Errorf("Parse(%q) = %#v", argv, got)
		}
	}
	if slices.Contains(commandNames, "auth") {
		t.Error("auth is in command table")
	}
	if got, err := Parse([]string{"logout"}); err != nil || got.Command != commandLogout {
		t.Errorf("Parse(logout) = %#v, %v", got, err)
	}
}

func TestParseKnowsEveryCommand(t *testing.T) {
	for _, name := range commandNames {
		argv := []string{name}
		if name == commandLaunch {
			argv = append(argv, "claude")
		}
		opts, err := Parse(argv)
		if err != nil || opts.Command != name {
			t.Errorf("Parse(%q) = %#v, %v", argv, opts, err)
		}
	}
}
