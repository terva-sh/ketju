package backup

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/service/posts"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbtest"
)

// shortDir is a data directory with a path short enough for a socket:
// t.TempDir under a long TMPDIR can pass the limit.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "kb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if _, err := SocketPath(dir); err != nil {
		dir, err = os.MkdirTemp("/tmp", "kb")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(dir) })
	}
	return dir
}

func deps(t *testing.T, dir string, w *posts.Writer) Deps {
	t.Helper()
	return Deps{DataDir: dir, DB: dbtest.Open(t, db.Options{}), Hold: w.Hold,
		CatchUp: func(context.Context) error { return nil }}
}

func stagingDirs(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		if strings.HasPrefix(e.Name(), StagingPrefix) {
			out = append(out, e.Name())
		}
	}
	return out
}

// TestSnapshotWritesAStagedDatabase: a snapshot holds appends while it runs,
// releases them, and leaves its database in a staging directory.
func TestSnapshotWritesAStagedDatabase(t *testing.T) {
	dir := t.TempDir()
	var w posts.Writer
	d := deps(t, dir, &w)
	var heldDuring atomic.Bool
	d.WhileHeld = func(ctx context.Context) {
		// A second Hold fails while this one is in force.
		_, err := w.Hold(ctx)
		heldDuring.Store(errors.Is(err, posts.ErrHeld))
	}
	res, err := Snapshot(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if !heldDuring.Load() {
		t.Error("appends were not held during the snapshot")
	}
	if _, err := os.Stat(filepath.Join(dir, res.Staging)); err != nil {
		t.Fatalf("the staged database: %v", err)
	}
	release, err := w.Hold(context.Background())
	if err != nil {
		t.Fatalf("appends are still held after the snapshot: %v", err)
	}
	release()
}

// TestSnapshotPastItsCapReleases: a snapshot whose steps outlast the cap
// fails, releases the appends, and leaves no staging directory.
func TestSnapshotPastItsCapReleases(t *testing.T) {
	dir := t.TempDir()
	var w posts.Writer
	d := deps(t, dir, &w)
	d.Cap = 50 * time.Millisecond
	d.CatchUp = func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}
	if _, err := Snapshot(context.Background(), d); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Snapshot past its cap = %v", err)
	}
	if s := stagingDirs(t, dir); len(s) != 0 {
		t.Errorf("a failed snapshot left %v", s)
	}
	release, err := w.Hold(context.Background())
	if err != nil {
		t.Fatalf("appends are still held after a failed snapshot: %v", err)
	}
	release()
}

// TestBackupSocketAnswersOnlyItsOwner: the socket is mode 0600; a peer with
// another uid is answered "forbidden" and nothing is held; a second request
// during a snapshot is answered "busy"; an unknown version is refused.
func TestBackupSocketAnswersOnlyItsOwner(t *testing.T) {
	dir := shortDir(t)
	var snaps atomic.Int32
	gate := make(chan struct{})
	started := make(chan struct{}, 1)
	peer := atomic.Int64{}
	peer.Store(int64(os.Getuid()))
	s, err := Listen(dir, Options{
		Snapshot: func(ctx context.Context) (Result, error) {
			snaps.Add(1)
			started <- struct{}{}
			<-gate
			return Result{Staging: "backup.staging-x/ketju.db", Paused: time.Millisecond}, nil
		},
		PeerUID: func(*net.UnixConn) (int, error) { return int(peer.Load()), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(time.Second)
	fi, err := os.Stat(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 || fi.Mode()&os.ModeSocket == 0 {
		t.Errorf("the socket is %v, want a 0600 socket", fi.Mode())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Another user.
	peer.Store(int64(os.Getuid() + 1))
	a, err := Ask(ctx, s.Path())
	if err != nil || a.OK || a.Error != ErrForbidden {
		t.Errorf("another uid was answered %+v, %v; want forbidden", a, err)
	}
	if n := snaps.Load(); n != 0 {
		t.Errorf("another uid's request took %d snapshot(s)", n)
	}
	peer.Store(int64(os.Getuid()))

	// One at a time.
	first := make(chan Answer, 1)
	go func() {
		a, err := Ask(ctx, s.Path())
		if err != nil {
			t.Error(err)
		}
		first <- a
	}()
	<-started
	if a, err := Ask(ctx, s.Path()); err != nil || a.Error != ErrBusy {
		t.Errorf("a second request during a snapshot = %+v, %v; want busy", a, err)
	}
	close(gate)
	if a := <-first; !a.OK || a.Staging != "backup.staging-x/ketju.db" || a.PausedMS != 1 {
		t.Errorf("the snapshot answered %+v", a)
	}

	// Another version.
	c, err := net.Dial("unix", s.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Write([]byte(`{"v":2,"op":"snapshot"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 256)
	n, _ := c.Read(buf)
	if !strings.Contains(string(buf[:n]), ErrBadVersion) {
		t.Errorf("version 2 was answered %s", buf[:n])
	}
	if n := snaps.Load(); n != 1 {
		t.Errorf("%d snapshots taken, want 1", n)
	}
}

// TestRemoveStale: a socket and staging directories a stopped process left
// are removed, and nothing else is.
func TestRemoveStale(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{SocketName, StagingPrefix + "a/ketju.db", "threads/keep"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := RemoveStale(dir); err != nil {
		t.Fatal(err)
	}
	if s := stagingDirs(t, dir); len(s) != 0 {
		t.Errorf("left %v", s)
	}
	if _, err := os.Stat(filepath.Join(dir, SocketName)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the socket is still there: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "threads/keep")); err != nil {
		t.Errorf("RemoveStale removed something else: %v", err)
	}
}

// TestSocketPathLimit: a data directory whose socket path is past the limit
// is refused rather than bound.
func TestSocketPathLimit(t *testing.T) {
	long := "/" + strings.Repeat("d", 120)
	if _, err := SocketPath(long); !errors.Is(err, ErrPathTooLong) {
		t.Errorf("SocketPath(%d bytes) = %v", len(long), err)
	}
	if _, err := Listen(long, Options{PeerUID: func(*net.UnixConn) (int, error) { return 0, nil }}); !errors.Is(err, ErrPathTooLong) {
		t.Errorf("Listen on a long path = %v", err)
	}
}
