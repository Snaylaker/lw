package cli

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/snaylaker/lw/internal/config"
	"github.com/snaylaker/lw/internal/credential"
	"github.com/snaylaker/lw/internal/domain"
	"github.com/snaylaker/lw/internal/gitrepo"
	"github.com/snaylaker/lw/internal/linear"
	"github.com/snaylaker/lw/internal/tui"
	"github.com/snaylaker/lw/internal/worktree"
)

// runFlow is `lw`: SPEC §3, in order. The repository first, so a bad invocation
// fails before any network; then the credential; then the pickers; then the
// worktree. The full-screen UI ends there, and the path is printed after it.
//
// Everything from the worktree on obeys SPEC §3's failure rule: the checkout
// exists, so the exit code stays 0. Only a cancellation changes it, to 130.
func runFlow(ctx context.Context, opts Options, env *execEnv) int {
	run, err := newFlow(ctx, opts, env)
	if err != nil {
		return Report(err, env.stderr)
	}
	result, code := run.pick(ctx)
	if result == nil {
		return code
	}
	return run.finish(*result)
}

// flow is one run's resolved world: everything steps 3–5 need, decided before
// the terminal is taken over.
type flow struct {
	opts Options
	env  *execEnv

	// repo is the checkout the user is standing in, if any. It is a default and
	// a picker row, not the answer: the run settles the repository on the repo
	// screen (SPEC §4).
	repo     *domain.Repo
	flagRepo *domain.Repo

	stored              *config.StoredConfig
	configPath          string
	credential          domain.Credential
	needsCredential     bool
	validatedCredential *[sha256.Size]byte
}

// newFlow performs steps 1 and 2: the repository, then the credential.
func newFlow(ctx context.Context, opts Options, env *execEnv) (*flow, error) {
	// 1. The repository. --repo is validated immediately, before anything can
	//    touch the network, so a bad path fails instantly. Without it the
	//    current directory is only a default: not standing in a repository is
	//    no longer an error, because the run asks which one to use (SPEC §4).
	var flagRepo, hereRepo *domain.Repo
	if opts.Repo != "" {
		named, err := gitrepo.Source(ctx, gitrepo.SourceOptions{
			Flag: config.ExpandTilde(opts.Repo, env.env),
			Dir:  env.dir,
			Run:  env.run,
		})
		if err != nil {
			return nil, err
		}
		flagRepo = &named
	} else {
		validation := gitrepo.Validate(ctx, env.dir, env.run)
		switch validation.Status {
		case gitrepo.StatusOK:
			here := validation.Repo
			hereRepo = &here
		case gitrepo.StatusGitMissing, gitrepo.StatusUnbornHead:
			return nil, gitrepo.ValidationError(validation)
		}
	}

	configPath := env.configPath()
	// Read once: every later question — the credential command, the worktree
	// root, the pins — is answered from this one value, so
	// a run cannot contradict itself about what is configured.
	stored, err := config.ReadStoredConfig(configPath)
	if err != nil {
		return nil, err
	}

	// 2. Existing credentials are resolved here. A genuinely missing key is not
	//    an error for an interactive run: the launcher completes onboarding.
	resolved, err := credential.Resolve(ctx, credential.Options{
		Env:        env.env,
		Platform:   env.platform,
		Command:    storedCredentialCommand(stored),
		ConfigPath: configPath,
		Run:        env.credential,
		Vault:      env.vault,
	})
	needsCredential := false
	if err != nil {
		if !errors.Is(err, credential.ErrNotFound) || opts.Issue != "" {
			return nil, err
		}
		needsCredential = true
	}

	f := &flow{
		opts:            opts,
		env:             env,
		repo:            hereRepo,
		flagRepo:        flagRepo,
		stored:          stored,
		configPath:      configPath,
		credential:      resolved.Credential,
		needsCredential: needsCredential,
	}
	if !needsCredential {
		f.activateCredential(resolved.Credential)
	}
	return f, nil
}

