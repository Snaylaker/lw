// Package doctor reports the state of the environment a run depends on, one
// line per check. Only a mandatory check changes the exit code: lw's promise
// needs git, a readable config file and a place to write, and everything else —
// a key — is advisory, because a worktree still opens without
// them.
//
// Every check is a pure function of injected dependencies, so the whole report
// can be produced without touching the network, the user's repositories, or a
// real credential helper. doctor makes no network call at all: it reports that a
// credential is *available*, never that a provider would accept it.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/snaylaker/lw/internal/config"
	"github.com/snaylaker/lw/internal/credential"
	"github.com/snaylaker/lw/internal/gitrepo"
	"github.com/snaylaker/lw/internal/lwerr"
	issueprovider "github.com/snaylaker/lw/provider"
)

// Status is the verdict of one check. The values double as the text printed in
// the six-character status column.
type Status string

const (
	StatusOK   Status = "ok"
	StatusWarn Status = "warn"
	StatusFail Status = "FAIL"
)

// Check is one line of the report.
type Check struct {
	Label  string
	Detail string
	Status Status
	// Mandatory checks are the ones that decide the exit code.
	Mandatory bool
	// NextAction is set when Status is StatusFail or StatusWarn and a next step
	// exists; it is printed after the detail, so no failure is a dead end.
	NextAction string
}

// Deps is the whole seam. Every zero value means "ask the host", so production
// callers pass nothing but Stdout while tests pass everything.
type Deps struct {
	Stdout     io.Writer
	Env        map[string]string // nil means the process environment
	Platform   string            // empty means the host platform
	Dir        string            // working directory; empty means os.Getwd
	Run        gitrepo.Runner    // nil means gitrepo.DefaultRunner
	ConfigPath string            // empty means config.Path(env, platform)
	Credential credential.Runner // nil means the real shell, for credentialCommand
	Vault      credential.Vault  // nil means the system keychain with file fallback
	Extensions map[string]string // provider ID to display name for custom binaries
}

// The labels, in report order. They are the check names from the specification,
// with the trailing clause of a name — "(count per event)", ", and its source" —
// dropped, because that clause describes the detail rather than the check.
const (
	labelPlatform     = "platform"
	labelGit          = "git"
	labelRepository   = "current directory is a usable repository"
	labelCredential   = "Linear credential"
	labelConfig       = "config file readable"
	labelWorktreeRoot = "worktree root writable"
)

// A malformed config file must never read as "nothing configured yet", and the
// next action has to say what is *not* at risk. This is the literal of SPEC §7,
// shared with internal/config so doctor's advice and the error a real run
// prints are the same sentence: config.json stores no key, so deleting it can
// cost preferences but never a credential.
const configNextAction = config.InvalidFileNextAction

// resolved is Deps with every default filled in, so no check re-derives them.
type resolved struct {
	env        map[string]string
	platform   string
	dir        string
	run        gitrepo.Runner
	configPath string
	credential credential.Runner
	vault      credential.Vault
	extensions map[string]string
}

func resolve(deps Deps) resolved {
	r := resolved{
		env:        deps.Env,
		platform:   deps.Platform,
		dir:        deps.Dir,
		run:        deps.Run,
		configPath: deps.ConfigPath,
		credential: deps.Credential,
		vault:      deps.Vault,
		extensions: deps.Extensions,
	}
	if r.env == nil {
		r.env = config.OSEnv()
	}
	if r.platform == "" {
		r.platform = config.HostPlatform()
	}
	if r.dir == "" {
		// A working directory we cannot name is not fatal: only the repository
		// check needs one, and it is advisory.
		if working, err := os.Getwd(); err == nil {
			r.dir = working
		}
	}
	if r.run == nil {
		r.run = gitrepo.DefaultRunner
	}
	if r.configPath == "" {
		r.configPath = config.Path(r.env, r.platform)
	}
	if r.vault == nil {
		r.vault = credential.NewVault(r.configPath)
	}
	// r.credential stays nil on purpose: credential.Resolve reads that as "the
	// real shell", and doctor has no better default to offer.
	return r
}

// Checks runs every check in report order. The config file is read once, so the
// report cannot contradict itself about what is configured.
func Checks(ctx context.Context, deps Deps) []Check {
	env := resolve(deps)
	stored, configErr := config.ReadStoredConfig(env.configPath)

	return []Check{
		platformCheck(env.platform),
		gitCheck(ctx, env),
		repositoryCheck(ctx, env),
		credentialCheck(ctx, env, stored),
		configCheck(env.configPath, configErr),
		worktreeRootCheck(stored, env.env),
	}
}

// FormatCheck renders the exact line, without a trailing newline: a
// six-character status column, the label, and the detail — plus the next action
// when the check has one.
func FormatCheck(check Check) string {
	line := fmt.Sprintf("%6s%s: %s", string(check.Status)+"  ", check.Label, check.Detail)
	if check.NextAction != "" {
		line += " — next: " + check.NextAction
	}
	return line
}

