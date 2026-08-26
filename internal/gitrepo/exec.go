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
	"strings"
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
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	// A key supplied through LINEAR_API_KEY belongs to lw, not to git, hooks,
	// filters or any other process git may start.
	cmd.Env = withoutLinearKey(os.Environ())
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

func withoutLinearKey(environ []string) []string {
	result := make([]string, 0, len(environ))
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(name, "LINEAR_API_KEY") {
			continue
		}
		result = append(result, entry)
	}
	return result
}