// pick completes missing onboarding, chooses an issue, opens its worktree, and releases
// the terminal. A nil result means the run is over and the second value is the
// exit code.
func (f *flow) pick(ctx context.Context) (*domain.FlowResult, int) {
	// --issue names the issue outright, so no picker — and no full-screen UI —
	// is needed. An explicit/current repository still wins; outside one, the
	// issue's remembered project or team association can now resolve it.
	if f.opts.Issue != "" {
		issue, err := linear.ResolveIssue(ctx, linear.ResolveIssueRequest{
			Credential: f.credential,
			Identifier: f.opts.Issue,
			HTTPClient: f.env.http,
		})
		if err != nil {
			return nil, Report(err, f.env.stderr)
		}
		repo := firstRepo(f.flagRepo, f.repo)
		if repo == nil {
			if remembered, ok := f.repoForIssue(ctx, issue); ok {
				repo = &remembered
			}
		}
		if repo == nil {
			return nil, Report(gitrepo.ValidationError(gitrepo.Validation{
				Status: gitrepo.StatusNotARepo, Dir: f.env.dir,
			}), f.env.stderr)
		}
		result, err := f.execute(ctx, *repo, issue, nil)
		if err != nil {
			return nil, Report(err, f.env.stderr)
		}
		return &result, 0
	}

	deps := tui.LauncherDeps{
		NeedsRepoRoot:     len(config.RepoRoots(f.stored, f.env.env)) == 0,
		SuggestedRepoRoot: f.suggestedRepoRoot(),
		ListRepos: func() []tui.RankedRepo {
			return f.listRepos(ctx)
		},
		SetRepoRoot: func(root string) ([]tui.RankedRepo, error) {
			return f.setRepoRoot(ctx, root)
		},
		SearchIssues:      f.searchIssues,
		ListProjects:      f.listProjects,
		ListProjectIssues: f.listProjectIssues,
		ProjectPins:       config.PinnedProjects(f.stored),
		ToggleProjectPin:  f.toggleProjectPin,
		ListTeams:         f.listTeams,
		ListTeamIssues:    f.listTeamIssues,
		TeamPins:          config.PinnedTeams(f.stored),
		ToggleTeamPin:     f.toggleTeamPin,
		ExecuteFlow:       f.execute,
		PreselectRepo:     f.flagRepo,
		RepoForIssue: func(issue domain.Issue) (domain.Repo, bool) {
			return f.repoForIssue(ctx, issue)
		},
		RecordRepoUse: f.recordRepoUse,
	}
	if f.needsCredential {
		deps.Credential = &tui.CredentialSetup{
			File: credential.FallbackPath(f.configPath),
			Save: f.setCredential,
		}
	}
	if f.repo != nil {
		deps.Repo = *f.repo
	}
	outcome, err := f.env.launch(deps)
	if err != nil {
		return nil, Report(err, f.env.stderr)
	}
	switch {
	case outcome.Cancelled:
		// A cancellation prints nothing. The worktree, if one was created
		// before the abort landed, stays exactly where it is.
		return nil, 130
	case outcome.Result == nil:
		// The error view already showed the message and its next action.
		return nil, 1
	}
	return outcome.Result, 0
}

// execute is step 4, and the only thing the launcher does that touches disk:
// create or reuse the worktree. worktree.Open writes the metadata itself.
func (f *flow) execute(ctx context.Context, repo domain.Repo, issue domain.Issue, onStage func(domain.StageUpdate)) (domain.FlowResult, error) {
	// The source repository is only known after the picker. Automatic cleanup
	// must operate on that choice, never merely on the directory lw started in.
	pruneMergedIfConfigured(ctx, repo, f.env)

	result, err := worktree.Open(ctx, worktree.Options{
		Repo:    repo,
		Issue:   issue,
		Root:    config.ResolveWorktreeRoot(f.stored, f.env.env),
		Run:     f.env.run,
		OnStage: onStage,
	})
	if err != nil {
		return domain.FlowResult{}, err
	}
	return domain.FlowResult{CheckoutPath: result.Path, Created: result.Created}, nil
}

