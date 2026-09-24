//go:build !unix

package lock

import (
	"errors"
	"os"
	"runtime"
)

// errHeld is never returned here; it exists so lock.go compiles everywhere.
var errHeld = errors.New("lock: held")

// errUnsupported refuses. The docs name no platform beyond unix, and a server
// that starts without the one guard against a second writer is worse than
// one that says it cannot run here.
var errUnsupported = errors.New("the data directory lock is not implemented on " + runtime.GOOS +
	"; ketju serves only on unix-like systems")

// openDir refuses before touching the directory, so a refused Acquire
// leaves nothing behind and says why whether or not the directory exists
// (review 441).
func openDir(string) (*os.File, error) { return nil, errUnsupported }

func openFile(string) (*os.File, error) { return nil, errUnsupported }

func openRecord(string) (*os.File, error) { return nil, errUnsupported }

func tryLock(*os.File) error { return errUnsupported }

func unlock(*os.File) error { return nil }

func running(int) bool { return true }
