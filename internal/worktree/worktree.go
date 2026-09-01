// Package worktree creates the checkout a run opens, reuses the one an earlier
// run left behind, and writes the metadata each worktree carries.
package worktree

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/snaylaker/lw/internal/domain"
	"github.com/snaylaker/lw/internal/gitrepo"
	"github.com/snaylaker/lw/internal/lwerr"
)

// Options are the inputs of Open.
type Options struct {
	// Repo is the source checkout: the worktree is added to it, and its name
	// groups every worktree cut from it.
	Repo domain.Repo
	// Issue names the directory and the metadata the worktree carries.
	Issue domain.Issue
	// Branch is resolved before Open. Empty keeps compatibility for callers that
	// still want the old identifier-named branch.
	Branch domain.Branch
	// Root is the absolute directory holding one subdirectory per repository.
	Root string
	// Run is nil for gitrepo.DefaultRunner.
	Run gitrepo.Runner
	// OnStage may be nil; every update it receives is a StageCreatingWorktree or
	// StagePreparing one.
	OnStage func(domain.StageUpdate)
}

// Result is a worktree that exists on disk. Created is false for one that was
// already there.
type Result struct {
	Path    string
	Branch  string
	Created bool
}

// The path-conflict failure of SPEC §5, quoted there literally.
const ConflictNextAction = "remove it, or set worktreeRoot to another location"

// ConflictMessage is SPEC §5's `<path> already exists and is not a worktree of
// <repo>`.
func ConflictMessage(path, repoName string) string {
	return path + " already exists and is not a worktree of " + repoName
}

// cancelledFirst replaces a failure with the cancellation that caused it. A
// Ctrl+C tears the context down while git is running, so git exits non-zero and
// the wrapper would otherwise report an internal error — SPEC §10 wants silence
// and exit 130 instead.
func cancelledFirst(ctx context.Context, err error) error {
	if err != nil && ctx.Err() != nil {
		return lwerr.NewCancelled()
	}
	return err
}

// Path is where the worktree for an issue lives: one directory per repository,
// one directory per issue inside it.
func Path(root, repoName, identifier string) string {
	return filepath.Join(root, repoName, identifier)
}

// Open guarantees a checkout of the issue's branch and returns it. The checkout
// existing is the product's promise, so everything that can be repaired is
// repaired rather than reported: a registration whose directory is gone is
// pruned and recreated, and a worktree already holding the branch is reused
// wherever it sits.
func Open(ctx context.Context, opts Options) (Result, error) {
	run := opts.Run
	if run == nil {
		run = gitrepo.DefaultRunner
	}
	stage := opts.OnStage
	if stage == nil {
		stage = func(domain.StageUpdate) {}
	}

	identifier := opts.Issue.Identifier
	branch := opts.Branch
	if err := validIdentifier(identifier); err != nil {
		return Result{}, err
	}
	if branch.Name == "" {
		return Result{}, lwerr.New(lwerr.Internal,
			"the worktree branch was not resolved",
			"report this: it is a bug in lw",
		)
	}
	path := Path(opts.Root, opts.Repo.Name, identifier)

	stage(domain.StageUpdate{Stage: domain.StagePreparing, State: domain.StateActive})
	registered, err := list(ctx, opts.Repo.Root, run)
	if err != nil {
		stage(domain.StageUpdate{Stage: domain.StagePreparing, State: domain.StateFailed})
		return Result{}, cancelledFirst(ctx, err)
	}
	if existing := match(registered, opts.Repo.Root, path, branch.Name); existing != nil {
		if !stale(*existing) {
			if !samePath(existing.Path, path) {
				metadata, metadataErr := ReadMetadata(ctx, existing.Path, run)
				if metadataErr != nil || metadata == nil || metadata.Identifier != identifier {
					stage(domain.StageUpdate{Stage: domain.StagePreparing, State: domain.StateFailed})
					return Result{}, branchCheckoutConflict(branch.Name, existing.Path)
				}
			}
			// The same directory keeps the spelling this run computed, so the
			// reported path does not change shape between creating and reusing.
			reused := existing.Path
			if samePath(reused, path) {
				reused = path
			}
			stage(domain.StageUpdate{Stage: domain.StagePreparing, State: domain.StateDone})
			stage(domain.StageUpdate{Stage: domain.StageCreatingWorktree, State: domain.StateSkipped, Detail: "already exists"})
			if err := WriteMetadata(ctx, reused, MetadataOf(opts.Issue, existing.Branch), run); err != nil {
				return Result{}, cancelledFirst(ctx, err)
			}
			return Result{Path: reused, Branch: existing.Branch, Created: false}, nil
		}
		// A registration git can no longer follow blocks both the path and the
		// branch, and it is bookkeeping rather than user data: repair it.
		prune(ctx, opts.Repo.Root, run)
	}
	if checkout := branchCheckout(registered, branch.Name); checkout != nil {
		stage(domain.StageUpdate{Stage: domain.StagePreparing, State: domain.StateFailed})
		return Result{}, branchCheckoutConflict(branch.Name, checkout.Path)
	}

	if occupied(path) {
		stage(domain.StageUpdate{Stage: domain.StagePreparing, State: domain.StateFailed})
		return Result{}, lwerr.New(lwerr.WorktreeConflict,
			ConflictMessage(path, opts.Repo.Name),
			ConflictNextAction,
		)
	}
	stage(domain.StageUpdate{Stage: domain.StagePreparing, State: domain.StateDone})

	stage(domain.StageUpdate{Stage: domain.StageCreatingWorktree, State: domain.StateActive})
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		stage(domain.StageUpdate{Stage: domain.StageCreatingWorktree, State: domain.StateFailed})
		return Result{}, cancelledFirst(ctx, lwerr.Wrap(err, lwerr.Internal,
			"the worktree directory could not be created at "+filepath.Dir(path),
			`set "worktreeRoot" in config.json to a writable directory`,
		))
	}
	if err := add(ctx, opts.Repo.Root, path, branch, run); err != nil {
		stage(domain.StageUpdate{Stage: domain.StageCreatingWorktree, State: domain.StateFailed})
		return Result{}, cancelledFirst(ctx, err)
	}
	if err := WriteMetadata(ctx, path, MetadataOf(opts.Issue, branch.Name), run); err != nil {
		stage(domain.StageUpdate{Stage: domain.StageCreatingWorktree, State: domain.StateFailed})
		return Result{}, cancelledFirst(ctx, err)
	}
	stage(domain.StageUpdate{Stage: domain.StageCreatingWorktree, State: domain.StateDone})
	return Result{Path: path, Branch: branch.Name, Created: true}, nil
}

