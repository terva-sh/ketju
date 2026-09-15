package main

import "errors"

// Exit codes. A tool that reports everything as 1 is a tool nobody can put in
// a cron job, and these two are meant to be run from one.
const (
	exitOK       = 0
	exitCorrupt  = 1 // a checksum failed, or a log could not be walked
	exitUsage    = 2
	exitDrift    = 3 // logs are sound but something derived from them is behind
	exitInternal = 4
)

// errUsage is a mistake in how the command was invoked.
type errUsage struct{ err error }

func (e errUsage) Error() string { return e.err.Error() }
func (e errUsage) Unwrap() error { return e.err }

// errCorrupt is damage found in the data, as opposed to a failure to look.
type errCorrupt struct{ err error }

func (e errCorrupt) Error() string { return e.err.Error() }
func (e errCorrupt) Unwrap() error { return e.err }

// errDrift is data that is sound in itself but out of step with something
// derived from it.
type errDrift struct{ err error }

func (e errDrift) Error() string { return e.err.Error() }
func (e errDrift) Unwrap() error { return e.err }

func exitCodeFor(err error) int {
	var u errUsage
	var c errCorrupt
	var d errDrift
	switch {
	case err == nil:
		return exitOK
	case errors.As(err, &u):
		return exitUsage
	case errors.As(err, &c):
		return exitCorrupt
	case errors.As(err, &d):
		return exitDrift
	default:
		return exitInternal
	}
}
