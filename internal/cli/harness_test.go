package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/snaylaker/lw/internal/credential"
	"github.com/snaylaker/lw/internal/domain"
	"github.com/snaylaker/lw/internal/gitrepo"
	"github.com/snaylaker/lw/internal/tui"
	issueprovider "github.com/snaylaker/lw/provider"
)

// The clock every test runs on, so a recents timestamp is a value and not a race.
var testNow = time.Unix(1785600000, 0).UTC()

// --- real git ----------------------------------------------------------------

// isolateGit keeps the suite off the machine's git configuration: a global
// commit.gpgsign or a template hook would otherwise decide whether it passes.
func isolateGit(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	t.Setenv("GIT_AUTHOR_NAME", "lw test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@lw.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "lw test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@lw.invalid")
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, output)
	}
	return strings.TrimSpace(string(output))
}

// newRepo is a real repository with one commit. /var is a symlink to
// /private/var on darwin, so the path is resolved the way git will report it.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := realPath(t, t.TempDir())
	git(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("source\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "README.md")
	git(t, dir, "commit", "-q", "-m", "initial")
	return dir
}

func realPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolving %s: %v", path, err)
	}
	return resolved
}

// --- the harness -------------------------------------------------------------

// harness is one run's world: a real repository, a config directory nobody else
// shares, and a recording stand-in for every seam that would otherwise reach
// the network, the terminal, or the user's own machine.
type harness struct {
	t *testing.T

	repo string
	// dir is the working directory the run is given; empty means the repository.
	dir          string
	configDir    string
	worktreeRoot string
	binDir       string
	env          map[string]string

	stdout bytes.Buffer
	stderr bytes.Buffer

	// launch stands in for the full-screen UI.
	launch func(deps tui.LauncherDeps) (tui.LauncherOutcome, error)
	// launched is what the run handed the launcher.
	launched *tui.LauncherDeps

	http       *fakeDoer
	credential *fakeCredentialRunner
	vault      *fakeVault
	gitRun     gitrepo.Runner
	child      ChildRunner
	providers  []issueprovider.Provider
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	isolateGit(t)

	home := realPath(t, t.TempDir())
	h := &harness{
		t:            t,
		repo:         newRepo(t),
		configDir:    filepath.Join(home, "config"),
		worktreeRoot: filepath.Join(home, "worktrees"),
		// An empty bin directory: no agent is ever "detected" by accident, so a
		// test says which agent it means or means none at all.
		binDir:     filepath.Join(home, "bin"),
		http:       &fakeDoer{},
		credential: &fakeCredentialRunner{},
		vault:      &fakeVault{},
	}
	if err := os.MkdirAll(h.binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	h.env = map[string]string{
		"HOME":          home,
		"PATH":          h.binDir,
		"LW_CONFIG_DIR": h.configDir,
	}
	return h
}

// configPath is where this harness's config.json lives.
func (h *harness) configPath() string { return filepath.Join(h.configDir, "config.json") }

// writeConfig writes config.json, filling in the worktree root so no test
// creates a checkout outside its own temporary directory.
func (h *harness) writeConfig(stored map[string]any) {
	h.t.Helper()
	if _, ok := stored["worktreeRoot"]; !ok {
		stored["worktreeRoot"] = h.worktreeRoot
	}
	payload, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		h.t.Fatal(err)
	}
	if err := os.MkdirAll(h.configDir, 0o700); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(h.configPath(), append(payload, '\n'), 0o600); err != nil {
		h.t.Fatal(err)
	}
}

// withKey puts the API key in the environment, which is one of the two sources
// SPEC §6 allows.
func (h *harness) withKey(key string) *harness {
	h.env["LINEAR_API_KEY"] = key
	return h
}

// deps assembles the injected dependencies. Nothing here can reach the network,
// a real credential helper, the terminal, or a repository of the user's own.
func (h *harness) deps() Deps {
	dir := h.dir
	if dir == "" {
		dir = h.repo
	}
	return Deps{
		Stdout:     &h.stdout,
		Stderr:     &h.stderr,
		Env:        h.env,
		Dir:        dir,
		Run:        h.gitRun,
		HTTPClient: h.http,
		Credential: h.credential.run,
		Vault:      h.vault,
		Now:        func() time.Time { return testNow },
		Launch:     h.launchLauncher,
		Child:      h.child,
		Providers:  h.providers,
	}
}

func (h *harness) run(argv ...string) int {
	h.t.Helper()
	return Run(argv, h.deps())
}

func (h *harness) launchLauncher(deps tui.LauncherDeps) (tui.LauncherOutcome, error) {
	h.launched = &deps
	if h.launch == nil {
		h.t.Fatal("the run opened the launcher, but the test did not expect it to")
	}
	return h.launch(deps)
}

