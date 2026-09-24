// Package lock is the lock file that keeps a data directory to one process
// (docs/02 "One process per data directory", docs/10 item 24).
//
// Every thread log has exactly one writer, its actor, and the database has
// one write connection. A second process against the same directory would
// not share that work; it would be a second writer to every file, and the
// corruption would be quiet. So the process that owns the directory locks it
// first, before opening anything, and a second one is refused with a message
// saying who holds it.
//
// # What is locked
//
// The lock is an advisory flock on the data directory itself, not on a file
// inside it. A flock belongs to an inode, and a lock file's inode can be
// swapped out from under its holder: delete or replace data/ketju.lock while
// a server runs, and the next process would lock the new file and believe
// the directory was its own (review 443). The directory's inode cannot be
// replaced without replacing the directory, which is other data.
//
// data/ketju.lock is only the record of who took the lock last, for the
// refusal message. Nothing reads it to decide anything, and deleting it
// loses a name, not the lock.
//
// # Staleness
//
// The kernel drops a flock when the holder's descriptor closes, and a
// process that is killed has every descriptor closed for it, however it
// died. So a lock left by a killed process is simply not held: the next
// Acquire takes it, with no pid probing, no timeout, and nothing for an
// operator to delete.
package lock

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"
)

// FileName is the lock file's name inside the data directory.
const FileName = "ketju.lock"

// Holder is what the record file records about the process that took it.
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

	// Holder is what the record file says, when it could be read. Nil when
	// it could not, which is said in the message; the refusal stands either
	// way, because the flock is what is held, not the file's contents.
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
		msg += fmt.Sprintf(" (The lock record could not be read to name the holder: %v.)", e.ReadErr)
	}
	return msg
}

// Lock is a held lock. Release it on shutdown, after everything that writes
// to the directory has stopped.
type Lock struct {
	d *os.File // the data directory, which holds the flock
}

// Acquire takes the lock on dir for purpose ("serve", "reindex"). It fails
// with a *HeldError when another process holds it.
//
// dir must exist: creating the data directory is the caller's decision, and
// a lock taken in a directory that a typo just created would protect nothing.
func Acquire(dir, purpose string) (*Lock, error) {
	d, err := openDir(dir)
	if err != nil {
		return nil, fmt.Errorf("lock: opening %s: %w", dir, err)
	}
	path := filepath.Join(dir, FileName)
	if err := tryLock(d); err != nil {
		d.Close()
		if errors.Is(err, errHeld) {
			held := &HeldError{Dir: dir}
			held.Holder, held.ReadErr = readHolder(path)
			return nil, held
		}
		return nil, fmt.Errorf("lock: locking %s: %w", dir, err)
	}

	if err := record(path, purpose); err != nil {
		unlock(d)
		d.Close()
		return nil, fmt.Errorf("lock: recording the holder in %s: %w", path, err)
	}
	return &Lock{d: d}, nil
}

// Release gives the lock up. It empties the record first, while the lock is
// still held: the next holder writes its own record only after taking the
// lock, and a refusal in between must find no name rather than this one
// (review 445). The file itself stays.
func (l *Lock) Release() error {
	if l == nil || l.d == nil {
		return nil
	}
	d := l.d
	l.d = nil
	return errors.Join(clearRecord(filepath.Join(d.Name(), FileName)), unlock(d), d.Close())
}

func clearRecord(path string) error {
	f, err := openFile(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Truncate(0); err != nil {
		return err
	}
	return f.Sync()
}

// record replaces the record file's contents with this process's Holder,
// durably, so a refusal after a crash still names the last holder. It runs
// only while the directory is locked, so no two processes write it at once.
func record(path, purpose string) error {
	f, err := openFile(path)
	if err != nil {
		return err
	}
	defer f.Close()
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

// readHolder reads the record a refused Acquire names the holder from.
//
// A holder writes its record just after taking the lock, so a refusal in
// that moment can find the record of whoever held the lock before. After a
// clean Release that is an empty file, which reads as unnamed. After a
// holder that was killed it is that holder's record, naming a process that
// is gone; when it names this host, the pid is checked and a record whose
// process is not running is not believed (review 445). A record from
// another host cannot be checked, and a pid the system has since reused
// passes; the message says to check, and names what it read.
func readHolder(path string) (*Holder, error) {
	f, err := openRecord(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// Bounded: a record is well under a kilobyte, and a file someone filled
	// with something else should not be read whole into memory.
	b, err := io.ReadAll(io.NewSectionReader(f, 0, 64<<10))
	if err != nil {
		return nil, err
	}
	var h Holder
	if err := json.Unmarshal(b, &h); err != nil {
		return nil, fmt.Errorf("%s does not hold a holder record: %w", path, err)
	}
	// Valid JSON is not yet a record: {} or null decodes without error into
	// a holder with pid 0 and no host, and a refusal naming that would
	// claim to know who holds the lock when it does not (review 440).
	var missing []string
	if h.PID <= 0 {
		missing = append(missing, "pid")
	}
	if h.Host == "" {
		missing = append(missing, "host")
	}
	if h.Purpose == "" {
		missing = append(missing, "purpose")
	}
	if h.StartedAt.IsZero() {
		missing = append(missing, "started_at")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%s holds an incomplete holder record (no %s)", path, strings.Join(missing, ", "))
	}
	if host, _ := os.Hostname(); h.Host == host && !running(h.PID) {
		return nil, fmt.Errorf("%s names pid %d, which is not running: it held the lock before, "+
			"and the process holding it now has not recorded itself yet", path, h.PID)
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
