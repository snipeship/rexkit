package popup

import (
	"errors"
	"flag"
)

// ExitUsage is the exit status for a command-line mistake.
const ExitUsage = 2

// UsageError is a command-line mistake that the flag package has already
// reported (with the usage) on stderr. It exits with ExitUsage, and
// Reported tells the caller not to print it again.
type UsageError struct{ Err error }

func (e *UsageError) Error() string  { return e.Err.Error() }
func (e *UsageError) Unwrap() error  { return e.Err }
func (e *UsageError) ExitCode() int  { return ExitUsage }
func (e *UsageError) Reported() bool { return true }

// ParseFlags parses args with fs, which should use flag.ContinueOnError. A
// parse error comes back as a *UsageError; -h comes back as flag.ErrHelp.
// Every rexkit command parses its flags through it, so a mistyped flag is
// reported once and exits 2 everywhere.
func ParseFlags(fs *flag.FlagSet, args []string) error {
	err := fs.Parse(args)
	if err == nil || errors.Is(err, flag.ErrHelp) {
		return err
	}
	return &UsageError{Err: err}
}
