package doctor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/snaylaker/lw/internal/config"
	"github.com/snaylaker/lw/internal/credential"
	"github.com/snaylaker/lw/internal/gitrepo"
)

// ---------------------------------------------------------------- test doubles

// shellRunner stands in for the platform shell that credentialCommand runs
// through. The real one is never invoked: a credential helper reads the user's
// own secret store, and a test that ran it would be reading a real key.
type shellRunner struct {
	calls  [][]string
	stdout string
	err    error
}

func (r *shellRunner) run(_ context.Context, shell string, args, _ []string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{shell}, args...))
	return []byte(r.stdout), r.err
}

type missingVault struct{}

func (missingVault) Load() (string, credential.Source, error) {
	return "", "", credential.ErrNotFound
}
func (missingVault) Save(string, credential.Store) (credential.Location, error) {
	return credential.KeyringLocation(), nil
}
func (missingVault) Delete() error { return nil }

type foundVault struct{}

func (foundVault) Load() (string, credential.Source, error) {
	return "saved-test-key", credential.SourceKeyring, nil
}
func (foundVault) Save(string, credential.Store) (credential.Location, error) {
	return credential.KeyringLocation(), nil
}
func (foundVault) Delete() error { return nil }

// gitRunner answers `git --version` and reports every other invocation as a
// directory that is not a repository, so a test that cares about one of them is
// not perturbed by the other.
func gitRunner(version string, versionExit int, versionErr error) gitrepo.Runner {
	return func(_ context.Context, _, name string, args []string) (gitrepo.ExecResult, error) {
		if name == "git" && len(args) == 1 && args[0] == "--version" {
			if versionErr != nil {
				return gitrepo.ExecResult{}, versionErr
			}
			return gitrepo.ExecResult{Stdout: version, ExitCode: versionExit}, nil
		}
		return gitrepo.ExecResult{Stderr: "not a git repository", ExitCode: 128}, nil
	}
}

// tempDir resolves symlinks because macOS hands out /var/folders paths that
// really live under /private, and the checks report resolved paths.
func tempDir(t *testing.T) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// baseDeps is a fully injected environment: no process environment, no real
// shell, no real PATH, no real git, and a home under t.TempDir(). The credential
// runner fails the test if it is called, so every test that does not configure a
// credentialCommand also asserts that none was run.
func baseDeps(t *testing.T) Deps {
	t.Helper()
	home := tempDir(t)
	configDir := filepath.Join(home, "config")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	return Deps{
		Stdout:   io.Discard,
		Env:      map[string]string{"HOME": home, "LW_CONFIG_DIR": configDir, "PATH": ""},
		Platform: "darwin",
		Dir:      home,
		Run:      gitRunner("git version 2.42.0\n", 0, nil),
		Vault:    missingVault{},
		Credential: func(context.Context, string, []string, []string) ([]byte, error) {
			t.Errorf("no credentialCommand is configured, so no shell may be run")
			return nil, errors.New("unexpected credential command")
		},
	}
}

func writeConfig(t *testing.T, deps Deps, contents string) {
	t.Helper()
	path := config.Path(deps.Env, deps.Platform)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, deps Deps) []Check {
	t.Helper()
	return Checks(context.Background(), deps)
}

func report(checks []Check) string {
	var out bytes.Buffer
	for _, check := range checks {
		out.WriteString(FormatCheck(check) + "\n")
	}
	return out.String()
}

func find(t *testing.T, checks []Check, label string) Check {
	t.Helper()
	for _, check := range checks {
		if check.Label == label {
			return check
		}
	}
	t.Fatalf("no check labelled %q in %v", label, labels(checks))
	return Check{}
}

func labels(checks []Check) []string {
	names := make([]string, 0, len(checks))
	for _, check := range checks {
		names = append(names, check.Label)
	}
	return names
}

func expect(t *testing.T, check Check, status Status, detail, nextAction string) {
	t.Helper()
	if check.Status != status {
		t.Errorf("%s: status = %q, want %q (detail %q)", check.Label, check.Status, status, check.Detail)
	}
	if check.Detail != detail {
		t.Errorf("%s: detail = %q, want %q", check.Label, check.Detail, detail)
	}
	if check.NextAction != nextAction {
		t.Errorf("%s: next action = %q, want %q", check.Label, check.NextAction, nextAction)
	}
}

