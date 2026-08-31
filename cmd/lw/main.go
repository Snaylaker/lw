// Command lw picks a Linear issue and opens a git worktree. Plain lw prints
// the path; lw run starts an explicit command inside it.
package main

import (
	"os"

	"github.com/snaylaker/lw/internal/cli"
)

func main() { os.Exit(cli.Main()) }