// firstRepo is the first of the candidates that is set.
func firstRepo(candidates ...*domain.Repo) *domain.Repo {
	for _, candidate := range candidates {
		if candidate != nil {
			return candidate
		}
	}
	return nil
}

func (f *flow) setCredential(ctx context.Context, key string, target credential.Store) (credential.Location, error) {
	candidate := domain.Credential{Key: strings.TrimSpace(key)}
	fingerprint := sha256.Sum256([]byte("validated-linear-credential\x00" + candidate.Key))
	if f.validatedCredential == nil || *f.validatedCredential != fingerprint {
		f.validatedCredential = nil
		if err := linear.ValidateCredential(ctx, candidate, f.env.http); err != nil {
			return credential.Location{}, err
		}
		if err := ctx.Err(); err != nil {
			return credential.Location{}, linear.MapError(err)
		}
		// Remember only a one-way, process-local fingerprint. File consent can
		// reuse this validation without retaining a second copy of the API key,
		// while a different or direct file submission is validated normally.
		f.validatedCredential = &fingerprint
	}
	location, err := credential.Save(candidate.Key, target, credential.Options{
		Env:        f.env.env,
		Platform:   f.env.platform,
		ConfigPath: f.configPath,
		Vault:      f.env.vault,
	})
	if err != nil {
		return credential.Location{}, err
	}
	f.validatedCredential = nil
	f.activateCredential(candidate)
	f.needsCredential = false
	return location, nil
}

func (f *flow) activateCredential(value domain.Credential) {
	f.credential = value
}

func (f *flow) suggestedRepoRoot() string {
	if f.repo != nil {
		return filepath.Dir(f.repo.Root)
	}
	return f.env.dir
}

// listRepos is the repo picker's rows, all from disk: where the user is
// standing, then what they picked before, then everything under the configured
// roots. With no roots configured the parent of the current checkout is scanned,
// so siblings show up without anyone having to configure anything.
func (f *flow) listRepos(ctx context.Context) []tui.RankedRepo {
	roots := config.RepoRoots(f.stored, f.env.env)
	if len(roots) == 0 && f.repo != nil {
		roots = []string{filepath.Dir(f.repo.Root)}
	}

	var recent []domain.Repo
	for _, entry := range config.RecentRepos(f.stored) {
		path := config.ResolveConfiguredPath(entry.Path, f.env.env)
		if repo, err := gitrepo.Resolve(ctx, path, f.env.run); err == nil {
			recent = append(recent, repo)
		}
	}
	return tui.RankRepos(f.repo, recent, gitrepo.Discover(ctx, roots, f.env.run))
}

// setRepoRoot accepts either a directory containing repositories or a
// repository itself. The latter resolves to its parent, which is usually what
// someone means when they paste the path they already have in front of them.
func (f *flow) setRepoRoot(ctx context.Context, value string) ([]tui.RankedRepo, error) {
	root := config.ExpandTilde(strings.TrimSpace(value), f.env.env)
	if !filepath.IsAbs(root) {
		root = filepath.Join(f.env.dir, root)
	}
	root = filepath.Clean(root)
	if repo, err := gitrepo.Resolve(ctx, root, f.env.run); err == nil {
		root = filepath.Dir(repo.Root)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("cannot read repository root %s", root)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("repository root is not a directory: %s", root)
	}
	if repos := gitrepo.Discover(ctx, []string{root}, f.env.run); len(repos) == 0 {
		return nil, fmt.Errorf("no git repositories found directly inside %s", root)
	}
	if err := config.AddRepoRoot(root, f.configPath); err != nil {
		return nil, err
	}
	f.stored, err = config.ReadStoredConfig(f.configPath)
	if err != nil {
		return nil, err
	}
	return f.listRepos(ctx), nil
}

