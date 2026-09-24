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
	d   *os.File // the data directory, which holds the lock
	rec *os.File // the record, locked for as long as this holds
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

	rec, err := record(path, purpose)
	if err != nil {
		unlock(d)
		d.Close()
		return nil, fmt.Errorf("lock: recording the holder in %s: %w", path, err)
	}
	return &Lock{d: d, rec: rec}, nil
}

// Release gives the lock up. It empties the record and lets go of it before
// the directory, so the record never outlives the lock it names. The file
// itself stays.
func (l *Lock) Release() error {
	if l == nil || l.d == nil {
		return nil
	}
	d, rec := l.d, l.rec
	l.d, l.rec = nil, nil
	return errors.Join(rec.Truncate(0), rec.Sync(), unlock(rec), rec.Close(), unlock(d), d.Close())
}

// recordWait bounds how long a new holder waits for the record's lock. Only
// a refusal reading the record holds it, and only for a moment.
const recordWait = 2 * time.Second

// record writes this process's Holder into the record file, durably, and
// keeps the file open with an exclusive lock on it for as long as the
// directory lock is held.
//
// That lock is what makes the record believable (reviews 445 and 446). A
// refusal names the holder only if the record it reads is locked, and only
// a live holder that wrote it can be holding that lock: the kernel drops it
// when the holder exits or is killed, zombie or not, on whatever host. A
// record left by a holder that is gone is unlocked, and so is one replaced
// under a live holder, since the lock is on the inode the holder wrote.
func record(path, purpose string) (*os.File, error) {
	f, err := openFile(path)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(recordWait)
	for {
		err = tryLock(f)
		if !errors.Is(err, errHeld) || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("locking the record: %w", err)
	}
	fail := func(err error) (*os.File, error) {
		unlock(f)
		f.Close()
		return nil, err
	}
	host, _ := os.Hostname()
	b, err := json.Marshal(Holder{
		PID: os.Getpid(), Host: host, Purpose: purpose,
		StartedAt: time.Now().UTC().Truncate(time.Second), Version: version(),
	})
	if err != nil {
		return fail(err)
	}
	if err := f.Truncate(0); err != nil {
		return fail(err)
	}
	if _, err := f.WriteAt(append(b, '\n'), 0); err != nil {
		return fail(err)
	}
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	return f, nil
}

// errUnheldRecord is a record no live holder has locked: left by an earlier
// holder, or not yet written by the current one.
var errUnheldRecord = errors.New("no running holder has locked it: it was left by an earlier holder, " +
	"and the process holding the lock now has not recorded itself yet")

// readHolder reads the record a refused Acquire names the holder from, and
// believes it only if a live holder has it locked (see record).
func readHolder(path string) (*Holder, error) {
	f, err := openRecord(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	switch err := tryLock(f); {
	case err == nil:
		unlock(f)
		return nil, fmt.Errorf("%s: %w", path, errUnheldRecord)
	case !errors.Is(err, errHeld):
		return nil, fmt.Errorf("checking %s: %w", path, err)
	}
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
	return &h, nil
}

// version is the module version the binary was built at, or "(devel)".
func version() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" {
		return bi.Main.Version
	}
	return "(devel)"
}
