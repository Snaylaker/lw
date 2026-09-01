// Package gitrepo resolves and validates the git checkout a run operates on.
// Nothing here writes to a repository: every git invocation is a read-only
// rev-parse.
package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"

	"github.com/snaylaker/lw/internal/processenv"
)

// ExecResult is a finished command. ExitCode is meaningful only for a command
// that actually ran.
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Runner runs a command with dir as its working directory. A command that ran
// and failed yields a non-zero ExitCode and a nil error; only a command that
// could not be started at all (git missing, dir unreadable) yields an error.
// Callers inject a fake so tests never touch a real repository.
type Runner func(ctx context.Context, dir, name string, args []string) (ExecResult, error)

// DefaultRunner is the Runner used when none is injected. A process killed by a
// signal has no exit code, so it is reported as a failure to run rather than as
// a non-zero exit.
func DefaultRunner(ctx context.Context, dir, name string, args []string) (ExecResult, error) {
	return NewRunner(processenv.BuiltInProviderSecrets())(ctx, dir, name, args)
}

// NewRunner returns a Git runner that removes every named provider secret from
// Git, hooks, filters, and other child processes Git may start.
func NewRunner(sensitive []string) Runner {
	blocked := append([]string(nil), sensitive...)
	return func(ctx context.Context, dir, name string, args []string) (ExecResult, error) {
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Dir = dir
		cmd.Env = processenv.Without(os.Environ(), blocked)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		result := ExecResult{Stdout: stdout.String(), Stderr: stderr.String()}
		if err == nil {
			return result, nil
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() >= 0 {
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		return ExecResult{}, err
	}
}
