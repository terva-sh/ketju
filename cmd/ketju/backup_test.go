package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/auth/oidc/oidctest"
	"github.com/terva-sh/ketju/internal/backup"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/lock"
	"github.com/terva-sh/ketju/internal/store/blob"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// The tests of `ketju backup` (TKT-01M29CXK35). docs/15's invariant is the
// first: a snapshot ketju backup makes passes ketju fsck after restore.

// backupConfig is writeConfig with a data directory whose control socket
// path fits a Unix socket. Under a long TMPDIR it moves to a short
// directory of its own.
func backupConfig(t *testing.T, extra string) (string, string) {
	t.Helper()
	path, data := writeConfig(t, extra)
	if _, err := backup.SocketPath(data); err == nil {
		return path, data
	}
	dir, err := os.MkdirTemp("/tmp", "kbk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	short := filepath.Join(dir, "data")
	body := strings.Replace(string(b), fmt.Sprintf("data_dir = %q", data), fmt.Sprintf("data_dir = %q", short), 1)
	path = filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path, short
}

// runBackupCmd runs ketju backup and returns its parsed --json report.
func runBackupCmd(t *testing.T, data, dest string) (backupSummary, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err := backupMain(t.Context(), &out, &errOut, []string{"--root", data, "--json", dest}, backupOpts{})
	var sum backupSummary
	if err == nil {
		if jerr := json.Unmarshal(out.Bytes(), &sum); jerr != nil {
			t.Fatalf("the report is not JSON: %v\n%s", jerr, out.String())
		}
		t.Logf("ketju backup: %+v", sum)
	}
	return sum, errOut.String(), err
}

// restore copies a backup into an empty data directory, as the README says.
func restore(t *testing.T, from string) string {
	t.Helper()
	to := filepath.Join(t.TempDir(), "restored")
	if err := os.CopyFS(to, os.DirFS(from)); err != nil {
		t.Fatal(err)
	}
	return to
}

// mustFsck runs ketju fsck on root and fails unless it exits 0.
func mustFsck(t *testing.T, root string) {
	t.Helper()
	var out bytes.Buffer
	if err := fsckMain(&out, []string{"--root", root}); err != nil {
		t.Fatalf("ketju fsck on the restored backup: %v (exit %d)\n%s", err, exitCodeFor(err), out.String())
	}
	if !strings.Contains(out.String(), "no corruption found") {
		t.Errorf("fsck output:\n%s", out.String())
	}
}

// logs reads every log under root.
func logs(t *testing.T, root string) map[threadid.ID][]byte {
	t.Helper()
	sizes, _, err := threadid.WalkLogs(root)
	if err != nil {
		t.Fatal(err)
	}
	out := map[threadid.ID][]byte{}
	for id := range sizes {
		b, err := os.ReadFile(id.LogPath(root))
		if err != nil {
			t.Fatal(err)
		}
		out[id] = b
	}
	return out
}

// handles is every member's handle in root's database.
func handles(t *testing.T, root string) []string {
	t.Helper()
	d, err := db.OpenReadOnly(t.Context(), filepath.Join(root, "db", "ketju.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var out []string
	if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(t.Context(), `SELECT handle FROM users ORDER BY id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var h string
			if err := rows.Scan(&h); err != nil {
				return err
			}
			out = append(out, h)
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// backupServe starts a serve with one member signed in, and returns it,
// their actor, the config path and the data directory.
func backupServe(t *testing.T, o serveOpts) (*running, *actor, string, string) {
	t.Helper()
	const handle, subject = "backer", "backer-sub"
	idp := oidctest.New(t)
	idp.Users = []string{subject}
	path, data := backupConfig(t, providerBlock(idp.URL))
	seedPeople(t, data, idp.URL, seeded{subject: subject, handle: handle, role: domain.RoleMember})
	o.idpClient, o.getenv = idp.Client(), siteURL
	r := start(t, path, o)
	a := newActor(r, handle)
	if res := signIn(t, a, idp, subject); res.status != http.StatusSeeOther {
		t.Fatalf("signing in answered %d:\n%s", res.status, res)
	}
	return r, a, path, data
}

// TestBackupRestoresAndPassesFsck is docs/15's invariant. Against a serve
// that takes posts throughout, ketju backup writes a backup that, copied
// into an empty data directory, passes ketju fsck with exit 0, and serves
// every thread it holds. Its logs are the source's up to its lengths, and
// its members are the source's.
func TestBackupRestoresAndPassesFsck(t *testing.T) {
	r, a, _, data := backupServe(t, serveOpts{})
	var ids []string
	for range 3 {
		ids = append(ids, newThread(t, a))
	}

	stop := make(chan struct{})
	var wrote atomic.Int32
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			var res answer
			if i%4 == 3 {
				res = a.do(t, http.MethodPost, "/api/v1/forums/general/threads",
					map[string]any{"title": fmt.Sprintf("More %d", i), "source": "x", "tags": []string{}})
			} else {
				res = a.do(t, http.MethodPost, "/api/v1/threads/"+ids[i%len(ids)]+"/posts",
					map[string]any{"source": fmt.Sprintf("reply %d", i)})
			}
			switch res.status {
			case http.StatusCreated:
				wrote.Add(1)
			case http.StatusTooManyRequests:
				time.Sleep(50 * time.Millisecond)
			default:
				t.Errorf("a write during the backup answered %d:\n%s", res.status, res)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	waitUntil(t, func() bool { return wrote.Load() >= 5 })
	dest := filepath.Join(t.TempDir(), "backup")
	sum, errOut, err := runBackupCmd(t, data, dest)
	if err != nil {
		t.Fatalf("ketju backup: %v (exit %d)\n%s", err, exitCodeFor(err), errOut)
	}
	before := wrote.Load()
	waitUntil(t, func() bool { return wrote.Load() >= before+5 })
	close(stop)
	wg.Wait()
	if err := r.shutdown(t); err != nil {
		t.Fatal(err)
	}

	if sum.Mode != "online" || sum.Threads < 3 || sum.CaughtUp != 0 {
		t.Errorf("report = %+v; want online, at least 3 threads, nothing for the copy's catch-up to do", sum)
	}
	got := restore(t, dest)
	mustFsck(t, got)

	src, copied := logs(t, data), logs(t, got)
	if len(copied) != sum.Threads {
		t.Errorf("the backup holds %d logs, the report says %d", len(copied), sum.Threads)
	}
	for id, b := range copied {
		if !bytes.HasPrefix(src[id], b) {
			t.Errorf("thread %s: the backup's log is not a prefix of the source's", id)
		}
	}
	if s, c := handles(t, data), handles(t, got); !slices.Equal(s, c) {
		t.Errorf("members: source %v, backup %v", s, c)
	}

	// It serves every thread it holds.
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, fmt.Appendf(nil, "listen = %q\ndata_dir = %q\n", freePort(t), got), 0o644); err != nil {
		t.Fatal(err)
	}
	rr := start(t, cfg, serveOpts{})
	for id := range copied {
		if code, body := get(t, "http://"+rr.addr+"/api/v1/threads/"+id.String()); code != http.StatusOK {
			t.Errorf("the restored serve answered %d for thread %s:\n%s", code, id, body)
		}
	}
	if err := rr.shutdown(t); err != nil {
		t.Fatal(err)
	}
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// fileBytes reads the database and every log under root, by relative path.
func fileBytes(t *testing.T, root string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if rel == filepath.Join("db", "ketju.db") || strings.HasSuffix(rel, threadid.LogExt) {
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			out[rel] = b
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestBackupOfAStoppedDataDirectory: with serve stopped and a log appended
// past its projection, as a crash leaves it, the backup holds those records
// and passes fsck after restore. The source's database and logs are
// byte-identical afterwards.
func TestBackupOfAStoppedDataDirectory(t *testing.T) {
	path, data := backupConfig(t, "")
	ids := seedThreads(t, data, 3)
	r := start(t, path, serveOpts{})
	if err := r.shutdown(t); err != nil {
		t.Fatal(err)
	}
	c, err := threadcache.New(data, threadcache.Options{Sync: threadlog.SyncNever})
	if err != nil {
		t.Fatal(err)
	}
	appendTo(t, c, ids[0], threadlog.Append{Type: threadlog.TypePost, Author: 1, TimeMS: 1_700_000_002_000,
		Body: &threadlog.PostBody{Source: "written after the projection"}})
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	var fsckOut bytes.Buffer
	if err := fsckMain(&fsckOut, []string{"--root", data}); exitCodeFor(err) != exitDrift {
		t.Fatalf("the source should show drift: %v\n%s", err, fsckOut.String())
	}
	before := fileBytes(t, data)

	dest := filepath.Join(t.TempDir(), "backup")
	sum, errOut, err := runBackupCmd(t, data, dest)
	if err != nil {
		t.Fatalf("ketju backup: %v\n%s", err, errOut)
	}
	if sum.Mode != "offline" || sum.Threads != 3 || sum.CaughtUp != 1 {
		t.Errorf("report = %+v; want offline, 3 threads, 1 caught up", sum)
	}
	after := fileBytes(t, data)
	if len(after) != len(before) {
		t.Errorf("the source had %d files and has %d", len(before), len(after))
	}
	for rel, b := range before {
		if !bytes.Equal(after[rel], b) {
			t.Errorf("the backup wrote the source's %s", rel)
		}
	}
	got := restore(t, dest)
	mustFsck(t, got)
	src, copied := logs(t, data), logs(t, got)
	for _, id := range ids {
		if !bytes.Equal(src[id], copied[id]) {
			t.Errorf("thread %s: the backup's log differs from the stopped source's", id)
		}
	}
}

// TestBackupLeavesAnotherSchemaAlone: a database at an older or a newer
// schema than this binary's is copied at it, neither migrated nor caught up,
// so the binary that wrote it can restore it; the source's drift is reported
// rather than failing the backup. This is the backup an upgrade takes.
//
// Each schema differs in its tables, not only in its migration record: the
// older has no post_attachments, as before 0020, and the newer has renamed
// what the projection and attachment checks read. A check that cannot read
// the other schema is reported as not checked, and the copy is kept (Terva
// review 1987).
func TestBackupLeavesAnotherSchemaAlone(t *testing.T) {
	for _, tc := range []struct {
		schema    string
		change    []string
		recorded  func(n int) int
		drift     int
		unchecked int
		fsck      int
	}{
		{schemaOlder, []string{
			`DELETE FROM schema_migrations WHERE version = (SELECT max(version) FROM schema_migrations)`,
			`DROP TABLE post_attachments`,
		}, func(n int) int { return n - 1 }, 1, 1, exitDrift},
		{schemaNewer, []string{
			`INSERT INTO schema_migrations (version, name, checksum, created_at)
			SELECT max(version) + 1, 'from_a_newer_binary', 'x', 1 FROM schema_migrations`,
			`ALTER TABLE projection_state RENAME COLUMN log_length TO projected_length`,
			`ALTER TABLE post_attachments RENAME COLUMN hash TO blob_hash`,
		}, func(n int) int { return n + 1 }, 0, 2, -1},
	} {
		t.Run(tc.schema, func(t *testing.T) {
			path, data := backupConfig(t, "")
			ids := seedThreads(t, data, 2)
			r := start(t, path, serveOpts{})
			if err := r.shutdown(t); err != nil {
				t.Fatal(err)
			}
			c, err := threadcache.New(data, threadcache.Options{Sync: threadlog.SyncNever})
			if err != nil {
				t.Fatal(err)
			}
			appendTo(t, c, ids[0], threadlog.Append{Type: threadlog.TypePost, Author: 1, TimeMS: 1_700_000_002_000,
				Body: &threadlog.PostBody{Source: "written after the projection"}})
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			d := openDB(t, data)
			n := migrationsRecorded(t, d)
			for _, q := range tc.change {
				execDB(t, d, q)
			}
			d.Close()

			dest := filepath.Join(t.TempDir(), "backup")
			sum, errOut, err := runBackupCmd(t, data, dest)
			if err != nil {
				t.Fatalf("ketju backup: %v\n%s", err, errOut)
			}
			if sum.Schema != tc.schema || sum.CaughtUp != 0 || sum.Drift != tc.drift || len(sum.Unchecked) != tc.unchecked {
				t.Errorf("report = %+v; want schema %s, nothing caught up, %d thread(s) of drift, %d check(s) not made",
					sum, tc.schema, tc.drift, tc.unchecked)
			}
			got := restore(t, dest)
			cd, err := db.OpenReadOnly(t.Context(), adminDBPath(got))
			if err != nil {
				t.Fatal(err)
			}
			defer cd.Close()
			if m := migrationsRecorded(t, cd); m != tc.recorded(n) {
				t.Errorf("the copy records %d migrations, want the source's %d", m, tc.recorded(n))
			}
			// What fsck says of the source, it says of the copy: drift
			// only, which serve closes when it next starts, where this
			// binary can read the projection at all.
			if tc.fsck >= 0 {
				var out bytes.Buffer
				if err := fsckMain(&out, []string{"--root", got}); exitCodeFor(err) != tc.fsck {
					t.Errorf("fsck of the restored copy: %v, want exit %d\n%s", err, tc.fsck, out.String())
				}
			}
		})
	}
}

func migrationsRecorded(t *testing.T, d *db.DB) int {
	t.Helper()
	var n int
	if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT count(*) FROM schema_migrations`).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestBackupPausesAppendsOnlyForTheSnapshot: an append started while the
// snapshot holds appends returns only after the release, and succeeds; a
// thread page read meanwhile is served. A snapshot that outlasts its cap
// fails, releases the appends, and leaves no staging directory.
func TestBackupPausesAppendsOnlyForTheSnapshot(t *testing.T) {
	holding := make(chan struct{}, 1)
	var release atomic.Pointer[chan struct{}]
	var waitCap atomic.Bool
	r, a, _, data := backupServe(t, serveOpts{pauseCap: 3 * time.Second, snapshotHold: func(ctx context.Context) {
		holding <- struct{}{}
		if waitCap.Load() {
			<-ctx.Done()
			return
		}
		select {
		case <-*release.Load():
		case <-ctx.Done():
		}
	}})
	id := newThread(t, a)
	reader := newActor(r, "reader")

	rel := make(chan struct{})
	release.Store(&rel)
	backedUp := make(chan error, 1)
	dest := filepath.Join(t.TempDir(), "backup")
	go func() {
		_, _, err := runBackupCmd(t, data, dest)
		backedUp <- err
	}()
	<-holding
	replied := make(chan answer, 1)
	go func() {
		replied <- a.do(t, http.MethodPost, "/api/v1/threads/"+id+"/posts", map[string]any{"source": "held"})
	}()
	if res := reader.do(t, http.MethodGet, "/api/v1/threads/"+id, nil); res.status != http.StatusOK {
		t.Errorf("a read during the pause answered %d", res.status)
	}
	select {
	case res := <-replied:
		t.Fatalf("an append returned during the pause: %d", res.status)
	case <-time.After(300 * time.Millisecond):
	}
	close(rel)
	if res := <-replied; res.status != http.StatusCreated {
		t.Errorf("the held append answered %d after the release:\n%s", res.status, res)
	}
	if err := <-backedUp; err != nil {
		t.Fatalf("ketju backup: %v", err)
	}

	// Past the cap.
	waitCap.Store(true)
	capped := filepath.Join(t.TempDir(), "capped")
	go func() {
		_, _, err := runBackupCmd(t, data, capped)
		backedUp <- err
	}()
	<-holding
	err := <-backedUp
	if err == nil || exitCodeFor(err) != exitInternal || !strings.Contains(err.Error(), "refused") {
		t.Errorf("a snapshot past its cap = %v (exit %d), want a refusal, exit 4", err, exitCodeFor(err))
	}
	if left := besides(t, capped); len(left) != 0 {
		t.Errorf("a failed backup left %v", left)
	}
	es, _ := os.ReadDir(data)
	for _, e := range es {
		if strings.HasPrefix(e.Name(), backup.StagingPrefix) {
			t.Errorf("a failed snapshot left %s", e.Name())
		}
	}
	start := time.Now()
	if res := a.do(t, http.MethodPost, "/api/v1/threads/"+id+"/posts", map[string]any{"source": "after"}); res.status != http.StatusCreated {
		t.Errorf("an append after a failed snapshot answered %d", res.status)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("an append after a failed snapshot took %s", took)
	}
}

// besides lists what is beside dest that starts with its name.
func besides(t *testing.T, dest string) []string {
	t.Helper()
	es, err := os.ReadDir(filepath.Dir(dest))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		if strings.HasPrefix(e.Name(), filepath.Base(dest)) {
			out = append(out, e.Name())
		}
	}
	return out
}

// TestBackupRefuses: DEST exists (exit 2); the lock is held by reindex
// (exit 4, naming it); a redaction is pending (exit 4); serve holds the lock
// but its socket is gone (exit 4); DEST is inside the data directory (exit
// 2). Nothing is left beside DEST.
func TestBackupRefuses(t *testing.T) {
	path, data := backupConfig(t, "")
	seedThreads(t, data, 1)
	r := start(t, path, serveOpts{})
	if err := r.shutdown(t); err != nil {
		t.Fatal(err)
	}
	check := func(name string, wantExit int, wantText string) {
		t.Helper()
		dest := filepath.Join(t.TempDir(), "backup")
		if name == "dest exists" {
			if err := os.Mkdir(dest, 0o750); err != nil {
				t.Fatal(err)
			}
		}
		_, _, err := runBackupCmd(t, data, dest)
		if code := exitCodeFor(err); code != wantExit || !strings.Contains(fmt.Sprint(err), wantText) {
			t.Errorf("%s: exit %d, %v; want exit %d naming %q", name, code, err, wantExit, wantText)
		}
		left := besides(t, dest)
		if name == "dest exists" {
			left = slices.DeleteFunc(left, func(s string) bool { return s == "backup" })
		}
		if len(left) != 0 {
			t.Errorf("%s: left %v", name, left)
		}
	}
	check("dest exists", exitUsage, "already exists")

	lk, err := lock.Acquire(data, "reindex")
	if err != nil {
		t.Fatal(err)
	}
	check("held by reindex", exitInternal, "ketju reindex")
	lk.Release()

	if err := os.WriteFile(journalPath(data), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	check("redaction pending", exitInternal, "redaction is pending")
	os.Remove(journalPath(data))

	// A redaction that starts after the first check and stops partway,
	// releasing the lock, is caught under the lock (Terva review 1983).
	raced := filepath.Join(t.TempDir(), "backup")
	var out, errOut bytes.Buffer
	err = backupMain(t.Context(), &out, &errOut, []string{"--root", data, raced}, backupOpts{beforeLock: func() {
		if err := os.WriteFile(journalPath(data), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}})
	if exitCodeFor(err) != exitInternal || !strings.Contains(fmt.Sprint(err), "redaction is pending") {
		t.Errorf("a redaction pending by the time of the lock: exit %d, %v", exitCodeFor(err), err)
	}
	if left := besides(t, raced); len(left) != 0 {
		t.Errorf("a redaction pending by the time of the lock: left %v", left)
	}
	os.Remove(journalPath(data))

	lk, err = lock.Acquire(data, "serve")
	if err != nil {
		t.Fatal(err)
	}
	check("no socket", exitInternal, "did not answer")
	lk.Release()

	if _, _, err := runBackupCmd(t, t.TempDir(), filepath.Join(t.TempDir(), "b")); exitCodeFor(err) != exitUsage {
		t.Errorf("a directory with no threads/: %v", err)
	}

	// DEST inside the data directory, directly or through a symlink to it,
	// is refused before anything is written: under blobs/ the copy would
	// walk itself (Terva review 1978).
	link := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(filepath.Join(data, "blobs"), link); err != nil {
		t.Fatal(err)
	}
	for _, dest := range []string{
		filepath.Join(data, "blobs", "backup"),
		filepath.Join(data, "backup"),
		filepath.Join(link, "backup"),
	} {
		_, _, err := runBackupCmd(t, data, dest)
		if exitCodeFor(err) != exitUsage || !strings.Contains(fmt.Sprint(err), "inside the data directory") {
			t.Errorf("DEST %s: exit %d, %v; want a usage error", dest, exitCodeFor(err), err)
		}
		if left := besides(t, dest); len(left) != 0 {
			t.Errorf("DEST %s: left %v", dest, left)
		}
	}
}

// TestBackupLeavesOutIndexesThumbnailsStagingAndSecrets: the copy has no
// thread index, thumbnail, staged upload, secrets/ or config.toml, and holds
// every blob.
func TestBackupLeavesOutIndexesThumbnailsStagingAndSecrets(t *testing.T) {
	path, data := backupConfig(t, "")
	ids := seedThreads(t, data, 2)
	r := start(t, path, serveOpts{})
	if err := r.shutdown(t); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ids[0].IndexPath(data)); err != nil {
		t.Fatalf("the source has no index to leave out: %v", err)
	}
	hash := strings.Repeat("ab", 32)
	for _, p := range []string{
		filepath.Join("blobs", "sha256", "ab", hash),
		filepath.Join("blobs", "sha256", "ab", hash+".thumb"),
		filepath.Join("blobs", "tmp", "upload-1"),
		filepath.Join("secrets", "cookie.key"),
		"config.toml",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(data, p)), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(data, p), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	dest := filepath.Join(t.TempDir(), "backup")
	sum, errOut, err := runBackupCmd(t, data, dest)
	if err != nil {
		t.Fatalf("ketju backup: %v\n%s", err, errOut)
	}
	if sum.Blobs != 1 {
		t.Errorf("blobs = %d, want 1", sum.Blobs)
	}
	var found []string
	err = filepath.WalkDir(dest, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dest, path)
		found = append(found, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range found {
		if strings.HasSuffix(f, threadid.IndexExt) || strings.HasSuffix(f, ".thumb") ||
			strings.HasPrefix(f, filepath.Join("blobs", "tmp")) || strings.HasPrefix(f, "secrets") ||
			f == "config.toml" || f == lock.FileName || strings.HasSuffix(f, "-wal") || strings.HasSuffix(f, "-shm") {
			t.Errorf("the backup holds %s", f)
		}
	}
	if !slices.Contains(found, filepath.Join("blobs", "sha256", "ab", hash)) {
		t.Errorf("the backup lacks the blob:\n%v", found)
	}
	mustFsck(t, restore(t, dest))
}

// TestBackupChecksTheAttachmentsPostsName: a backup holds every attachment
// its posts name that the source has. One the source lacks too is reported
// and does not fail the backup; one the source has and the copy lacks fails
// verification (Terva review 1982).
func TestBackupChecksTheAttachmentsPostsName(t *testing.T) {
	path, data := backupConfig(t, "")
	ids := seedThreads(t, data, 1)
	r := start(t, path, serveOpts{})
	if err := r.shutdown(t); err != nil {
		t.Fatal(err)
	}
	have, gone := strings.Repeat("ab", 32), strings.Repeat("cd", 32)
	c, err := threadcache.New(data, threadcache.Options{Sync: threadlog.SyncNever})
	if err != nil {
		t.Fatal(err)
	}
	appendTo(t, c, ids[0], threadlog.Append{Type: threadlog.TypePost, Author: 1, TimeMS: 1_700_000_002_000,
		Body: &threadlog.PostBody{Source: "two attachments", Attachments: []string{have, gone}}})
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	p, err := blob.Path(data, have)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("attachment"), 0o600); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "backup")
	sum, errOut, err := runBackupCmd(t, data, dest)
	if err != nil {
		t.Fatalf("ketju backup: %v\n%s", err, errOut)
	}
	if sum.Blobs != 1 || sum.MissingBlobs != 1 {
		t.Errorf("report = %+v; want 1 blob copied and 1 missing from the source", sum)
	}
	copied, err := blob.Path(dest, have)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(copied); err != nil || string(b) != "attachment" {
		t.Errorf("the backup's copy of the attachment: %q, %v", b, err)
	}

	// The copy losing one the source has is what verification catches.
	if err := os.Remove(copied); err != nil {
		t.Fatal(err)
	}
	lost, absent, err := checkBlobs(t.Context(), data, dest)
	if err != nil || !slices.Equal(lost, []string{have}) || !slices.Equal(absent, []string{gone}) {
		t.Errorf("checkBlobs = lost %v, absent %v, %v; want lost %s, absent %s", lost, absent, err, have, gone)
	}
}

// TestServeRemovesStaleBackupStaging: a socket file and a staging directory
// a stopped process left are gone once serve has started, under the lock,
// and the socket is serve's own.
func TestServeRemovesStaleBackupStaging(t *testing.T) {
	path, data := backupConfig(t, "")
	if err := os.MkdirAll(filepath.Join(data, backup.StagingPrefix+"old"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, backup.SocketName), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	r := start(t, path, serveOpts{})
	defer r.shutdown(t)
	if _, err := os.Stat(filepath.Join(data, backup.StagingPrefix+"old")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the stale staging directory is still there: %v", err)
	}
	fi, err := os.Stat(filepath.Join(data, backup.SocketName))
	if err != nil || fi.Mode()&os.ModeSocket == 0 || fi.Mode().Perm() != 0o600 {
		t.Errorf("the control socket is %v, %v; want serve's 0600 socket", fi, err)
	}
}
