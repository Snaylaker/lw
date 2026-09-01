// Command lw picks an issue and opens a git worktree. Plain lw prints
// the path; lw run starts an explicit command inside it.
package main

import (
	"os"

	lw "github.com/snaylaker/lw"
)

func main() {
	os.Exit(lw.Run(os.Args[1:], lw.Options{
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
	}))
}