// ------------------------------------------------------------------ formatting

// The status column is exactly six characters, so every label starts in the same
// column whatever the verdict.
func TestFormatCheckStatusColumn(t *testing.T) {
	cases := []struct {
		status Status
		want   string
	}{
		{StatusOK, "  ok  platform: darwin"},
		{StatusWarn, "warn  platform: darwin"},
		{StatusFail, "FAIL  platform: darwin"},
	}
	for _, tc := range cases {
		line := FormatCheck(Check{Label: "platform", Detail: "darwin", Status: tc.status})
		if line != tc.want {
			t.Errorf("FormatCheck(%q) = %q, want %q", tc.status, line, tc.want)
		}
		if prefix := line[:6]; len(prefix) != 6 || !strings.HasSuffix(prefix, "  ") {
			t.Errorf("status column = %q, want six characters ending in two spaces", prefix)
		}
	}
}

// A failing check reports its reason as "<message> — next: <next action>", with
// an em dash.
func TestFormatCheckFailureCarriesNextAction(t *testing.T) {
	line := FormatCheck(Check{
		Label:      "git",
		Detail:     "git could not be run",
		Status:     StatusFail,
		NextAction: "install git and make sure it is on PATH",
	})
	want := "FAIL  git: git could not be run — next: install git and make sure it is on PATH"
	if line != want {
		t.Fatalf("FormatCheck = %q, want %q", line, want)
	}
	if strings.Contains(line, " - next:") || strings.Contains(line, " -- next:") {
		t.Errorf("the separator must be an em dash: %q", line)
	}
}

func TestFormatCheckOmitsNextActionWhenAbsent(t *testing.T) {
	line := FormatCheck(Check{Label: "platform", Detail: "darwin", Status: StatusOK})
	if line != "  ok  platform: darwin" {
		t.Fatalf("FormatCheck = %q", line)
	}
	if strings.Contains(line, "next:") {
		t.Errorf("a check with no next action must not print one: %q", line)
	}
}

func TestFormatCheckHasNoTrailingNewline(t *testing.T) {
	line := FormatCheck(Check{Label: "platform", Detail: "darwin", Status: StatusOK})
	if strings.HasSuffix(line, "\n") {
		t.Fatalf("FormatCheck must not end in a newline: %q", line)
	}
}

// -------------------------------------------------------------- the check list

func TestChecksOrderAndMandatoryFlags(t *testing.T) {
	want := []struct {
		label     string
		mandatory bool
	}{
		{"platform", true},
		{"git", true},
		{"current directory is a usable repository", false},
		{"Linear credential", false},
		{"config file readable", true},
		{"worktree root writable", true},
	}
	checks := run(t, baseDeps(t))
	if len(checks) != len(want) {
		t.Fatalf("got %d checks %v, want %d", len(checks), labels(checks), len(want))
	}
	for i, expected := range want {
		if checks[i].Label != expected.label {
			t.Errorf("check %d = %q, want %q", i, checks[i].Label, expected.label)
		}
		if checks[i].Mandatory != expected.mandatory {
			t.Errorf("%s: mandatory = %v, want %v", checks[i].Label, checks[i].Mandatory, expected.mandatory)
		}
	}
}

// ---------------------------------------------------------------- one by one

func TestPlatformCheckAcceptsSupportedPlatforms(t *testing.T) {
	for _, platform := range []string{"darwin", "linux", "win32", "windows"} {
		deps := baseDeps(t)
		deps.Platform = platform
		expect(t, find(t, run(t, deps), "platform"), StatusOK, platform, "")
	}
}

func TestPlatformCheckFailsOnUnsupportedPlatform(t *testing.T) {
	deps := baseDeps(t)
	deps.Platform = "plan9"
	check := find(t, run(t, deps), "platform")
	expect(t, check, StatusFail, "lw does not support plan9", "use macOS, Linux, or Windows")
	if !check.Mandatory {
		t.Error("the platform check is mandatory")
	}
}

