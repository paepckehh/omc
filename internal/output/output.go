// Package output centralizes all user-facing formatting. Diagnostics and
// progress go to stderr; the final commit result and history go to stdout,
// preserving the Unix convention that stdout carries program output.
package output

import (
	"fmt"
	"io"
)

// UI holds the two output streams.
type UI struct {
	Out io.Writer // program output (final result, git log)
	Err io.Writer // diagnostics, progress, errors
}

// New returns a UI writing to the passed streams.
func New(out, err io.Writer) *UI {
	return &UI{Out: out, Err: err}
}

// Infof prints a diagnostic line to stderr.
func (u *UI) Infof(format string, args ...any) {
	fmt.Fprintf(u.Err, format+"\n", args...)
}

// Printf prints program output to stdout.
func (u *UI) Printf(format string, args ...any) {
	fmt.Fprintf(u.Out, format, args...)
}