// Run prints every check and returns the process exit code.
func Run(ctx context.Context, deps Deps) int {
	stdout := deps.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	checks := Checks(ctx, deps)
	for _, check := range checks {
		_, _ = io.WriteString(stdout, FormatCheck(check)+"\n")
	}
	return ExitCode(checks)
}

// ExitCode is SPEC §11's gate, on its own so the rule can be asserted
// directly: "Exit 1 if any **mandatory** check fails." A failing advisory check
// never changes it.
func ExitCode(checks []Check) int {
	for _, check := range checks {
		if check.Mandatory && check.Status == StatusFail {
			return 1
		}
	}
	return 0
}

func platformCheck(platform string) Check {
	check := Check{Label: labelPlatform, Mandatory: true, Status: StatusOK, Detail: platform}
	switch platform {
	case "darwin", "linux", "win32", "windows":
		return check
	}
	check.Status = StatusFail
	check.Detail = "lw does not support " + platform
	check.NextAction = "use macOS, Linux, or Windows"
	return check
}

func gitCheck(ctx context.Context, env resolved) Check {
	check := Check{Label: labelGit, Mandatory: true, Status: StatusOK}
	result, err := env.run(ctx, env.dir, "git", []string{"--version"})
	if err != nil {
		check.Status = StatusFail
		check.Detail = "git could not be run"
		check.NextAction = "install git and make sure it is on PATH"
		return check
	}
	if result.ExitCode != 0 {
		check.Status = StatusFail
		check.Detail = "git --version exited " + strconv.Itoa(result.ExitCode)
		check.NextAction = "reinstall git, then re-run lw doctor"
		return check
	}
	check.Detail = firstLine(result.Stdout)
	if check.Detail == "" {
		check.Detail = "installed, version unknown"
	}
	return check
}

// repositoryCheck is advisory: `lw --repo <path>` works from anywhere, so a
// directory that is not a checkout only warns.
func repositoryCheck(ctx context.Context, env resolved) Check {
	check := Check{Label: labelRepository, Status: StatusOK}
	if env.dir == "" {
		check.Status = StatusWarn
		check.Detail = "the current directory cannot be determined"
		check.NextAction = gitrepo.NotARepoNextAction
		return check
	}
	validation := gitrepo.Validate(ctx, env.dir, env.run)
	if validation.Status == gitrepo.StatusOK {
		check.Detail = validation.Repo.Root
		return check
	}
	failure := gitrepo.ValidationError(validation)
	check.Status = StatusWarn
	check.Detail = failure.Message
	check.NextAction = failure.NextAction
	return check
}

// credentialCheck reports availability and *which source* a run would use, and
// nothing else about the key: not the key, not a prefix of it, not its length.
// A missing key is a warning, never a failure — lw is perfectly installable
// before you have one, and `lw doctor` is exactly what you run to find out what
// is still missing.
//
// Resolving the credential runs the user's own credentialCommand when one is
// configured, which is the only way to answer whether it would work. That is a
// read of their own secret store, still no network call, and the key never
// leaves this function.
func credentialCheck(ctx context.Context, env resolved, stored *config.StoredConfig) Check {
	providerID := providerName(stored, env.env)
	switch providerID {
	case issueprovider.GitHub:
		return githubCredentialCheck(env.env)
	case issueprovider.Jira:
		return jiraCredentialCheck(env.env)
	case issueprovider.Linear:
		return linearCredentialCheck(ctx, env, stored)
	default:
		if name := strings.TrimSpace(env.extensions[string(providerID)]); name != "" {
			return Check{Label: name + " provider", Status: StatusOK, Detail: "available as a compiled extension"}
		}
		return Check{Label: "issue provider", Status: StatusWarn,
			Detail:     "unknown provider " + string(providerID),
			NextAction: "use linear, github, jira, or compile the configured extension"}
	}
}

func linearCredentialCheck(ctx context.Context, env resolved, stored *config.StoredConfig) Check {
	check := Check{Label: labelCredential, Status: StatusOK}
	found, err := credential.Resolve(ctx, credential.Options{
		Env:        env.env,
		Platform:   env.platform,
		Command:    credentialCommand(stored),
		ConfigPath: env.configPath,
		Run:        env.credential,
		Vault:      env.vault,
	})
	if err != nil {
		check.Status = StatusWarn
		check.Detail, check.NextAction = describe(err,
			"no Linear API key is available",
			"run lw to connect, set "+credential.EnvVar+", or add credentialCommand to "+env.configPath)
		return check
	}
	check.Detail = "available via " + string(found.Source)
	return check
}

