package worktree

import (
	"context"
	"strings"

	"github.com/snaylaker/lw/internal/domain"
	"github.com/snaylaker/lw/internal/gitrepo"
	"github.com/snaylaker/lw/internal/lwerr"
)

// Reasons a branch counts as finished, quoted in the report so the user can see
// why a checkout was offered up before agreeing to delete it.
const (
	ReasonMerged       = "merged"
	ReasonUpstreamGone = "upstream gone"
)

// Candidate is one worktree whose work looks done. Identifier and Title come
// from its metadata, so only worktrees this tool created are ever candidates:
// a worktree someone added by hand carries no lw.json and is never touched.
type Candidate struct {
	Path       string
	Branch     string
	Identifier string
	Title      string
	// Reason is ReasonMerged or ReasonUpstreamGone.
	Reason string
}

// PruneOptions are the inputs of FindFinished.
type PruneOptions struct {
	Repo domain.Repo
	Run  gitrepo.Runner
	// Current is the worktree the user is standing in, which is never a
	// candidate: deleting the directory out from under a running shell is not a
	// cleanup, it is a surprise.
	Current string
}

// FindFinished lists the worktrees of a repository whose branch is merged into
// the default branch, or whose upstream has been deleted.
//
// The second reason is the one that matters in practice. A squash merge — the
// GitHub default — produces a commit the branch is not an ancestor of, so the
// merge test alone misses most real pull requests; what a merged-and-tidied PR
// reliably leaves behind is an upstream marked "gone".
//
// No network: this reads refs that are already local. `lw prune` fetches first
// so "gone" is current; the automatic pass deliberately does not, so opening a
// worktree never waits on a remote.
func FindFinished(ctx context.Context, opts PruneOptions) ([]Candidate, error) {
	run := opts.Run
	if run == nil {
		run = gitrepo.DefaultRunner
	}
	root := opts.Repo.Root

	def, ok := defaultBranch(ctx, root, run)
	trees, err := listWorktrees(ctx, root, run)
	if err != nil {
		return nil, err
	}

	var found []Candidate
	for _, tree := range trees {
		// The main checkout has no branch of ours and must never be a
		// candidate; nor must the directory we are standing in.
		if tree.branch == "" || samePath(tree.path, root) || samePath(tree.path, opts.Current) {
			continue
		}
		// Only what this tool made. No metadata, no claim on the directory.
		metadata, err := ReadMetadata(ctx, tree.path, run)
		if err != nil || metadata == nil || metadata.Identifier == "" {
			continue
		}

		reason := ""
		switch {
		case upstreamGone(ctx, root, tree.branch, run):
			reason = ReasonUpstreamGone
		case ok && mergedInto(ctx, root, tree.branch, def, run):
			reason = ReasonMerged
		default:
			continue
		}

		found = append(found, Candidate{
			Path:       tree.path,
			Branch:     tree.branch,
			Identifier: metadata.Identifier,
			Title:      metadata.Title,
			Reason:     reason,
		})
	}
	return found, nil
}

// Fetch updates remote-tracking refs and drops the ones whose remote branch is
// gone, which is what makes ReasonUpstreamGone current. It is the one part of
// pruning that touches the network, so it is always an explicit step.
func Fetch(ctx context.Context, repo domain.Repo, run gitrepo.Runner) error {
	if run == nil {
		run = gitrepo.DefaultRunner
	}
	result, err := run(ctx, repo.Root, "git", []string{"fetch", "--prune", "--quiet"})
	if err != nil || result.ExitCode != 0 {
		return lwerr.Wrap(err, lwerr.Internal,
			"could not fetch from the remote",
			"check your network and remote access, or run lw prune --no-fetch",
		)
	}
	return nil
}

// Remove deletes one finished worktree and its branch. It never forces: a
// checkout with uncommitted work refuses to go, which is the correct answer —
// the point of pruning is to reclaim finished directories, not to throw away
// something the user forgot to commit.
func Remove(ctx context.Context, repo domain.Repo, candidate Candidate, run gitrepo.Runner) error {
	if run == nil {
		run = gitrepo.DefaultRunner
	}
	result, err := run(ctx, repo.Root, "git", []string{"worktree", "remove", candidate.Path})
	if err != nil || result.ExitCode != 0 {
		return lwerr.Wrap(err, lwerr.WorktreeConflict,
			candidate.Identifier+" could not be removed: "+firstLine(result.Stderr),
			"commit or discard the work in "+candidate.Path+", then re-run",
		)
	}
	// The branch is only worth deleting once its worktree is gone; -d refuses
	// anything not actually merged, which is a second opinion on our own test.
	if _, err := run(ctx, repo.Root, "git", []string{"branch", "-d", candidate.Branch}); err != nil {
		return nil
	}
	return nil
}

type worktreeEntry struct {
	path   string
	branch string
}

// listWorktrees parses `git worktree list --porcelain`, whose records are blank
// line separated and whose branch line is absent for a detached HEAD.
func listWorktrees(ctx context.Context, root string, run gitrepo.Runner) ([]worktreeEntry, error) {
	result, err := run(ctx, root, "git", []string{"worktree", "list", "--porcelain"})
	if err != nil || result.ExitCode != 0 {
		return nil, lwerr.Wrap(err, lwerr.Internal,
			"could not list the worktrees of "+root,
			"check that git works in this repository, then re-run",
		)
	}

	var entries []worktreeEntry
	var current worktreeEntry
	flush := func() {
		if current.path != "" {
			entries = append(entries, current)
		}
		current = worktreeEntry{}
	}
	for _, line := range strings.Split(result.Stdout, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "worktree "):
			flush()
			current.path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "branch "):
			current.branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	flush()
	return entries, nil
}

// defaultBranch asks the remote's HEAD, then falls back to the conventional
// names. ok is false when nothing plausible exists, which disables the merge
// test rather than guessing a branch to compare against.
func defaultBranch(ctx context.Context, root string, run gitrepo.Runner) (string, bool) {
	if result, err := run(ctx, root, "git",
		[]string{"symbolic-ref", "--short", "refs/remotes/origin/HEAD"}); err == nil && result.ExitCode == 0 {
		if name := strings.TrimSpace(result.Stdout); name != "" {
			return name, true
		}
	}
	for _, name := range []string{"main", "master"} {
		if exists, _ := branchExists(ctx, root, name, run); exists {
			return name, true
		}
	}
	return "", false
}

// mergedInto is true when every commit on branch is already contained in def.
func mergedInto(ctx context.Context, root, branch, def string, run gitrepo.Runner) bool {
	if branch == def || strings.HasSuffix(def, "/"+branch) {
		return false
	}
	result, err := run(ctx, root, "git", []string{"merge-base", "--is-ancestor", branch, def})
	return err == nil && result.ExitCode == 0
}

// upstreamGone is true when the branch tracked a remote branch that no longer
// exists — what a merged and deleted pull request leaves behind.
func upstreamGone(ctx context.Context, root, branch string, run gitrepo.Runner) bool {
	result, err := run(ctx, root, "git", []string{
		"for-each-ref", "--format=%(upstream:track)", "refs/heads/" + branch,
	})
	if err != nil || result.ExitCode != 0 {
		return false
	}
	return strings.Contains(result.Stdout, "[gone]")
}

func firstLine(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		return strings.TrimSpace(text[:i])
	}
	if text == "" {
		return "git gave no reason"
	}
	return text
}
