package cli

import (
	"fmt"
	"io"

	"github.com/snaylaker/lw/internal/lwerr"
)

// UsageKind marks the failures that are the user's invocation rather than the
// world's state: they print their message, then the help text, and exit 2.
// SPEC §10 lists the kinds a *reported* error may carry; a usage error never
// reaches that reporter, so it is a kind of its own.
const UsageKind lwerr.Kind = "usage"

// usageNextAction is carried for completeness — the usage path prints the help
// text instead — so no error in the tree is ever a dead end.
const usageNextAction = "run lw --help to see every command and flag"

// UsageError is the invocation was wrong.
func UsageError(message string) *lwerr.Error {
	return lwerr.New(UsageKind, message, usageNextAction)
}

func usagef(format string, args ...any) *lwerr.Error {
	return UsageError(fmt.Sprintf(format, args...))
}

// Report is the single exit path every command shares, and the only place an
// error becomes an exit code:
//
//	nil            0, silent
//	cancelled      130, silent — a Ctrl+C is not a report
//	usage          2, the message then the help text
//	anything else  1, "error: <message>" then "next: <next action>"
func Report(err error, stderr io.Writer) int {
	if err == nil {
		return 0
	}
	if e, ok := lwerr.As(err); ok {
		switch e.Kind {
		case lwerr.Cancelled:
			return 130
		case UsageKind:
			fmt.Fprintf(stderr, "error: %s\n\n", e.Message)
			fmt.Fprint(stderr, helpText)
			return 2
		}
	}
	return lwerr.Report(err, stderr)
}
