//go:build unix

package lock

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// errHeld is tryLock's answer when another open file holds the lock.
var errHeld = errors.New("lock: held")

// openDir opens the data directory to hold the flock. O_DIRECTORY refuses
// anything at that path that is not a directory. A data_dir configured as a
// symlink to one is the operator's choice, and the flock lands on the
// directory it names, which is the one serve will write to.
func openDir(dir string) (*os.File, error) {
	return os.OpenFile(dir, os.O_RDONLY|syscall.O_DIRECTORY, 0)
}

// openRecord opens the record for a refusal to read, without following a
// symlink planted there.
func openRecord(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
}

// openFile opens the record file for writing, creating it if it is not
// there, and refuses one that is not a plain file of its own.
//
// record truncates and rewrites what it opens, so the file has to be the
// record and nothing else (review 433). O_NOFOLLOW refuses a symlink,
// which would otherwise aim that truncation at whatever it names; the check
// after the open refuses a directory, a device or a fifo, and a hard link,
// which is the same aim without a symlink.
//
// Not O_TRUNC: the holder's record has to survive a refused open, or the
// refusal could not say who holds the lock.
func openFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o644)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, fmt.Errorf("%s is a symlink; the lock record must be a plain file", path)
		}
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	switch {
	case !fi.Mode().IsRegular():
		f.Close()
		return nil, fmt.Errorf("%s is not a plain file (%s)", path, fi.Mode().Type())
	case ok && st.Nlink != 1:
		f.Close()
		return nil, fmt.Errorf("%s has %d links; the lock record must be a file of its own", path, st.Nlink)
	}
	return f, nil
}

// tryLock takes an exclusive flock without waiting. A flock belongs to the
// open file description, so a second open of the same directory in this
// process is refused as well; that is what the in-process test relies on.
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
