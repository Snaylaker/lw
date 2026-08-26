// Package lwerr carries every user-facing failure. A LauncherError always
// offers a next action, so no error screen is a dead end.
package lwerr

import (
	"errors"
	"fmt"
	"io"
)

type Kind string

// The seven kinds of SPEC §10, and no others. There is deliberately no
// "auth_expired": an API key has no expiry, so a key Linear refuses is revoked
// or under-scoped, which is the same situation as having no key at all —
// auth_required, with a next action that says to make a new one.
const (
	AuthRequired      Kind = "auth_required"
	LinearUnavailable Kind = "linear_unavailable"
	NotARepo          Kind = "not_a_repo"
	ConfigInvalid     Kind = "config_invalid"
	WorktreeConflict  Kind = "worktree_conflict"
	Cancelled         Kind = "cancelled"
	Internal          Kind = "internal"
)

// Kinds is every kind, in the order SPEC §10 lists them. It exists so the set
// itself can be asserted on: a kind added here without a place in the spec, or
// a spec kind never declared, is a divergence worth failing a test over.
var Kinds = []Kind{
	AuthRequired,
	LinearUnavailable,
	NotARepo,
	ConfigInvalid,
	WorktreeConflict,
	Cancelled,
	Internal,
}

// Error never carries credential material in Message.
type Error struct {
	Kind       Kind
	Message    string
	NextAction string
	Cause      error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Cause }

func New(kind Kind, message, nextAction string) *Error {
	return &Error{Kind: kind, Message: message, NextAction: nextAction}
}

func Wrap(cause error, kind Kind, message, nextAction string) *Error {
	return &Error{Kind: kind, Message: message, NextAction: nextAction, Cause: cause}
}

// As reports whether err is a *Error, yielding it.
func As(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}

// Is reports whether err is a *Error of the given kind.
func Is(err error, kind Kind) bool {
	e, ok := As(err)
	return ok && e.Kind == kind
}

// Cancelled is the one error the launcher reports as exit 130 rather than 1.
func NewCancelled() *Error { return New(Cancelled, "operation cancelled", "nothing to do") }

// FallbackNextAction is the next action of an error that arrived without one.
// SPEC §10 makes the two lines unconditional — no error is a dead end — so a
// plain error still gets a second line, and the one thing that is always true
// of an error nothing in this tool classified is that it is worth reporting.
const FallbackNextAction = "report this: it is a bug in lw"

// Report writes the single error format every command shares and returns the
// exit code: 130 for cancellation, 1 otherwise. Both lines are always printed.
func Report(err error, w io.Writer) int {
	if e, ok := As(err); ok {
		if e.Kind == Cancelled {
			return 130
		}
		next := e.NextAction
		if next == "" {
			next = FallbackNextAction
		}
		fmt.Fprintf(w, "error: %s\nnext: %s\n", e.Message, next)
		return 1
	}
	fmt.Fprintf(w, "error: %s\nnext: %s\n", err, FallbackNextAction)
	return 1
}