// add follows the branch plan produced by the resolver. Remote-only branches
// become tracking local branches; new branches start from the fetched remote
// default branch when one exists.
func add(ctx context.Context, repoRoot, path string, branch domain.Branch, run gitrepo.Runner) error {
	var args []string
	switch {
	case branch.ExistingLocal:
		args = []string{"worktree", "add", path, branch.Name}
	case branch.ExistingRemote != "":
		args = []string{"worktree", "add", "--track", "-b", branch.Name, path, branch.ExistingRemote}
	default:
		args = []string{"worktree", "add", "-b", branch.Name, path}
		if branch.Base != "" {
			args = append(args, branch.Base)
		}
	}
	result, err := run(ctx, repoRoot, "git", args)
	if err == nil && result.ExitCode == 0 {
		return nil
	}
	return lwerr.Wrap(err, lwerr.Internal,
		`git could not create the worktree at "`+path+`": `+detail(result, err),
		"fix what git reports, then run lw again",
	)
}

// branchExists reports whether a branch is present. ok is false when git could
// not be asked at all, which is a different answer from "no": a caller that
// deletes things on absence must not treat an unanswerable git as permission.
//
// dir may be any directory in the repository — refs/heads lives in the common
// directory, so a linked worktree answers for the whole repository.
func branchExists(ctx context.Context, dir, name string, run gitrepo.Runner) (exists, ok bool) {
	result, err := run(ctx, dir, "git", []string{"rev-parse", "--verify", "--quiet", "refs/heads/" + name})
	if err != nil {
		return false, false
	}
	return result.ExitCode == 0 && strings.TrimSpace(result.Stdout) != "", true
}

// prune is advisory: a repository that cannot be pruned fails loudly at the
// next step instead, with git's own words.
func prune(ctx context.Context, repoRoot string, run gitrepo.Runner) {
	_, _ = run(ctx, repoRoot, "git", []string{"worktree", "prune"})
}

// registration is one entry of `git worktree list --porcelain`.
type registration struct {
	Path     string
	Branch   string // short name, empty when detached
	Prunable bool
	Bare     bool
}

