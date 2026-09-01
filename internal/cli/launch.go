package cli

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"strconv"

	"github.com/snaylaker/lw/internal/domain"
	"github.com/snaylaker/lw/internal/lwerr"
	"github.com/snaylaker/lw/internal/processenv"
)

// ChildRunner starts one explicit command in a resolved worktree. It receives
// arguments as an array rather than a shell line, so flags and spaces are never
// re-parsed by a platform shell. A non-negative exit code is the child process's
// own. A negative code means the process ended without one, usually by signal.
type ChildRunner func(
	dir string,
	argv []string,
	environ []string,
	stdin io.Reader,
	stdout, stderr io.Writer,
) (int, error)

// runLaunch is `lw run -- <command> [args...]`: resolve the normal issue and
// worktree flow, then give the child the terminal in that worktree. Unlike plain
// `lw`, this command does not print the path because stdout belongs to the child.
func runLaunch(ctx context.Context, opts Options, env *execEnv) int {
	flow, err := newFlow(ctx, opts, env)
	if err != nil {
		return Report(err, env.stderr)
	}
	result, code := flow.pick(ctx)
	if result == nil {
		return code
	}
	return flow.launch(ctx, *result)
}

func (f *flow) launch(ctx context.Context, result domain.FlowResult) int {
	code, err := f.env.child(
		result.CheckoutPath,
		f.opts.Args,
		processenv.FromMap(f.env.env, f.env.registry.sensitiveNames()),
		f.env.stdin,
		f.env.stdout,
		f.env.stderr,
	)
	if err != nil {
		command := strconv.Quote(f.opts.Args[0])
		return Report(lwerr.Wrap(err, lwerr.Internal,
			"could not start "+command+" in "+result.CheckoutPath,
			"install "+command+" and make sure it is on PATH, then retry"), f.env.stderr)
	}
	if code >= 0 {
		return code
	}
	if ctx.Err() != nil {
		return 130
	}
	return 1
}

func runChild(dir string, argv, environ []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	command := exec.Command(argv[0], argv[1:]...)
	command.Dir = dir
	command.Env = environ
	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr

	err := command.Run()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return -1, err
}
