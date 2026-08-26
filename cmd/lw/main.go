// Command lw picks a Linear issue, opens a git worktree for it, and prints the
// worktree path on stdout.
package main

import (
	"os"

	"github.com/snaylaker/lw/internal/cli"
)

func main() { os.Exit(cli.Main()) }
