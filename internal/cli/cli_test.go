package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/snaylaker/lw/internal/config"
	"github.com/snaylaker/lw/internal/lwerr"
)

// stubCommand replaces one entry of the dispatch table for the length of a
// test, so routing is tested without depending on what a command body does.
func stubCommand(t *testing.T, name string, handler commandFunc) {
	t.Helper()
	previous, existed := commands[name]
	commands[name] = handler
	t.Cleanup(func() {
		if existed {
			commands[name] = previous
			return
		}
		delete(commands, name)
	})
}

func TestEveryCommandHasAHandler(t *testing.T) {
	for _, name := range append([]string{commandRun}, commandNames...) {
		if dispatch(name) == nil {
			t.Errorf("no handler for command %q", name)
		}
	}
	if want := len(commandNames) + 1; len(commands) != want {
		t.Errorf("dispatch table has %d entries, want %d", len(commands), want)
	}
	if dispatch("nope") != nil {
		t.Error("dispatch resolved a command that does not exist")
	}
}

func TestRunRoutesToTheCommand(t *testing.T) {
	cases := []struct {
		name    string
		argv    []string
		command string
		want    Options
	}{
		{"run", nil, commandRun, Options{}},
		{"run with flags", []string{"--repo", "/src", "--issue", "ENG-3971"}, commandRun, Options{Repo: "/src", Issue: "ENG-3971"}},
		{"launch", []string{"run", "--", "claude", "--model", "sonnet"}, commandLaunch, Options{Command: "run", Args: []string{"claude", "--model", "sonnet"}}},
		{"doctor", []string{"doctor"}, commandDoctor, Options{Command: "doctor"}},
		{"branches", []string{"branches", "show-rule"}, commandBranches, Options{Command: "branches", Args: []string{"show-rule"}}},
		{"context", []string{"context", "--json"}, commandContext, Options{Command: "context", JSON: true}},
		{"summary", []string{"summary", "narrowed it down"}, commandSummary, Options{Command: "summary", Args: []string{"narrowed it down"}}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var got Options
			called := 0
			stubCommand(t, testCase.command, func(ctx context.Context, opts Options, env *execEnv) int {
				called++
				got = opts
				if ctx == nil {
					t.Error("the command was given a nil context")
				}
				return 7
			})

			var stdout, stderr bytes.Buffer
			code := Run(testCase.argv, Deps{Stdout: &stdout, Stderr: &stderr, Dir: t.TempDir()})

			if called != 1 {
				t.Fatalf("handler called %d times, want 1", called)
			}
			if code != 7 {
				t.Errorf("code = %d, want the handler's 7", code)
			}
			if !reflect.DeepEqual(got, testCase.want) {
				t.Errorf("options =\n%#v\nwant\n%#v", got, testCase.want)
			}
			if stdout.Len() != 0 || stderr.Len() != 0 {
				t.Errorf("dispatch printed: stdout %q stderr %q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunFillsInEveryDefault(t *testing.T) {
	dir := t.TempDir()
	var captured *execEnv
	stubCommand(t, commandDoctor, func(ctx context.Context, opts Options, env *execEnv) int {
		captured = env
		return 0
	})

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"doctor"}, Deps{Stdout: &stdout, Stderr: &stderr, Dir: dir}); code != 0 {
		t.Fatalf("code = %d", code)
	}
	if captured == nil {
		t.Fatal("the handler was never called")
	}

	if captured.env == nil {
		t.Error("env map is nil, want the process environment")
	}
	if captured.platform != config.HostPlatform() {
		t.Errorf("platform = %q, want the host %q", captured.platform, config.HostPlatform())
	}
	if captured.dir != dir {
		t.Errorf("dir = %q, want %q", captured.dir, dir)
	}
	if captured.stdin == nil || captured.stdout != &stdout || captured.stderr != &stderr {
		t.Error("streams were not carried through")
	}
	if captured.run == nil || captured.http == nil ||
		captured.now == nil || captured.launch == nil || captured.child == nil {
		t.Errorf("a dependency was left nil: %+v", captured)
	}
}

func TestRunKeepsInjectedDependencies(t *testing.T) {
	clock := func() time.Time { return time.Unix(1785600000, 0) }
	environment := map[string]string{"HOME": "/home/tester"}

	var captured *execEnv
	stubCommand(t, commandDoctor, func(ctx context.Context, opts Options, env *execEnv) int {
		captured = env
		return 0
	})

	Run([]string{"doctor"}, Deps{
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
		Dir:      "/src/acme-api",
		Env:      environment,
		Platform: config.PlatformWindows,
		Now:      clock,
	})

	if captured == nil {
		t.Fatal("the handler was never called")
	}
	if captured.platform != config.PlatformWindows {
		t.Errorf("platform = %q, want %q", captured.platform, config.PlatformWindows)
	}
	if captured.dir != "/src/acme-api" {
		t.Errorf("dir = %q", captured.dir)
	}
	if !reflect.DeepEqual(captured.env, environment) {
		t.Errorf("env = %v, want %v", captured.env, environment)
	}
	if !captured.now().Equal(clock()) {
		t.Error("the injected clock was replaced")
	}
}

func TestRunHelpPrintsTheHelpAndSucceeds(t *testing.T) {
	for _, argv := range [][]string{{"--help"}, {"doctor", "--help"}} {
		var stdout, stderr bytes.Buffer
		// A stubbed doctor would fail the test if --help fell through to it.
		stubCommand(t, commandDoctor, func(ctx context.Context, opts Options, env *execEnv) int {
			t.Errorf("%q reached the command body", argv)
			return 1
		})

		code := Run(argv, Deps{Stdout: &stdout, Stderr: &stderr, Dir: t.TempDir()})
		if code != 0 {
			t.Errorf("Run(%q) = %d, want 0", argv, code)
		}
		if stdout.String() != HelpText() {
			t.Errorf("Run(%q) stdout = %q, want the help text", argv, stdout.String())
		}
		if stderr.Len() != 0 {
			t.Errorf("Run(%q) wrote %q to stderr", argv, stderr.String())
		}
	}
}

func TestRunVersionPrintsTheVersionAndSucceeds(t *testing.T) {
	previous := Version
	Version = "v1.2.3"
	t.Cleanup(func() { Version = previous })

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--version"}, Deps{Stdout: &stdout, Stderr: &stderr, Dir: t.TempDir()})

	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if stdout.String() != "lw v1.2.3\n" {
		t.Errorf("stdout = %q, want %q", stdout.String(), "lw v1.2.3\n")
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestRunHelpWinsOverVersion(t *testing.T) {
	var stdout bytes.Buffer
	Run([]string{"--version", "--help"}, Deps{Stdout: &stdout, Stderr: &bytes.Buffer{}, Dir: t.TempDir()})
	if stdout.String() != HelpText() {
		t.Errorf("stdout = %q, want the help text", stdout.String())
	}
}

func TestRunUsageErrorPrintsMessageThenHelpAndExits2(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--nope"}, Deps{Stdout: &stdout, Stderr: &stderr, Dir: t.TempDir()})

	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	want := "error: unknown flag --nope\n\n" + HelpText()
	if stderr.String() != want {
		t.Errorf("stderr = %q, want %q", stderr.String(), want)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want the usage error only on stderr", stdout.String())
	}
}

func TestRunCancellationPrintsNothingAndExits130(t *testing.T) {
	stubCommand(t, commandDoctor, func(ctx context.Context, opts Options, env *execEnv) int {
		return Report(lwerr.NewCancelled(), env.stderr)
	})

	var stdout, stderr bytes.Buffer
	code := Run([]string{"doctor"}, Deps{Stdout: &stdout, Stderr: &stderr, Dir: t.TempDir()})

	if code != 130 {
		t.Errorf("code = %d, want 130", code)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("cancellation printed: stdout %q stderr %q", stdout.String(), stderr.String())
	}
}

func TestRunReportedErrorExits1(t *testing.T) {
	stubCommand(t, commandDoctor, func(ctx context.Context, opts Options, env *execEnv) int {
		return Report(lwerr.New(lwerr.NotARepo,
			"not inside a git repository",
			"run lw from inside a repository, or pass --repo <path>"), env.stderr)
	})

	var stdout, stderr bytes.Buffer
	code := Run([]string{"doctor"}, Deps{Stdout: &stdout, Stderr: &stderr, Dir: t.TempDir()})

	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	want := "error: not inside a git repository\nnext: run lw from inside a repository, or pass --repo <path>\n"
	if stderr.String() != want {
		t.Errorf("stderr = %q, want %q", stderr.String(), want)
	}
}

// No command body is a placeholder any more: every one of them reaches real
// work through the real dispatch table. `lw context` outside a worktree
// succeeds silently; the rest are covered by their own files.
func TestEveryCommandReachesARealBody(t *testing.T) {
	home := t.TempDir()
	environment := map[string]string{"HOME": home, "LW_CONFIG_DIR": filepath.Join(home, "config")}

	cases := []struct {
		argv []string
		want int
	}{
		{[]string{"context"}, 0},
		{[]string{"summary"}, 2}, // the text is required
	}
	for _, testCase := range cases {
		var stdout, stderr bytes.Buffer
		code := Run(testCase.argv, Deps{
			Stdout: &stdout, Stderr: &stderr, Dir: t.TempDir(), Env: environment,
		})
		if code != testCase.want {
			t.Errorf("Run(%q) = %d, want %d (stderr %q)", testCase.argv, code, testCase.want, stderr.String())
		}
		if strings.Contains(stderr.String(), "not implemented") {
			t.Errorf("Run(%q) still reaches a stub", testCase.argv)
		}
	}
}