func TestPlatformCheckDefaultsToTheHostPlatform(t *testing.T) {
	deps := baseDeps(t)
	deps.Platform = ""
	// Env stays injected, so nothing here reads the developer's environment.
	expect(t, find(t, run(t, deps), "platform"), StatusOK, config.HostPlatform(), "")
}

func TestGitCheckReportsTheVersion(t *testing.T) {
	deps := baseDeps(t)
	deps.Run = gitRunner("git version 2.42.0\n", 0, nil)
	expect(t, find(t, run(t, deps), "git"), StatusOK, "git version 2.42.0", "")
}

func TestGitCheckFailsWhenGitCannotBeRun(t *testing.T) {
	deps := baseDeps(t)
	deps.Run = gitRunner("", 0, errors.New("exec: \"git\": executable file not found in $PATH"))
	check := find(t, run(t, deps), "git")
	expect(t, check, StatusFail, "git could not be run", "install git and make sure it is on PATH")
}

func TestGitCheckFailsOnNonZeroExit(t *testing.T) {
	deps := baseDeps(t)
	deps.Run = gitRunner("", 127, nil)
	expect(t, find(t, run(t, deps), "git"), StatusFail,
		"git --version exited 127", "reinstall git, then re-run lw doctor")
}

func TestGitCheckReportsUnknownVersionOnSilentSuccess(t *testing.T) {
	deps := baseDeps(t)
	deps.Run = gitRunner("   \n", 0, nil)
	expect(t, find(t, run(t, deps), "git"), StatusOK, "installed, version unknown", "")
}

