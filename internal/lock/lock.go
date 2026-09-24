// Package lock is the lock file that keeps a data directory to one process
// (docs/02 "One process per data directory", docs/10 item 24).
//
// Every thread log has exactly one writer, its actor, and the database has
// one write connection. A second process against the same directory would
// not share that work; it would be a second writer to every file, and the
// corruption would be quiet. So the process that owns the directory takes
// data/ketju.lock first, before opening anything, and a second one is
// refused with a message saying who holds it.
//
// # Staleness
//
// The lock is an advisory flock on the file, not the file's existence. The
// kernel drops it when the holder's descriptor closes, and a process that is
// killed has every descriptor closed for it, however it died. So a lock left
// by a killed process is simply not held: the next Acquire takes it, with no
// pid probing, no timeout, and nothing for an operator to delete. The file
// itself stays on disk; what it says is who took it last, for the refusal
// message, and nothing reads it to decide anything.
package lock

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"
)

// FileName is the lock file's name inside the data directory.
const FileName = "ketju.lock"

// Holder is what the lock file records about the process that took it.
type Holder struct {
	PID       int       `json:"pid"`
	Host      string    `json:"host"`
	Purpose   string    `json:"purpose"`
	StartedAt time.Time `json:"started_at"`
	Version   string    `json:"version"`
}

// HeldError is a refusal: another process holds the lock.
type HeldError struct {
	// Dir is the data directory.
	Dir string

	// Holder is what the lock file says, when it could be read. Nil when it
	// could not, which is said in the message; the refusal stands either way,
	// because the flock is what is held, not the file's contents.
	Holder *Holder

	// ReadErr is why Holder could not be read.
	ReadErr error
}

func (e *HeldError) Error() string {
	who := "another process"
	if h := e.Holder; h != nil {
		who = fmt.Sprintf("ketju %s (pid %d on host %s, started %s)",
			h.Purpose, h.PID, h.Host, h.StartedAt.UTC().Format(time.RFC3339))
	}
	msg := fmt.Sprintf("data directory %s is in use by %s. "+
		"Check for another running ketju (ps, systemctl status, docker ps): "+
		"two processes writing the same thread logs would corrupt them. "+
		"If nothing is running, check whether the data directory is on a network filesystem, "+
		"where locks may not work.", e.Dir, who)
	if e.Holder == nil && e.ReadErr != nil {
		msg += fmt.Sprintf(" (The lock file could not be read to name the holder: %v.)", e.ReadErr)
	}
	return msg
}

// Lock is a held lock. Release it on shutdown, after everything that writes
// to the directory has stopped.
type Lock struct {
	f *os.File
}

// Acquire takes the lock on dir for purpose ("serve", "reindex"). It fails
// with a *HeldError when another process holds it.
//
// dir must exist: creating the data directory is the caller's decision, and
// a lock taken in a directory that a typo just created would protect nothing.
func Acquire(dir, purpose string) (*Lock, error) {
	path := filepath.Join(dir, FileName)
	// Not O_TRUNC: the holder's record has to survive a refused open, or the
	// refusal could not say who holds the lock.
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("lock: opening %s: %w", path, err)
	}
	if err := tryLock(f); err != nil {
		f.Close()
		if errors.Is(err, errHeld) {
			held := &HeldError{Dir: dir}
			held.Holder, held.ReadErr = readHolder(path)
			return nil, held
		}
		return nil, fmt.Errorf("lock: locking %s: %w", path, err)
	}

	if err := record(f, purpose); err != nil {
		unlock(f)
		f.Close()
		return nil, fmt.Errorf("lock: recording the holder in %s: %w", path, err)
	}
	return &Lock{f: f}, nil
}

// Release gives the lock up. The file is left in place: removing it would
// race a second process that has already opened it and is about to lock the
// unlinked inode, and then two processes would each hold "the" lock.
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	f := l.f
	l.f = nil
	return errors.Join(unlock(f), f.Close())
}

// record replaces the file's contents with this process's Holder, durably,
// so a refusal after a crash still names the last holder.
func record(f *os.File, purpose string) error {
	host, _ := os.Hostname()
	b, err := json.Marshal(Holder{
		PID: os.Getpid(), Host: host, Purpose: purpose,
		StartedAt: time.Now().UTC().Truncate(time.Second), Version: version(),
	})
	if err != nil {
		return err
	}
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.WriteAt(append(b, '\n'), 0); err != nil {
		return err
	}
	return f.Sync()
}

func readHolder(path string) (*Holder, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var h Holder
	if err := json.Unmarshal(b, &h); err != nil {
		return nil, fmt.Errorf("%s does not hold a holder record: %w", path, err)
	}
	return &h, nil
}

// version is the module version the binary was built at, or "(devel)".
func version() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" {
		return bi.Main.Version
	}
	return "(devel)"
}
