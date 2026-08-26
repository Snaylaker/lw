package cli

import (
	"context"
	"fmt"

	"github.com/snaylaker/lw/internal/config"
	"github.com/snaylaker/lw/internal/domain"
	"github.com/snaylaker/lw/internal/gitrepo"
	"github.com/snaylaker/lw/internal/worktree"
)

// runPrune is `lw prune [--yes] [--no-fetch]` (SPEC §5).
//
// It reports by default and deletes only with --yes. A worktree is someone's
// working directory: listing what would go is recoverable, removing it is not,
// so the destructive form is the one you have to ask for.
func runPrune(ctx context.Context, opts Options, env *execEnv) int {
	// --auto/--no-auto only record a preference; they are not a prune. Saying
	// so and stopping beats quietly doing both from one command.
	if opts.Auto || opts.NoAuto {
		return setAutoPrune(opts, env)
	}

	repo, err := gitrepo.Source(ctx, gitrepo.SourceOptions{Dir: env.dir, Run: env.run})
	if err != nil {
		return Report(err, env.stderr)
	}

	// A branch only looks "gone" once the remote-tracking refs are current, so
	// the manual command fetches unless told not to. A fetch that fails is
	// reported and then ignored: stale refs still find merged branches.
	if !opts.NoFetch {
		if err := worktree.Fetch(ctx, repo, env.run); err != nil {
			fmt.Fprintln(env.stderr, "warning: could not fetch; judging from the refs already here")
		}
	}

	finished, err := worktree.FindFinished(ctx, worktree.PruneOptions{
		Repo:    repo,
		Run:     env.run,
		Current: env.dir,
	})
	if err != nil {
		return Report(err, env.stderr)
	}
	if len(finished) == 0 {
		fmt.Fprintln(env.stdout, "Nothing to prune.")
		return 0
	}

	if !opts.Yes {
		fmt.Fprintf(env.stdout, "%d finished worktree(s):\n\n", len(finished))
		for _, candidate := range finished {
			fmt.Fprintln(env.stdout, describeCandidate(candidate))
		}
		fmt.Fprintln(env.stdout, "\nNothing was removed. Run lw prune --yes to remove them.")
		return 0
	}

	return removeFinished(ctx, repo, finished, env)
}

// removeFinished deletes each candidate, reporting per worktree. One that
// refuses to go — uncommitted work, most often — never stops the others: the
// point is to reclaim what is finished, and one busy checkout is not a reason
// to leave the rest behind.
func removeFinished(ctx context.Context, repo domain.Repo, finished []worktree.Candidate, env *execEnv) int {
	removed, failed := 0, 0
	for _, candidate := range finished {
		if err := worktree.Remove(ctx, repo, candidate, env.run); err != nil {
			Report(err, env.stderr)
			failed++
			continue
		}
		fmt.Fprintf(env.stdout, "removed %s (%s)\n", candidate.Identifier, candidate.Reason)
		removed++
	}
	fmt.Fprintf(env.stdout, "\n%d removed, %d kept.\n", removed, failed)
	if failed > 0 {
		return 1
	}
	return 0
}

func describeCandidate(candidate worktree.Candidate) string {
	line := "  " + candidate.Identifier
	if candidate.Title != "" {
		line += " " + candidate.Title
	}
	return line + "\n    " + candidate.Path + "  (" + candidate.Reason + ")"
}

// setAutoPrune persists `pruneMerged` so the choice survives the shell.
func setAutoPrune(opts Options, env *execEnv) int {
	if opts.Auto && opts.NoAuto {
		return Report(UsageError("lw prune takes --auto or --no-auto, not both"), env.stderr)
	}
	enabled := opts.Auto
	changed, err := config.SetPruneMerged(enabled, env.configPath())
	if err != nil {
		return Report(err, env.stderr)
	}

	state := "off"
	if enabled {
		state = "on"
	}
	if !changed {
		fmt.Fprintf(env.stdout, "Automatic pruning is already %s.\n", state)
		return 0
	}
	fmt.Fprintf(env.stdout, "Automatic pruning is now %s (%s).\n", state, env.configPath())
	if enabled {
		fmt.Fprintln(env.stdout, "Finished worktrees will be removed before opening the next one.")
	}
	return 0
}

// pruneMergedIfConfigured is the automatic half, run on the selected repository
// before its worktree is opened. It never fetches, so opening a worktree does not
// wait on a remote, and it never reports a failure: cleanup is a convenience,
// and a user asking for a worktree should not be handed someone else's problem.
func pruneMergedIfConfigured(ctx context.Context, repo domain.Repo, env *execEnv) {
	enabled, err := config.ReadPruneMerged(env.configPath())
	if err != nil || !enabled {
		return
	}
	finished, err := worktree.FindFinished(ctx, worktree.PruneOptions{
		Repo:    repo,
		Run:     env.run,
		Current: env.dir,
	})
	if err != nil {
		return
	}
	for _, candidate := range finished {
		if err := worktree.Remove(ctx, repo, candidate, env.run); err == nil {
			fmt.Fprintf(env.stderr, "pruned %s (%s)\n", candidate.Identifier, candidate.Reason)
		}
	}
}