// Real git, real repository, temporary directory: the one place a fake would
// lie about what git considers a checkout.
func TestRepositoryCheckReportsTheCheckoutRoot(t *testing.T) {
	requireGit(t)
	repo := tempDir(t)
	initRepo(t, repo)
	commitEmpty(t, repo)

	deps := baseDeps(t)
	deps.Dir = filepath.Join(repo, "nested")
	if err := os.MkdirAll(deps.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	deps.Run = gitrepo.DefaultRunner

	check := find(t, run(t, deps), "current directory is a usable repository")
	expect(t, check, StatusOK, repo, "")
	if check.Mandatory {
		t.Error("a directory that is not a checkout must not fail the run; the check is advisory")
	}
}

func TestRepositoryCheckWarnsOutsideARepository(t *testing.T) {
	requireGit(t)
	dir := tempDir(t)
	deps := baseDeps(t)
	deps.Dir = dir
	deps.Run = gitrepo.DefaultRunner

	check := find(t, run(t, deps), "current directory is a usable repository")
	if check.Status != StatusWarn {
		t.Fatalf("status = %q, want warn (detail %q)", check.Status, check.Detail)
	}
	// SPEC §4's words, reported through the doctor's own shape.
	if check.Detail != "not inside a git repository" {
		t.Errorf("detail = %q, want SPEC §4's message", check.Detail)
	}
	if check.NextAction != "run lw from inside a repository, or pass --repo <path>" {
		t.Errorf("next action = %q, want SPEC §4's", check.NextAction)
	}
}

func TestRepositoryCheckWarnsWithoutCommits(t *testing.T) {
	requireGit(t)
	repo := filepath.Join(tempDir(t), "acme-api")
	initRepo(t, repo)

	deps := baseDeps(t)
	deps.Dir = repo
	deps.Run = gitrepo.DefaultRunner

	check := find(t, run(t, deps), "current directory is a usable repository")
	if check.Status != StatusWarn {
		t.Fatalf("status = %q, want warn (detail %q)", check.Status, check.Detail)
	}
	if check.Detail != "acme-api has no commits yet" {
		t.Errorf("detail = %q, want SPEC §4's message", check.Detail)
	}
	if check.NextAction != "make an initial commit, then re-run" {
		t.Errorf("next action = %q, want SPEC §4's", check.NextAction)
	}
}

func TestRepositoryCheckWarnsWhenTheDirectoryIsUnknown(t *testing.T) {
	deps := baseDeps(t)
	deps.Dir = ""
	// An unreadable working directory is the only way Getwd fails, so the
	// behaviour is exercised through the same branch a failed lookup takes.
	deps.Run = func(context.Context, string, string, []string) (gitrepo.ExecResult, error) {
		return gitrepo.ExecResult{ExitCode: 128}, nil
	}
	check := find(t, run(t, deps), "current directory is a usable repository")
	if check.Status != StatusWarn {
		t.Fatalf("status = %q, want warn", check.Status)
	}
}

// --------------------------------------------------------- the Linear credential

// The environment is the second source, and the report names it — never the key.
func TestCredentialCheckReportsTheEnvironmentSource(t *testing.T) {
	const secret = "lin_api_super_secret_value"
	deps := baseDeps(t)
	deps.Env["LINEAR_API_KEY"] = secret

	checks := run(t, deps)
	expect(t, find(t, checks, "Linear credential"), StatusOK, "available via LINEAR_API_KEY", "")
	if strings.Contains(report(checks), secret) {
		t.Fatalf("the report must never contain the key:\n%s", report(checks))
	}
}

// A configured credentialCommand is the first source, and it is run through the
// platform shell.
func TestCredentialCheckReportsTheCommandSource(t *testing.T) {
	const secret = "lin_api_from_the_password_manager"
	deps := baseDeps(t)
	shell := &shellRunner{stdout: secret + "\n"}
	deps.Credential = shell.run
	writeConfig(t, deps, `{"credentialCommand":"op read op://private/linear/api-key"}`)

	checks := run(t, deps)
	expect(t, find(t, checks, "Linear credential"), StatusOK, "available via credentialCommand", "")
	if strings.Contains(report(checks), secret) {
		t.Fatalf("the report must never contain the key:\n%s", report(checks))
	}
	want := [][]string{{"sh", "-c", "op read op://private/linear/api-key"}}
	if !reflect.DeepEqual(shell.calls, want) {
		t.Errorf("shell calls = %v, want %v", shell.calls, want)
	}
}

// Both configured: the command wins, so doctor reports the source a run would
// really use rather than the one that happens to be easier to check.
func TestCredentialCheckPrefersTheCommandOverTheEnvironment(t *testing.T) {
	deps := baseDeps(t)
	deps.Env["LINEAR_API_KEY"] = "lin_api_from_the_environment"
	shell := &shellRunner{stdout: "lin_api_from_the_command\n"}
	deps.Credential = shell.run
	writeConfig(t, deps, `{"credentialCommand":"cat /dev/null"}`)

	expect(t, find(t, run(t, deps), "Linear credential"), StatusOK, "available via credentialCommand", "")
}

// No key at all is the normal state of a fresh machine. lw is installable before
// you have one, so this warns and never fails.
func TestCredentialCheckReportsASavedKeySource(t *testing.T) {
	deps := baseDeps(t)
	deps.Vault = foundVault{}
	expect(t, find(t, run(t, deps), "Linear credential"), StatusOK, "available via system keychain", "")
}

func TestCredentialCheckWarnsWhenNoKeyIsAvailable(t *testing.T) {
	deps := baseDeps(t)
	path := config.Path(deps.Env, deps.Platform)
	check := find(t, run(t, deps), "Linear credential")
	expect(t, check, StatusWarn, "No Linear API key.",
		"connect inside lw, set LINEAR_API_KEY, or add credentialCommand to "+path)
	if check.Mandatory {
		t.Error("a missing key must not fail the run; the check is advisory")
	}
}

// A helper failure echoes neither command text nor output: either can contain
// the secret when someone configured the helper poorly.
func TestCredentialCheckWarnsWhenTheCommandFails(t *testing.T) {
	const leak = "lin_api_leaked_through_stderr"
	deps := baseDeps(t)
	deps.Credential = func(context.Context, string, []string, []string) ([]byte, error) {
		return []byte(leak), errors.New("exit status 1: " + leak)
	}
	writeConfig(t, deps, `{"credentialCommand":"op read op://private/linear/api-key"}`)

	checks := run(t, deps)
	check := find(t, checks, "Linear credential")
	if check.Status != StatusWarn {
		t.Fatalf("status = %q, want warn (detail %q)", check.Status, check.Detail)
	}
	if strings.Contains(check.Detail, "op read") {
		t.Errorf("detail exposed command text: %q", check.Detail)
	}
	if check.NextAction == "" {
		t.Error("a warning must carry a next action")
	}
	if strings.Contains(report(checks), leak) {
		t.Fatalf("the report must never echo the helper's output:\n%s", report(checks))
	}
}

func TestCredentialCheckWarnsWhenTheCommandPrintsNothing(t *testing.T) {
	deps := baseDeps(t)
	deps.Credential = func(context.Context, string, []string, []string) ([]byte, error) {
		return []byte("\n"), nil
	}
	writeConfig(t, deps, `{"credentialCommand":"printf ''"}`)

	check := find(t, run(t, deps), "Linear credential")
	if check.Status != StatusWarn {
		t.Fatalf("status = %q, want warn (detail %q)", check.Status, check.Detail)
	}
	if check.Detail != "credentialCommand printed no key." {
		t.Errorf("detail = %q", check.Detail)
	}
}

// Nothing configured means nothing is executed: `lw doctor` on a machine with no
// credentialCommand must not spawn a shell to find that out.
func TestCredentialCheckRunsNothingWhenNoCommandIsConfigured(t *testing.T) {
	deps := baseDeps(t)
	shell := &shellRunner{stdout: "lin_api_never_read\n"}
	deps.Credential = shell.run
	deps.Env["LINEAR_API_KEY"] = "lin_api_from_the_environment"

	expect(t, find(t, run(t, deps), "Linear credential"), StatusOK, "available via LINEAR_API_KEY", "")
	if len(shell.calls) != 0 {
		t.Fatalf("the shell was run %d time(s): %v", len(shell.calls), shell.calls)
	}
}

// A config file we could not parse still leaves the environment to answer, so
// one broken file does not blank out the rest of the report.
func TestCredentialCheckFallsBackToTheEnvironmentOnAMalformedConfig(t *testing.T) {
	deps := baseDeps(t)
	deps.Env["LINEAR_API_KEY"] = "lin_api_from_the_environment"
	writeConfig(t, deps, `{"credentialCommand":`)

	expect(t, find(t, run(t, deps), "Linear credential"), StatusOK, "available via LINEAR_API_KEY", "")
}

// ------------------------------------------------------------------ the config

func TestConfigCheckReportsAReadableFile(t *testing.T) {
	deps := baseDeps(t)
	writeConfig(t, deps, `{"worktreeRoot":"~/w"}`)
	expect(t, find(t, run(t, deps), "config file readable"), StatusOK,
		config.Path(deps.Env, deps.Platform), "")
}

func TestConfigCheckReportsAMissingFile(t *testing.T) {
	deps := baseDeps(t)
	expect(t, find(t, run(t, deps), "config file readable"), StatusOK,
		config.Path(deps.Env, deps.Platform)+" (no configuration yet)", "")
}

// Section 9: a stray comma must never look like "nothing configured yet". The
// next action is the spec's literal, and it stays true now that lw stores
// nothing — the key is wherever the user put it, so no edit to this file, up to
// and including deleting it, can touch it.
func TestConfigCheckFailsOnMalformedJSON(t *testing.T) {
	deps := baseDeps(t)
	writeConfig(t, deps, `{"agent":"claude",}`)
	path := config.Path(deps.Env, deps.Platform)
	check := find(t, run(t, deps), "config file readable")
	expect(t, check, StatusFail, "the config file "+path+" is not valid JSON",
		"fix the JSON, or delete the file to start over; your stored API key is unaffected")
	if !check.Mandatory {
		t.Error("the config check is mandatory")
	}
	// doctor's advice and the error a real run prints have to be the same
	// sentence; two spellings of one remedy is how they drift apart.
	if check.NextAction != config.InvalidFileNextAction {
		t.Errorf("next action = %q, want config.InvalidFileNextAction", check.NextAction)
	}
}

func TestConfigCheckFailsWhenTheDocumentIsNotAnObject(t *testing.T) {
	deps := baseDeps(t)
	writeConfig(t, deps, `["agent"]`)
	path := config.Path(deps.Env, deps.Platform)
	expect(t, find(t, run(t, deps), "config file readable"), StatusFail,
		"the config file "+path+" is not valid JSON", configNextAction)
}

func TestConfigCheckFailsWhenTheFileCannotBeRead(t *testing.T) {
	requireNonRoot(t)
	deps := baseDeps(t)
	writeConfig(t, deps, `{"worktreeRoot":"~/w"}`)
	path := config.Path(deps.Env, deps.Platform)
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	expect(t, find(t, run(t, deps), "config file readable"), StatusFail,
		"the config file "+path+" cannot be read", configNextAction)
}

// ConfigPath overrides the derived location, so a caller that already knows the
// file does not re-derive it.
func TestConfigCheckHonoursAnExplicitPath(t *testing.T) {
	deps := baseDeps(t)
	path := filepath.Join(tempDir(t), "elsewhere.json")
	if err := os.WriteFile(path, []byte(`{"worktreeRoot":"~/w"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps.ConfigPath = path
	checks := run(t, deps)
	expect(t, find(t, checks, "config file readable"), StatusOK, path, "")
	// The same path is what the credential's next action points at, so a user is
	// told to edit the file doctor actually read.
	expect(t, find(t, checks, "Linear credential"), StatusWarn, "No Linear API key.",
		"connect inside lw, set LINEAR_API_KEY, or add credentialCommand to "+path)
}

// ------------------------------------------------------------- the worktree root

func TestWorktreeRootCheckReportsAWritableRoot(t *testing.T) {
	deps := baseDeps(t)
	root := filepath.Join(tempDir(t), "worktrees")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, deps, `{"worktreeRoot":"`+root+`"}`)
	expect(t, find(t, run(t, deps), "worktree root writable"), StatusOK, root, "")
}

// The probe writes for real, so it must leave the directory exactly as it found
// it.
func TestWorktreeRootCheckLeavesNothingBehind(t *testing.T) {
	deps := baseDeps(t)
	root := filepath.Join(tempDir(t), "worktrees")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, deps, `{"worktreeRoot":"`+root+`"}`)
	run(t, deps)

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("the probe left %v behind", names)
	}
}

func TestWorktreeRootCheckAcceptsARootThatDoesNotExistYet(t *testing.T) {
	deps := baseDeps(t)
	parent := tempDir(t)
	root := filepath.Join(parent, "a", "b", "worktrees")
	writeConfig(t, deps, `{"worktreeRoot":"`+root+`"}`)
	expect(t, find(t, run(t, deps), "worktree root writable"), StatusOK, root+" (will be created)", "")

	if _, err := os.Stat(filepath.Join(parent, "a")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the probe must not create the root: %v", err)
	}
}

// "~" is expanded against the injected HOME, so the check reports the path the
// run would actually use.
func TestWorktreeRootCheckExpandsTilde(t *testing.T) {
	deps := baseDeps(t)
	writeConfig(t, deps, `{"worktreeRoot":"~/checkouts"}`)
	want := filepath.Join(deps.Env["HOME"], "checkouts") + " (will be created)"
	expect(t, find(t, run(t, deps), "worktree root writable"), StatusOK, want, "")
}

func TestWorktreeRootCheckFailsWhenTheRootIsNotWritable(t *testing.T) {
	requireNonRoot(t)
	deps := baseDeps(t)
	root := filepath.Join(tempDir(t), "worktrees")
	if err := os.MkdirAll(root, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) })
	writeConfig(t, deps, `{"worktreeRoot":"`+root+`"}`)

	check := find(t, run(t, deps), "worktree root writable")
	expect(t, check, StatusFail, "the worktree root "+root+" is not writable",
		`fix its permissions, or set "worktreeRoot" in config.json to a writable path`)
	if !check.Mandatory {
		t.Error("the worktree root check is mandatory")
	}
}

func TestWorktreeRootCheckFailsWhenTheRootIsAFile(t *testing.T) {
	deps := baseDeps(t)
	root := filepath.Join(tempDir(t), "worktrees")
	if err := os.WriteFile(root, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, deps, `{"worktreeRoot":"`+root+`"}`)
	expect(t, find(t, run(t, deps), "worktree root writable"), StatusFail,
		"the worktree root "+root+" exists and is not a directory",
		`remove it, or set "worktreeRoot" in config.json to another path`)
}

// ------------------------------------------------------------------------- Run

func TestRunPrintsEveryCheckInOrder(t *testing.T) {
	deps := baseDeps(t)
	var out bytes.Buffer
	deps.Stdout = &out

	code := Run(context.Background(), deps)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\n%s", code, out.String())
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	checks := Checks(context.Background(), deps)
	if len(lines) != len(checks) {
		t.Fatalf("printed %d lines, want %d:\n%s", len(lines), len(checks), out.String())
	}
	for i, check := range checks {
		if lines[i] != FormatCheck(check) {
			t.Errorf("line %d = %q, want %q", i, lines[i], FormatCheck(check))
		}
	}
	if !strings.HasSuffix(out.String(), "\n") {
		t.Error("every line is terminated, including the last")
	}
}

func TestRunExitsOneWhenAMandatoryCheckFails(t *testing.T) {
	deps := baseDeps(t)
	deps.Run = gitRunner("", 0, errors.New("git not found"))
	var out bytes.Buffer
	deps.Stdout = &out

	if code := Run(context.Background(), deps); code != 1 {
		t.Fatalf("exit code = %d, want 1\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "FAIL  git: ") {
		t.Errorf("the failure must still be printed:\n%s", out.String())
	}
}

// Warnings are the normal state of a fresh machine: no key, no agent, not in a
// repository. None of that is an error.
// SPEC §11: "Exit 1 if any mandatory check fails." Asserted on the gate itself,
// because no advisory check the tool has can currently produce a FAIL — so a
// run-level test cannot tell the mandatory rule from "any failure at all".
func TestExitCodeOnlyMandatoryFailuresCount(t *testing.T) {
	cases := []struct {
		name   string
		checks []Check
		want   int
	}{
		{"nothing", nil, 0},
		{"an advisory failure", []Check{{Status: StatusFail}}, 0},
		{"a mandatory failure", []Check{{Status: StatusFail, Mandatory: true}}, 1},
		{"a mandatory warning", []Check{{Status: StatusWarn, Mandatory: true}}, 0},
		{"a mandatory pass beside an advisory failure", []Check{
			{Status: StatusOK, Mandatory: true},
			{Status: StatusFail},
		}, 0},
		{"one mandatory failure among many", []Check{
			{Status: StatusOK, Mandatory: true},
			{Status: StatusWarn},
			{Status: StatusFail, Mandatory: true},
		}, 1},
	}
	for _, tc := range cases {
		if got := ExitCode(tc.checks); got != tc.want {
			t.Errorf("%s: ExitCode = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestRunExitsZeroWithOnlyWarnings(t *testing.T) {
	deps := baseDeps(t)
	var out bytes.Buffer
	deps.Stdout = &out

	if code := Run(context.Background(), deps); code != 0 {
		t.Fatalf("exit code = %d, want 0\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "warn  Linear credential: ") {
		t.Fatalf("a machine with no key still exits 0:\n%s", out.String())
	}
	if strings.Contains(out.String(), "FAIL") {
		t.Errorf("nothing mandatory should have failed:\n%s", out.String())
	}
}

func TestRunReportsEveryMandatoryFailure(t *testing.T) {
	deps := baseDeps(t)
	deps.Platform = "plan9"
	deps.Run = gitRunner("", 0, errors.New("git not found"))
	writeConfig(t, deps, `{`)
	var out bytes.Buffer
	deps.Stdout = &out

	if code := Run(context.Background(), deps); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	reported := out.String()
	for _, label := range []string{"platform", "git", "config file readable"} {
		if !strings.Contains(reported, "FAIL  "+label+": ") {
			t.Errorf("%q is missing from:\n%s", label, reported)
		}
	}
}

func TestChecksAreIdenticalAcrossRuns(t *testing.T) {
	deps := baseDeps(t)
	first := Checks(context.Background(), deps)
	second := Checks(context.Background(), deps)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("the report is not reproducible:\n%v\n%v", first, second)
	}
}

// ------------------------------------------------------------------- git fixtures

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

func requireNonRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
}

// runGit never reads the developer's own git configuration, so a test cannot
// depend on the machine it runs on.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"HOME="+dir,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func initRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "init", "--quiet", dir)
}

func commitEmpty(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "--allow-empty", "-m", "init", "--quiet")
}
