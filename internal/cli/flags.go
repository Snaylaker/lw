package cli

import (
	"regexp"
	"slices"
	"strings"

	"github.com/snaylaker/lw/internal/lwerr"
)

// The command names, as they appear as the first positional argument. The
// path-only flow is the empty one: `lw` with no command at all. `lw run` uses
// that same flow, then starts an explicit child command in the worktree.
const (
	commandRun      = ""
	commandLaunch   = "run"
	commandDoctor   = "doctor"
	commandBranches = "branches"
	commandContext  = "context"
	commandSummary  = "summary"
	commandPrune    = "prune"
	commandLogout   = "logout"
)

// Options is the parsed command line: what to do, and with which flags. It is
// the only thing a command body reads about the invocation.
type Options struct {
	Command  string   // "" path-only flow, "run", "doctor", "branches", "context", "summary", "prune", "logout"
	Args     []string // remaining positional arguments for the subcommand
	Repo     string
	Issue    string
	Branch   string
	Provider string
	Username string
	JSON     bool // context --json
	// Yes applies `lw prune`. Without it the command only reports, because
	// deleting a checkout is not undoable.
	Yes bool
	// NoFetch skips the prune-fetch, keeping `lw prune` entirely offline.
	NoFetch bool
	// Auto and NoAuto persist the automatic-cleanup preference in config.json,
	// so the choice is made once with a flag rather than by hand-editing.
	Auto    bool
	NoAuto  bool
	Version bool
	Help    bool
}

// flagSpec is one accepted flag. Commands nil means every command accepts it;
// otherwise the flag is a usage error anywhere else, so `lw summary --json`
// fails loudly instead of being quietly ignored.
type flagSpec struct {
	boolean  bool
	commands []string
	apply    func(opts *Options, value string)
}

func (spec flagSpec) allowedOn(command string) bool {
	return spec.commands == nil || slices.Contains(spec.commands, command)
}

var flowCommands = []string{commandRun, commandLaunch}
var repositoryCommands = []string{commandRun, commandLaunch, commandBranches}

var flagSpecs = map[string]flagSpec{
	"--repo":     {commands: repositoryCommands, apply: func(o *Options, v string) { o.Repo = v }},
	"--issue":    {commands: flowCommands, apply: func(o *Options, v string) { o.Issue = v }},
	"--branch":   {commands: flowCommands, apply: func(o *Options, v string) { o.Branch = v }},
	"--provider": {commands: repositoryCommands, apply: func(o *Options, v string) { o.Provider = v }},
	"--username": {commands: []string{commandBranches}, apply: func(o *Options, v string) { o.Username = v }},
	"--json":     {boolean: true, commands: []string{commandContext}, apply: func(o *Options, _ string) { o.JSON = true }},
	"--yes":      {boolean: true, commands: []string{commandPrune}, apply: func(o *Options, _ string) { o.Yes = true }},
	"--no-fetch": {boolean: true, commands: []string{commandPrune}, apply: func(o *Options, _ string) { o.NoFetch = true }},
	"--auto":     {boolean: true, commands: []string{commandPrune}, apply: func(o *Options, _ string) { o.Auto = true }},
	"--no-auto":  {boolean: true, commands: []string{commandPrune}, apply: func(o *Options, _ string) { o.NoAuto = true }},
	"--version":  {boolean: true, apply: func(o *Options, _ string) { o.Version = true }},
	"--help":     {boolean: true, apply: func(o *Options, _ string) { o.Help = true }},
}

var commandNames = []string{
	commandLaunch,
	commandDoctor,
	commandBranches,
	commandContext,
	commandSummary,
	commandPrune,
	commandLogout,
}

// Parse turns argv — which excludes the program name — into Options. Every
// failure is a usage error: the message, then the help text, then exit 2.
//
// Both `--flag value` and `--flag=value` are accepted. A flag that wants a
// value never swallows the next flag: `lw --repo --help` is a missing value,
// not a repository called `--help`. A value that itself starts with a dash
// has to be written as `--flag=-value`. Everything after a bare `--` is
// positional, so `lw summary -- --not-a-flag` records that text.
func Parse(argv []string) (Options, *lwerr.Error) {
	var opts Options
	var positional []string
	var used []string
	endOfFlags := false

	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if !endOfFlags && arg == "--" {
			endOfFlags = true
			continue
		}
		if endOfFlags || !isFlag(arg) {
			positional = append(positional, arg)
			continue
		}

		name, value, hasValue := splitFlag(arg)
		spec, known := flagSpecs[name]
		if !known {
			return Options{}, usagef("unknown flag %s", name)
		}
		switch {
		case spec.boolean:
			if hasValue {
				return Options{}, usagef("%s does not take a value", name)
			}
		case hasValue:
			if value == "" {
				return Options{}, usagef("%s needs a value", name)
			}
		default:
			next, ok := valueAt(argv, i+1)
			if !ok {
				return Options{}, usagef("%s needs a value", name)
			}
			value = next
			i++
		}
		spec.apply(&opts, value)
		used = append(used, name)
	}

	if len(positional) > 0 {
		if !slices.Contains(commandNames, positional[0]) {
			return Options{}, usagef("unknown command %s", positional[0])
		}
		opts.Command = positional[0]
		if len(positional) > 1 {
			opts.Args = positional[1:]
		}
	}

	for _, name := range used {
		if !flagSpecs[name].allowedOn(opts.Command) {
			return Options{}, usagef("%s is not a valid flag for %s", name, commandLabel(opts.Command))
		}
	}

	// Without an explicit provider, direct mode preserves Linear's established
	// invocation contract. GitHub and Jira references use --provider so their
	// provider-specific validators can accept different shapes.
	_, _, prefixed := prefixedProviderReference(opts.Issue)
	if opts.Issue != "" && !prefixed && (opts.Provider == "" || strings.EqualFold(opts.Provider, "linear")) && !issueIdentifierRE.MatchString(opts.Issue) {
		return Options{}, usagef("--issue takes an identifier like ENG-3971, not %q", opts.Issue)
	}
	if opts.Command == commandLaunch && len(opts.Args) == 0 && !opts.Help && !opts.Version {
		return Options{}, usagef("lw run needs a command after --")
	}
	return opts, nil
}

var issueIdentifierRE = regexp.MustCompile(`^[A-Za-z0-9]+-[0-9]+$`)

// isFlag reports whether arg is a flag rather than a positional argument. A
// lone "-" is a positional; "--" is handled by the caller.
func isFlag(arg string) bool {
	return strings.HasPrefix(arg, "-") && arg != "-"
}

func splitFlag(arg string) (name, value string, hasValue bool) {
	if index := strings.IndexByte(arg, '='); index >= 0 {
		return arg[:index], arg[index+1:], true
	}
	return arg, "", false
}

// valueAt yields argv[i] only when it is not itself a flag, which is what stops
// a missing value from eating the next one.
func valueAt(argv []string, i int) (string, bool) {
	if i >= len(argv) || isFlag(argv[i]) {
		return "", false
	}
	return argv[i], true
}

// commandLabel names a command the way the user typed it, for error messages.
func commandLabel(command string) string {
	if command == commandRun {
		return "lw"
	}
	return "lw " + command
}
