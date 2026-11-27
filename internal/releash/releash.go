// Package releash holds the logic behind the releash CLI. The cobra wiring in
// package main maps options into a Config or CheckOptions and calls a Releash;
// everything the CLI does lives here so tests can drive it without a process.
package releash

import (
	"io"
	"os"
)

// Exit codes from ADR 0004. They are part of the public contract: existing
// codes never change meaning.
const (
	// ExitOK means releash gave an answer, or printed help or its version.
	ExitOK = 0
	// ExitInternal means an unexpected internal error.
	ExitInternal = 1
	// ExitUsage means a bad command line: an unknown command or option, a
	// missing or extra argument, an invalid --timeout or a repeated releash
	// option.
	ExitUsage = 2
	// ExitNotConfigured means no Unleash URL or API token, or an empty one.
	ExitNotConfigured = 3
	// ExitInvalid means an invalid context value, property name or Unleash URL.
	ExitInvalid = 4
	// ExitDuplicate means a context field or property was set more than once.
	ExitDuplicate = 5
)

// Config holds settings shared by every releash command. It has no fields
// yet: Check takes its settings per call in CheckOptions.
type Config struct{}

// Releash holds the configuration and output writers that releash commands
// run with. Commands, such as Check, are methods that write to Out and Err and
// return an exit code instead of calling os.Exit. A Releash is not safe for
// concurrent use.
type Releash struct {
	*Config

	// Out receives command output. It defaults to os.Stdout.
	Out io.Writer
	// Err receives errors and diagnostics. It defaults to os.Stderr.
	Err io.Writer
}

// New returns a Releash with an empty Config that writes to os.Stdout and
// os.Stderr.
func New() *Releash {
	return NewWithConfig(Config{})
}

// NewWithConfig returns a Releash that uses a copy of config and writes to
// os.Stdout and os.Stderr.
func NewWithConfig(config Config) *Releash {
	return &Releash{
		Config: &config,
		Out:    os.Stdout,
		Err:    os.Stderr,
	}
}
