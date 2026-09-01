// Package cli parses the command line, dispatches to one command, and owns the
// process's exit code. It is the only package that reads argv, touches the
// process's streams, or decides what "the real thing" means for a dependency:
// everything below it takes its collaborators as arguments, so a test drives
// the whole tool without a network, a credential helper, or a repository of the
// user's own.
package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/snaylaker/lw/internal/config"
	"github.com/snaylaker/lw/internal/credential"
	"github.com/snaylaker/lw/internal/gitrepo"
	"github.com/snaylaker/lw/internal/linear"
	"github.com/snaylaker/lw/internal/lwerr"
	"github.com/snaylaker/lw/internal/tui"
	issueprovider "github.com/snaylaker/lw/provider"
)

// httpTimeout bounds one provider request. A hung connection must become an
// actionable error rather than a picker that never paints.
const httpTimeout = 30 * time.Second

// Deps is every seam the CLI has. A zero value means "the real thing", so
// production wiring passes almost nothing and a test passes everything.
type Deps struct {
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	Env        map[string]string // nil means the process environment
	Platform   string            // empty means the host platform
	Dir        string            // working directory; empty means os.Getwd
	Run        gitrepo.Runner    // nil means gitrepo.DefaultRunner
	HTTPClient linear.Doer       // nil means a default client
	Credential credential.Runner // nil means the real shell, for credentialCommand
	Vault      credential.Vault  // nil means the system keychain with file fallback
	Now        func() time.Time
	Launch     func(deps tui.LauncherDeps) (tui.LauncherOutcome, error) // nil means tui.RunLauncher
	Child      ChildRunner                                              // nil means a direct child process
	Providers  []issueprovider.Provider                                 // optional compile-time provider extensions
}

// Main is the production entry point: the process's own argv, environment and
// streams, and nothing else.
func Main() int {
	return Run(os.Args[1:], Deps{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr})
}

// Run parses argv — which excludes the program name — and returns the exit
// code. It never calls os.Exit, so a test can assert on it.
func Run(argv []string, deps Deps) int {
	stdout := writerOr(deps.Stdout, os.Stdout)
	stderr := writerOr(deps.Stderr, os.Stderr)

	opts, err := Parse(argv)
	if err != nil {
		return Report(err, stderr)
	}
	// --help and --version answer before anything is resolved, so they work
	// from a deleted directory or a broken configuration.
	if opts.Help {
		fmt.Fprint(stdout, helpText)
		return 0
	}
	if opts.Version {
		fmt.Fprintf(stdout, "lw %s\n", Version)
		return 0
	}

	command := dispatch(opts.Command)
	if command == nil {
		return Report(usagef("unknown command %s", opts.Command), stderr)
	}

	env, err := newExecEnv(deps)
	if err != nil {
		return Report(err, stderr)
	}

	// Ctrl+C cancels the work in progress rather than killing the process
	// outright, so the exit code stays the tool's own.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	return command(ctx, opts, env)
}

// commandFunc is one command body. It returns the process exit code and has
// already reported whatever it is about to exit for.
type commandFunc func(ctx context.Context, opts Options, env *execEnv) int

// commands is the dispatch table. The empty key is the run: `lw` on its own.
var commands = map[string]commandFunc{
	commandRun:      runFlow,
	commandLaunch:   runLaunch,
	commandDoctor:   runDoctor,
	commandBranches: runBranches,
	commandContext:  runContext,
	commandSummary:  runSummary,
	commandPrune:    runPrune,
	commandLogout:   runLogout,
}

func dispatch(command string) commandFunc { return commands[command] }

// execEnv is Deps with every default filled in, so no command body has to know
// what a nil field meant.
type execEnv struct {
	stdin      io.Reader
	stdout     io.Writer
	stderr     io.Writer
	env        map[string]string
	platform   string
	dir        string
	run        gitrepo.Runner
	http       linear.Doer
	credential credential.Runner
	vault      credential.Vault
	now        func() time.Time
	launch     func(deps tui.LauncherDeps) (tui.LauncherOutcome, error)
	child      ChildRunner
	providers  map[issueprovider.ID]issueprovider.Provider
}

func newExecEnv(deps Deps) (*execEnv, *lwerr.Error) {
	providers := make(map[issueprovider.ID]issueprovider.Provider, len(deps.Providers))
	for _, candidate := range deps.Providers {
		if candidate != nil && candidate.ID() != "" {
			providers[candidate.ID()] = candidate
		}
	}
	env := &execEnv{
		stdin:      readerOr(deps.Stdin, os.Stdin),
		stdout:     writerOr(deps.Stdout, os.Stdout),
		stderr:     writerOr(deps.Stderr, os.Stderr),
		env:        deps.Env,
		platform:   deps.Platform,
		dir:        deps.Dir,
		run:        deps.Run,
		http:       deps.HTTPClient,
		credential: deps.Credential,
		vault:      deps.Vault,
		now:        deps.Now,
		launch:     deps.Launch,
		child:      deps.Child,
		providers:  providers,
	}
	if env.env == nil {
		env.env = config.OSEnv()
	}
	if env.platform == "" {
		env.platform = config.HostPlatform()
	}
	if env.run == nil {
		env.run = gitrepo.DefaultRunner
	}
	if env.http == nil {
		env.http = &http.Client{Timeout: httpTimeout}
	}
	if env.now == nil {
		env.now = time.Now
	}
	if env.launch == nil {
		env.launch = tui.RunLauncher
	}
	if env.child == nil {
		env.child = runChild
	}
	if env.vault == nil {
		env.vault = credential.NewVault(env.configPath())
	}
	if env.dir == "" {
		dir, err := os.Getwd()
		if err != nil {
			return env, lwerr.Wrap(err, lwerr.Internal,
				"cannot determine the current directory",
				"run lw from a directory that still exists, or pass --repo <path>")
		}
		env.dir = dir
	}
	return env, nil
}

// configPath is where config.json lives for this run.
func (env *execEnv) configPath() string {
	return config.Path(env.env, env.platform)
}

// nowMillis is the clock unit used by durable repository recents.
func (env *execEnv) nowMillis() int64 { return env.now().UnixMilli() }

func readerOr(value, fallback io.Reader) io.Reader {
	if value == nil {
		return fallback
	}
	return value
}

func writerOr(value, fallback io.Writer) io.Writer {
	if value == nil {
		return fallback
	}
	return value
}

// The command bodies live by concern: runFlow in run.go, runLaunch in
// launch.go, then branches.go, doctor.go, context.go, summary.go, prune.go and logout.go.
