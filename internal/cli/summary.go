package cli

import (
	"context"
	"strings"

	"github.com/snaylaker/lw/internal/worktree"
)

// runSummary is `lw summary <text>` (SPEC §9): the one mutable field of a
// worktree's metadata.
//
// The text is joined from the remaining arguments, so an unquoted sentence
// records what it looks like it records rather than only its first word.
// Success is silent: the answer is visible in the next `lw context`.
func runSummary(ctx context.Context, opts Options, env *execEnv) int {
	if len(opts.Args) == 0 {
		return Report(UsageError("lw summary needs the text to record"), env.stderr)
	}
	text := strings.Join(opts.Args, " ")
	// Refuse to summarise a ticket whose branch is gone: reaping first turns
	// that into the honest "carries no worktree metadata" rather than quietly
	// updating a file nothing points at any more.
	_, _ = worktree.PruneOrphanedMetadata(ctx, env.dir, env.run)
	if _, err := worktree.UpdateSummary(ctx, env.dir, text, env.run); err != nil {
		return Report(err, env.stderr)
	}
	return 0
}
