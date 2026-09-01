// Package lw exposes the compile-time provider extension entry point for custom
// binaries. The official cmd/lw binary uses Run with no extensions and includes
// the built-in Linear, GitHub, and Jira providers.
package lw

import (
	"io"

	"github.com/snaylaker/lw/internal/cli"
	issueprovider "github.com/snaylaker/lw/provider"
)

// Options configure a custom lw binary without exposing internal orchestration
// types. Extension providers are selected by their ID through --provider,
// LW_ISSUE_PROVIDER, or issueProvider in config.json.
type Options struct {
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	Env       map[string]string
	Platform  string
	Dir       string
	Providers []issueprovider.Provider
}

// Run executes lw without calling os.Exit. A custom main can pass provider
// implementations here and exit with the returned code.
func Run(argv []string, options Options) int {
	return cli.Run(argv, cli.Deps{
		Stdin: options.Stdin, Stdout: options.Stdout, Stderr: options.Stderr,
		Env: options.Env, Platform: options.Platform, Dir: options.Dir,
		Providers: options.Providers,
	})
}
