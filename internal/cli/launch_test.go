package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/snaylaker/lw/internal/domain"
)

func TestRunLaunchStartsTheExactCommandInsideTheResolvedWorktree(t *testing.T) {
	h := newHarness(t).withKey("test-key")
	h.writeConfig(map[string]any{})
	issue := testIssue("DEMO-4009")
	h.picks(issue)

	var gotDir string
	var gotArgv, gotEnv []string
	h.child = func(dir string, argv, environ []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
		gotDir = dir
		gotArgv = append([]string(nil), argv...)
		gotEnv = append([]string(nil), environ...)
		if stdin == nil {
			t.Error("child stdin is nil")
		}
		_, _ = io.WriteString(stdout, "agent stdout\n")
		_, _ = io.WriteString(stderr, "agent stderr\n")
		return 0, nil
	}

	code := h.run("run", "--", "claude", "--model", "sonnet")
	if code != 0 {
		t.Fatalf("code = %d, stderr %q", code, h.stderr.String())
	}
	if want := h.worktreeFor(issue.Identifier); gotDir != want {
		t.Errorf("child dir = %q, want %q", gotDir, want)
	}
	if want := []string{"claude", "--model", "sonnet"}; !reflect.DeepEqual(gotArgv, want) {
		t.Errorf("child argv = %q, want %q", gotArgv, want)
	}
	if strings.Contains(strings.Join(gotEnv, "\n"), "LINEAR_API_KEY=") {
		t.Errorf("child inherited LINEAR_API_KEY: %q", gotEnv)
	}
	if h.stdout.String() != "agent stdout\n" {
		t.Errorf("stdout = %q; the worktree path must not precede child output", h.stdout.String())
	}
	if h.stderr.String() != "agent stderr\n" {
		t.Errorf("stderr = %q", h.stderr.String())
	}
	if _, err := os.Stat(gotDir); err != nil {
		t.Errorf("worktree was not created: %v", err)
	}
}

func TestRunLaunchPropagatesTheChildExitCode(t *testing.T) {
	h := newHarness(t).withKey("test-key")
	h.writeConfig(map[string]any{})
	h.picks(testIssue("DEMO-4009"))
	h.child = func(string, []string, []string, io.Reader, io.Writer, io.Writer) (int, error) {
		return 23, nil
	}

	if code := h.run("run", "--", "codex"); code != 23 {
		t.Errorf("code = %d, want child exit code 23", code)
	}
	if h.stdout.Len() != 0 || h.stderr.Len() != 0 {
		t.Errorf("lw added output around the child: stdout %q stderr %q", h.stdout.String(), h.stderr.String())
	}
}

func TestRunLaunchReportsACommandThatCannotStart(t *testing.T) {
	h := newHarness(t).withKey("test-key")
	h.writeConfig(map[string]any{})
	h.picks(testIssue("DEMO-4009"))
	h.child = func(string, []string, []string, io.Reader, io.Writer, io.Writer) (int, error) {
		return -1, errors.New("executable not found")
	}

	if code := h.run("run", "--", "missing-agent"); code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	for _, want := range []string{
		`error: could not start "missing-agent" in `,
		`next: install "missing-agent" and make sure it is on PATH, then retry`,
	} {
		if !strings.Contains(h.stderr.String(), want) {
			t.Errorf("stderr %q does not contain %q", h.stderr.String(), want)
		}
	}
}

func TestLaunchMapsASignalledChildAfterCancellationTo130(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	env := &execEnv{
		stdin:  strings.NewReader(""),
		stdout: io.Discard,
		stderr: io.Discard,
		env:    map[string]string{},
		child: func(string, []string, []string, io.Reader, io.Writer, io.Writer) (int, error) {
			return -1, nil
		},
	}
	f := &flow{opts: Options{Args: []string{"claude"}}, env: env}
	if code := f.launch(ctx, domain.FlowResult{CheckoutPath: t.TempDir()}); code != 130 {
		t.Errorf("code = %d, want 130", code)
	}
}

func TestChildEnvironmentIsSortedAndRemovesProviderSecretsCaseInsensitively(t *testing.T) {
	got := childEnvironment(map[string]string{
		"ZED":            "last",
		"linear_api_key": "secret",
		"github_token":   "secret",
		"GH_TOKEN":       "secret",
		"jira_api_token": "secret",
		"ALPHA":          "first",
	})
	want := []string{"ALPHA=first", "ZED=last"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("environment = %q, want %q", got, want)
	}
}

func TestRunChildUsesTheRequestedDirectoryArgumentsAndStreams(t *testing.T) {
	dir := realPath(t, t.TempDir())
	script := filepath.Join(dir, "agent")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' \"$PWD\" \"$1\"\nprintf 'err\\n' >&2\nexit 17\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	code, err := runChild(dir, []string{script, "argument with spaces"}, []string{"PATH=/usr/bin:/bin"}, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if code != 17 {
		t.Errorf("code = %d, want 17", code)
	}
	if want := dir + "\nargument with spaces\n"; stdout.String() != want {
		t.Errorf("stdout = %q, want %q", stdout.String(), want)
	}
	if stderr.String() != "err\n" {
		t.Errorf("stderr = %q", stderr.String())
	}
}