func (f *flow) repoForIssue(ctx context.Context, issue domain.Issue) (domain.Repo, bool) {
	path := ""
	if issue.ProjectID != "" {
		path = config.ProjectRepoPath(f.stored, issue.ProjectID)
	} else {
		path = config.TeamRepoPath(f.stored, issue.TeamID)
	}
	if path == "" {
		return domain.Repo{}, false
	}
	repo, err := gitrepo.Resolve(ctx, config.ResolveConfiguredPath(path, f.env.env), f.env.run)
	return repo, err == nil
}

// recordRepoUse remembers the recent repository and the issue's most specific
// durable scope: project when present, otherwise team.
func (f *flow) recordRepoUse(issue domain.Issue, repo domain.Repo) {
	if _, err := config.RecordRepoUse(config.RepoUse{
		ProjectID: issue.ProjectID,
		TeamID:    issue.TeamID,
		Path:      repo.Root,
	}, f.configPath, f.env.nowMillis()); err != nil {
		return
	}
	f.reloadStoredConfig()
}

func (f *flow) searchIssues(ctx context.Context, query string) ([]domain.Issue, error) {
	return linear.FindIssues(ctx, linear.FindIssuesRequest{
		Credential: f.credential,
		Query:      query,
		HTTPClient: f.env.http,
	})
}

func (f *flow) listProjects(ctx context.Context) ([]domain.Project, error) {
	return linear.ListProjects(ctx, linear.ListProjectsRequest{
		Credential: f.credential,
		HTTPClient: f.env.http,
	})
}

func (f *flow) listProjectIssues(ctx context.Context, project domain.Project) ([]domain.Issue, error) {
	return linear.ListProjectIssues(ctx, linear.ListProjectIssuesRequest{
		Credential: f.credential,
		ProjectID:  project.ID,
		HTTPClient: f.env.http,
	})
}

func (f *flow) listTeams(ctx context.Context) ([]domain.Team, error) {
	return linear.ListTeams(ctx, linear.ListTeamsRequest{
		Credential: f.credential,
		HTTPClient: f.env.http,
	})
}

func (f *flow) listTeamIssues(ctx context.Context, team domain.Team) ([]domain.Issue, error) {
	return linear.ListTeamIssues(ctx, linear.ListTeamIssuesRequest{
		Credential: f.credential,
		TeamKey:    team.Key,
		HTTPClient: f.env.http,
	})
}

func (f *flow) toggleProjectPin(project domain.Project) ([]string, error) {
	result, err := config.ToggleProjectPin(project.ID, f.configPath)
	if err != nil {
		return nil, err
	}
	f.reloadStoredConfig()
	return result.IDs, nil
}

func (f *flow) toggleTeamPin(team domain.Team) ([]string, error) {
	result, err := config.ToggleTeamPin(team.ID, f.configPath)
	if err != nil {
		return nil, err
	}
	f.reloadStoredConfig()
	return result.IDs, nil
}

func (f *flow) reloadStoredConfig() {
	if stored, err := config.ReadStoredConfig(f.configPath); err == nil {
		f.stored = stored
	}
}

// finish is the end of the run: put the worktree path on stdout and stop.
//
// That is the whole output contract. Everything a run says about its progress
// goes to stderr, so stdout holds exactly one line and `cd $(lw)` works. What
// happens in the worktree next is the caller's business, not this tool's.
func (f *flow) finish(result domain.FlowResult) int {
	fmt.Fprintln(f.env.stdout, result.CheckoutPath)
	return 0
}

// --- the configuration -------------------------------------------------------

// A nil StoredConfig is "nothing configured yet", which every reader below has
// to answer for itself.

func storedCredentialCommand(stored *config.StoredConfig) string {
	if stored == nil {
		return ""
	}
	return stored.CredentialCommand
}
