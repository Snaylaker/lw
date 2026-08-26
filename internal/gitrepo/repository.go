package gitrepo

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/snaylaker/lw/internal/domain"
	"github.com/snaylaker/lw/internal/lwerr"
)

// SourceOptions decides which repository a run operates on. There is no
// discovery and no picker: like git and gh, the tool uses the repository the
// user is standing in unless one is named explicitly.
type SourceOptions struct {
	// Flag is the --repo value, empty when the flag was not given. A relative
	// path is interpreted against Dir, and a "~" must already be expanded by the
	// caller — this package never reads the environment.
	Flag string
	// Dir is the working directory; empty means ask the operating system.
	Dir string
	Run Runner
}

// Source is the one way a run learns its repository: the --repo value when
// given, otherwise the current directory, resolved through the same validation
// (and the same linked-worktree rule) either way.
func Source(ctx context.Context, opts SourceOptions) (domain.Repo, error) {
	dir := opts.Dir
	if dir == "" {
		working, err := os.Getwd()
		if err != nil {
			return domain.Repo{}, lwerr.Wrap(err, lwerr.NotARepo,
				"the current directory cannot be determined",
				NotARepoNextAction,
			)
		}
		dir = working
	}
	if opts.Flag != "" {
		// Resolved against the working directory rather than handed to git as
		// typed, so the failure message names a path the user can act on.
		if filepath.IsAbs(opts.Flag) {
			dir = filepath.Clean(opts.Flag)
		} else {
			dir = filepath.Join(dir, opts.Flag)
		}
	}
	return Resolve(ctx, dir, opts.Run)
}

// Status classifies one directory. StatusUnbornHead is a repository we
// resolved but cannot branch from, which git only reports much later as
// "fatal: invalid reference: HEAD".
type Status string

const (
	StatusOK         Status = "ok"
	StatusNotARepo   Status = "not_a_repo"
	StatusUnbornHead Status = "unborn_head"
	// StatusGitMissing is git itself being absent, which is not the same
	// failure as standing outside a repository: telling someone to cd into one
	// is a dead end when nothing can run git anywhere.
	StatusGitMissing Status = "git_missing"
)

// Validation is the outcome of inspecting one directory. Repo is set for
// StatusOK and StatusUnbornHead; Dir and the optional Cause for StatusNotARepo.
type Validation struct {
	Status Status
	Repo   domain.Repo
	Dir    string
	Cause  error
}

// Validate never fails for a bad directory — callers decide between skipping
// and reporting. A linked worktree resolves to its main checkout, so the
// launcher can run from inside a worktree it created: the main repo stays the
// source and re-picking the same issue reuses the existing worktree.
//
// Both git invocations run in dir, not in the resolved root.
func Validate(ctx context.Context, dir string, run Runner) Validation {
	if run == nil {
		run = DefaultRunner
	}

	toplevel, err := run(ctx, dir, "git", []string{"rev-parse", "--show-toplevel"})
	if err != nil {
		if IsGitMissing(err) {
			return Validation{Status: StatusGitMissing, Dir: dir, Cause: err}
		}
		return Validation{Status: StatusNotARepo, Dir: dir, Cause: err}
	}
	if toplevel.ExitCode != 0 {
		return Validation{Status: StatusNotARepo, Dir: dir}
	}
	root := strings.TrimSpace(toplevel.Stdout)
	if root == "" {
		return Validation{Status: StatusNotARepo, Dir: dir}
	}
	if main, ok := mainCheckoutRoot(ctx, dir, run); ok {
		root = main
	}
	repo := domain.Repo{Root: root, Name: filepath.Base(root)}

	head, err := run(ctx, dir, "git", []string{"rev-parse", "--verify", "--quiet", "HEAD"})
	// Inside a resolved repository the only reason HEAD does not verify is that
	// no commit exists yet.
	if err != nil || head.ExitCode != 0 || strings.TrimSpace(head.Stdout) == "" {
		return Validation{Status: StatusUnbornHead, Repo: repo}
	}
	return Validation{Status: StatusOK, Repo: repo}
}

// Resolve validates one directory, raising the matching actionable error.
func Resolve(ctx context.Context, dir string, run Runner) (domain.Repo, error) {
	validation := Validate(ctx, dir, run)
	if validation.Status == StatusOK {
		return validation.Repo, nil
	}
	return domain.Repo{}, ValidationError(validation)
}

// The two failures of SPEC §4, quoted there literally: the unborn message names
// the repository, the not-a-repo message names no directory at all. Both carry
// kind not_a_repo.
const (
	NotARepoNextAction   = "run lw from inside a repository, or pass --repo <path>"
	NotARepoMessage      = "not inside a git repository"
	UnbornHeadNextAction = "make an initial commit, then re-run"
	// The same words `lw doctor` uses for its mandatory git check, so the two
	// commands agree about what is wrong.
	GitMissingMessage    = "git could not be run"
	GitMissingNextAction = "install git and make sure it is on PATH"
)

// IsGitMissing distinguishes git itself being absent from anything git had an
// opinion about. It is the one classification every caller needs and nobody
// should re-derive: telling someone to change directory when nothing can run
// git anywhere is the dead end SPEC §10 forbids.
func IsGitMissing(err error) bool { return errors.Is(err, exec.ErrNotFound) }

// UnbornHeadMessage is SPEC §4's `<name> has no commits yet`.
func UnbornHeadMessage(name string) string { return name + " has no commits yet" }

// ValidationError renders a failed validation in the words SPEC §4 fixes.
// Returns nil for StatusOK, which has no error.
func ValidationError(validation Validation) *lwerr.Error {
	switch validation.Status {
	case StatusGitMissing:
		return lwerr.Wrap(
			validation.Cause,
			lwerr.NotARepo,
			GitMissingMessage,
			GitMissingNextAction,
		)
	case StatusUnbornHead:
		return lwerr.New(
			lwerr.NotARepo,
			UnbornHeadMessage(validation.Repo.Name),
			UnbornHeadNextAction,
		)
	case StatusNotARepo:
		return lwerr.Wrap(
			validation.Cause,
			lwerr.NotARepo,
			NotARepoMessage,
			NotARepoNextAction,
		)
	default:
		return nil
	}
}

// mainCheckoutRoot is the main checkout of the repository dir belongs to, or
// false when dir is not in a linked worktree — which also covers bare main
// repos, submodules, and any git too old for --path-format=absolute. Those keep
// their plain toplevel.
func mainCheckoutRoot(ctx context.Context, dir string, run Runner) (string, bool) {
	result, err := run(ctx, dir, "git", []string{"rev-parse", "--path-format=absolute", "--git-dir", "--git-common-dir"})
	if err != nil || result.ExitCode != 0 {
		return "", false
	}
	lines := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	if len(lines) < 2 {
		return "", false
	}
	gitDir, commonDir := strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1])
	if gitDir == commonDir {
		return "", false
	}
	if filepath.Base(commonDir) != ".git" {
		return "", false
	}
	return filepath.Dir(commonDir), true
}
