package cli

import (
	"context"
	"fmt"

	"github.com/snaylaker/lw/internal/config"
	"github.com/snaylaker/lw/internal/worktree"
)

// readOnlyNotice ends every plain-text context. It is the whole promise of the
// integration surface: reading a worktree's ticket never writes to Linear.
const readOnlyNotice = "This context is read-only; it never writes to Linear."

// runContext is `lw context [--json]` (SPEC §9).
//
// The contract that matters is the silent one: outside a worktree this tool
// created it prints nothing and exits 0, so a caller can run it unconditionally
// in every repository without having to guard it.
func runContext(ctx context.Context, opts Options, env *execEnv) int {
	// Metadata naming a branch that no longer exists is reaped before it can be
	// reported. Best effort by design: a failure here must never turn the silent
	// command into a noisy one.
	_, _ = worktree.PruneOrphanedMetadata(ctx, env.dir, env.run)

	metadata, err := worktree.ReadMetadata(ctx, env.dir, env.run)
	if err != nil {
		// Not the silent case: the file is there and unusable, which is a real
		// problem and hiding it would only make it harder to find.
		return Report(err, env.stderr)
	}
	if metadata == nil {
		return 0
	}

	if opts.JSON {
		// The metadata object verbatim: the same encoder that wrote lw.json, so
		// the bytes match the file byte for byte.
		payload, err := config.MarshalIndented(metadata)
		if err != nil {
			return Report(err, env.stderr)
		}
		_, _ = env.stdout.Write(payload)
		return 0
	}

	fmt.Fprintf(env.stdout, "Ticket: %s — %s\n", metadata.Identifier, metadata.Title)
	fmt.Fprintln(env.stdout, metadata.URL)
	if metadata.Summary != "" {
		fmt.Fprintf(env.stdout, "Summary: %s\n", metadata.Summary)
	}
	fmt.Fprintln(env.stdout, readOnlyNotice)
	return 0
}