func githubCredentialCheck(env map[string]string) Check {
	check := Check{Label: "GitHub credential", Status: StatusOK}
	source := "GITHUB_TOKEN"
	token := strings.TrimSpace(env[source])
	if token == "" {
		source = "GH_TOKEN"
		token = strings.TrimSpace(env[source])
	}
	if token != "" {
		check.Detail = "available via " + source
		return check
	}
	check.Status = StatusWarn
	check.Detail = "no token; only public GitHub issues are available"
	check.NextAction = "set GITHUB_TOKEN for private issues and a higher API rate limit"
	return check
}

func jiraCredentialCheck(env map[string]string) Check {
	check := Check{Label: "Jira credential", Status: StatusOK}
	var missing []string
	for _, name := range []string{"JIRA_BASE_URL", "JIRA_EMAIL", "JIRA_API_TOKEN"} {
		if strings.TrimSpace(env[name]) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		check.Detail = "available via Jira environment variables"
		return check
	}
	check.Status = StatusWarn
	check.Detail = "missing " + strings.Join(missing, ", ")
	check.NextAction = "set the Jira Cloud URL, account email, and API token"
	return check
}

func providerName(stored *config.StoredConfig, env map[string]string) issueprovider.ID {
	value := strings.ToLower(strings.TrimSpace(env["LW_ISSUE_PROVIDER"]))
	if value == "" && stored != nil {
		value = stored.IssueProvider
	}
	if value == "" || value == "linear" {
		return issueprovider.Linear
	}
	return issueprovider.ID(value)
}

// credentialCommand is the configured helper, if the config file could be read
// at all. A file we could not parse yields none, so the environment still
// answers and the report stays useful.
func credentialCommand(stored *config.StoredConfig) string {
	if stored == nil {
		return ""
	}
	return stored.CredentialCommand
}

// configCheck is mandatory because a file we cannot parse is not the same as no
// file: reading it as empty would silently discard someone's preferences — and
// with them credentialCommand, which is how the key is found at all.
func configCheck(path string, configErr error) Check {
	check := Check{Label: labelConfig, Mandatory: true, Status: StatusOK}
	if configErr == nil {
		check.Detail = path
		if _, err := os.Stat(path); err != nil {
			check.Detail = path + " (no configuration yet)"
		}
		return check
	}
	check.Status = StatusFail
	check.NextAction = configNextAction
	// Only a file that exists and cannot be read produces a path error; anything
	// else that survived ReadStoredConfig is a document we could not use.
	var pathErr *fs.PathError
	if errors.As(configErr, &pathErr) {
		check.Detail = "the config file " + path + " cannot be read"
		return check
	}
	check.Detail = "the config file " + path + " is not valid JSON"
	return check
}

func worktreeRootCheck(stored *config.StoredConfig, env map[string]string) Check {
	check := Check{Label: labelWorktreeRoot, Mandatory: true, Status: StatusOK}
	root := config.ResolveWorktreeRoot(stored, env)
	missing, err := verifyWritable(root)
	switch {
	case errors.Is(err, errNotDirectory):
		check.Status = StatusFail
		check.Detail = "the worktree root " + root + " exists and is not a directory"
		check.NextAction = `remove it, or set "worktreeRoot" in config.json to another path`
	case err != nil:
		check.Status = StatusFail
		check.Detail = "the worktree root " + root + " is not writable"
		check.NextAction = `fix its permissions, or set "worktreeRoot" in config.json to a writable path`
	case missing:
		check.Detail = root + " (will be created)"
	default:
		check.Detail = root
	}
	return check
}

var errNotDirectory = errors.New("not a directory")

// verifyWritable proves writability by writing: it creates one temporary file in
// the root — or in the nearest existing ancestor, since the root is created on
// demand — and removes it again, so a clean environment stays clean. missing
// reports that the root itself does not exist yet.
func verifyWritable(root string) (missing bool, err error) {
	target, missing, err := nearestExistingDir(root)
	if err != nil {
		return missing, err
	}
	file, err := os.CreateTemp(target, ".lw-doctor-*")
	if err != nil {
		return missing, err
	}
	name := file.Name()
	closeErr := file.Close()
	removeErr := os.Remove(name)
	if closeErr != nil {
		return missing, closeErr
	}
	return missing, removeErr
}

func nearestExistingDir(root string) (dir string, missing bool, err error) {
	target := root
	for {
		info, statErr := os.Stat(target)
		if statErr == nil {
			if !info.IsDir() {
				return "", false, fmt.Errorf("%s: %w", target, errNotDirectory)
			}
			return target, target != root, nil
		}
		if !errors.Is(statErr, fs.ErrNotExist) {
			return "", false, statErr
		}
		parent := filepath.Dir(target)
		if parent == target {
			return "", false, statErr
		}
		target = parent
	}
}

// describe prefers the actionable pair an lwerr carries, falling back to the
// caller's wording rather than echoing an unknown error's text — which, on this
// path, could be a credential helper's own output.
func describe(err error, message, nextAction string) (string, string) {
	if e, ok := lwerr.As(err); ok {
		return e.Message, e.NextAction
	}
	return message, nextAction
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(line)
}