func list(ctx context.Context, repoRoot string, run gitrepo.Runner) ([]registration, error) {
	result, err := run(ctx, repoRoot, "git", []string{"worktree", "list", "--porcelain"})
	if err != nil || result.ExitCode != 0 {
		return nil, lwerr.Wrap(err, lwerr.Internal,
			`the worktrees of "`+repoRoot+`" could not be listed: `+detail(result, err),
			"check that the directory is a healthy git repository",
		)
	}
	return parseList(result.Stdout), nil
}

// parseList reads the record-per-worktree porcelain format: a "worktree" line
// opens a record and a blank line closes it. Attributes older git versions do
// not emit are simply absent, which is why staleness is also checked on disk.
func parseList(output string) []registration {
	registrations := []registration{}
	var current *registration
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			if current != nil {
				registrations = append(registrations, *current)
				current = nil
			}
			continue
		}
		key, value, _ := strings.Cut(line, " ")
		switch key {
		case "worktree":
			if current != nil {
				registrations = append(registrations, *current)
			}
			current = &registration{Path: value}
		case "branch":
			if current != nil {
				current.Branch = strings.TrimPrefix(value, "refs/heads/")
			}
		case "prunable":
			if current != nil {
				current.Prunable = true
			}
		case "bare":
			if current != nil {
				current.Bare = true
			}
		}
	}
	if current != nil {
		registrations = append(registrations, *current)
	}
	return registrations
}

// branchCheckoutConflict explains the collision before git's less focused
// worktree-add error does.
func branchCheckoutConflict(branch, path string) *lwerr.Error {
	return lwerr.New(lwerr.WorktreeConflict,
		`branch "`+branch+`" is already checked out at `+path,
		"choose another branch or remove that worktree")
}

func branchCheckout(registrations []registration, branch string) *registration {
	for i := range registrations {
		if !registrations[i].Bare && !stale(registrations[i]) && registrations[i].Branch == branch {
			return &registrations[i]
		}
	}
	return nil
}

// match prefers the stable issue path and then an alternate lw worktree holding
// the selected branch. The caller verifies that an alternate belongs to the
// same issue. The main checkout is never a reusable candidate.
func match(registrations []registration, repoRoot, path, branch string) *registration {
	candidates := make([]*registration, 0, len(registrations))
	for i := range registrations {
		if registrations[i].Bare || samePath(registrations[i].Path, repoRoot) {
			continue
		}
		candidates = append(candidates, &registrations[i])
	}
	for _, candidate := range candidates {
		if samePath(candidate.Path, path) {
			return candidate
		}
	}
	for _, candidate := range candidates {
		if candidate.Branch == branch {
			return candidate
		}
	}
	return nil
}

// stale covers both the flag newer git versions report and the case it was
// added for, so the repair works on a git too old to say "prunable".
func stale(entry registration) bool {
	if entry.Prunable {
		return true
	}
	info, err := os.Stat(entry.Path)
	return err != nil || !info.IsDir()
}

// occupied reports a path holding something that is not ours: git would fail on
// it, and a directory the user filled is not ours to delete. An empty directory
// is not occupied — `git worktree add` accepts one.
func occupied(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	if !info.IsDir() {
		return true
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return true
	}
	return len(entries) > 0
}

// samePath compares what git recorded with what this run computed. The two can
// differ by a symlink — a macOS /tmp, a home directory behind a link — while
// naming the same worktree.
func samePath(a, b string) bool {
	if a == b {
		return true
	}
	cleanA, cleanB := filepath.Clean(a), filepath.Clean(b)
	if cleanA == cleanB {
		return true
	}
	realA, errA := filepath.EvalSymlinks(cleanA)
	realB, errB := filepath.EvalSymlinks(cleanB)
	return errA == nil && errB == nil && realA == realB
}

// validIdentifier guards the one value that becomes a path segment and a branch
// name. Provider adapters must supply a deterministic path-safe key; anything
// else is a bug upstream, not user input to explain.
func validIdentifier(identifier string) error {
	if identifier != "" && identifier != "." && identifier != ".." &&
		!strings.ContainsAny(identifier, `/\`) {
		return nil
	}
	return lwerr.New(lwerr.Internal,
		`"`+identifier+`" cannot name a worktree`,
		"report this: it is a bug in lw",
	)
}

// detail is git's own first line of complaint, which is the only part of a git
// failure worth putting in front of a user.
func detail(result gitrepo.ExecResult, err error) string {
	for _, stream := range []string{result.Stderr, result.Stdout} {
		for _, line := range strings.Split(stream, "\n") {
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				return trimmed
			}
		}
	}
	if err != nil {
		return err.Error()
	}
	return "git exited without a message"
}