// picks makes the launcher choose this issue and open its worktree, which is
// exactly what the real one does before it releases the terminal.
func (h *harness) picks(issue domain.Issue) {
	h.launch = func(deps tui.LauncherDeps) (tui.LauncherOutcome, error) {
		result, err := deps.ExecuteFlow(context.Background(), h.repoStructOrDeps(deps), issue, nil)
		if err != nil {
			return tui.LauncherOutcome{}, err
		}
		return tui.LauncherOutcome{Result: &result}, nil
	}
}

// worktreeFor is where this harness's worktree for an identifier lands.
func (h *harness) worktreeFor(identifier string) string {
	return filepath.Join(h.worktreeRoot, filepath.Base(h.repo), identifier)
}

func testIssue(identifier string) domain.Issue {
	return domain.Issue{
		ID:         "issue-" + identifier,
		Identifier: identifier,
		Title:      "Improve command completion output",
		URL:        "https://linear.app/acme/issue/" + identifier,
		StateType:  "started",
		StateName:  "In Progress",
		TeamKey:    strings.SplitN(identifier, "-", 2)[0],
	}
}

// --- the recording stand-ins -------------------------------------------------

// fakeDoer answers GraphQL requests from a canned body and counts them, so a
// test can say "no request was made" as precisely as "one was".
type fakeDoer struct {
	mu       sync.Mutex
	calls    int
	bodies   []string
	response string
	status   int
	err      error
}

func (d *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
	}
	d.mu.Lock()
	d.calls++
	d.bodies = append(d.bodies, string(body))
	response, status, err := d.response, d.status, d.err
	d.mu.Unlock()

	if err != nil {
		return nil, err
	}
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(response)),
		Header:     http.Header{},
	}, nil
}

func (d *fakeDoer) requests() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

type fakeVault struct {
	key                string
	source             credential.Source
	loadErr            error
	saveErr            error
	keyringUnavailable bool
	saveCalls          []credential.Store
	deleteErr          error
	deleted            bool
}

func (v *fakeVault) Load() (string, credential.Source, error) {
	if v.loadErr != nil {
		return "", "", v.loadErr
	}
	if v.key == "" {
		return "", "", credential.ErrNotFound
	}
	source := v.source
	if source == "" {
		source = credential.SourceKeyring
	}
	return v.key, source, nil
}

func (v *fakeVault) Save(key string, target credential.Store) (credential.Location, error) {
	v.saveCalls = append(v.saveCalls, target)
	if v.saveErr != nil {
		return credential.Location{}, v.saveErr
	}
	if v.keyringUnavailable && target == credential.StoreKeyring {
		return credential.Location{}, credential.ErrKeyringUnavailable
	}
	v.key = key
	if target == credential.StoreFile {
		v.source = credential.SourceFile
		return credential.FileLocation("/test/credentials"), nil
	}
	v.source = credential.SourceKeyring
	return credential.KeyringLocation(), nil
}

func (v *fakeVault) Delete() error {
	if v.deleteErr != nil {
		return v.deleteErr
	}
	v.key = ""
	v.deleted = true
	return nil
}

// fakeCredentialRunner stands in for the platform shell running
// credentialCommand: no test ever runs a real credential helper.
type fakeCredentialRunner struct {
	calls  int
	args   [][]string
	stdout string
	err    error
}

func (r *fakeCredentialRunner) run(ctx context.Context, shell string, args, _ []string) ([]byte, error) {
	r.calls++
	r.args = append(r.args, args)
	return []byte(r.stdout), r.err
}

// repoStruct is this harness's repository as the flow resolves it.
func (h *harness) repoStruct(t *testing.T) domain.Repo {
	t.Helper()
	repo, err := gitrepo.Resolve(context.Background(), h.repo, nil)
	if err != nil {
		t.Fatalf("resolving the harness repository: %v", err)
	}
	return repo
}

// execEnv is the filled-in dependency set a command body receives, so a test can
// drive one directly instead of through Run.
func (h *harness) execEnv(t *testing.T) *execEnv {
	t.Helper()
	env, err := newExecEnv(h.deps())
	if err != nil {
		t.Fatalf("newExecEnv: %v", err)
	}
	return env
}

// write creates a file and the directories above it.
func write(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

// repoStructOrDeps is the repository a faked launcher should hand back to the
// flow: --repo when it was given, else the one the harness is standing in.
func (h *harness) repoStructOrDeps(deps tui.LauncherDeps) domain.Repo {
	if deps.PreselectRepo != nil {
		return *deps.PreselectRepo
	}
	return deps.Repo
}

// worktreeForRepo is where a worktree cut from another repository lands.
func (h *harness) worktreeForRepo(repoRoot, identifier string) string {
	return filepath.Join(h.worktreeRoot, filepath.Base(repoRoot), identifier)
}
