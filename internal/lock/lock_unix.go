//go:build unix

package lock

import (
	"errors"
	"os"
	"syscall"
)

// errHeld is tryLock's answer when another open file holds the lock.
var errHeld = errors.New("lock: held")

// tryLock takes an exclusive flock without waiting. A flock belongs to the
// open file description, so a second open of the same file in this process
// is refused as well; that is what the in-process test relies on.
func tryLock(f *os.File) error {
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, syscall.EINTR):
			continue
		case errors.Is(err, syscall.EWOULDBLOCK):
			return errHeld
		default:
			return err
		}
	}
}

func unlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
