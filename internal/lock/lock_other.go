//go:build !unix

package lock

import (
	"errors"
	"os"
	"runtime"
)

// errHeld is never returned here; it exists so lock.go compiles everywhere.
var errHeld = errors.New("lock: held")

// tryLock refuses. The docs name no platform beyond unix, and a server that
// starts without the one guard against a second writer is worse than one
// that says it cannot run here.
func tryLock(*os.File) error {
	return errors.New("the data directory lock is not implemented on " + runtime.GOOS +
		"; ketju serves only on unix-like systems")
}

func unlock(*os.File) error { return nil }
