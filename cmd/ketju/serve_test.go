package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/lock"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// The helper process: this test binary run again as `ketju serve`, for the
// tests that need a real signal and a real second process. It mounts
// /test/slow, which answers after 300 ms, and prints the address once ready.
//
// With KETJU_AS_MAIN set it is the binary itself, main and all, for a test
// that runs commands as an operator types them (TestREADMERecoveryProcedure).
func TestMain(m *testing.M) {
	if os.Getenv("KETJU_AS_MAIN") != "" {
		main()
		os.Exit(0)
	}
	if path := os.Getenv("KETJU_SERVE_HELPER"); path != "" {
		os.Exit(serveHelper(path))
	}
	os.Exit(m.Run())
}

func serveHelper(path string) int {
	ctx, stop := signalContext()
	defer stop()
	err := serve(ctx, path, serveOpts{
		ready: func(addr string) { fmt.Println(addr) },
		wrap: func(h http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/test/slow" {
					time.Sleep(300 * time.Millisecond)
					io.WriteString(w, "done")
					return
				}
				h.ServeHTTP(w, r)
			})
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ketju: %v\n", err)
	}
	return exitCodeFor(err)
}

// buffer is a log sink a test can read while serve writes to it.
type buffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *buffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// freePort is a port nothing is listening on a moment ago. Config refuses
// port 0, so the test cannot ask the kernel to choose at listen time.
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

// writeConfig writes a config for a data directory in a temp dir, listening
// on a free port, and returns its path and the data directory.
func writeConfig(t *testing.T, extra string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	data := filepath.Join(dir, "data")
	path := filepath.Join(dir, "config.toml")
	body := fmt.Sprintf("listen = %q\ndata_dir = %q\n[storage]\nfsync = \"never\"\n%s", freePort(t), data, extra)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path, data
}

// running is a serve started in this process.
type running struct {
	addr  string
	steps func() []string
	log   *buffer
	stop  context.CancelFunc
	done  chan error    // serve's result, read once
	exit  chan struct{} // closed when serve has returned
	cache *threadcache.Cache
}

func start(t *testing.T, path string, o serveOpts) *running {
	t.Helper()
	r := &running{log: &buffer{}, done: make(chan error, 1), exit: make(chan struct{})}
	var mu sync.Mutex
	var steps []string
	o.step = func(name string) { mu.Lock(); steps = append(steps, name); mu.Unlock() }
	r.steps = func() []string { mu.Lock(); defer mu.Unlock(); return slices.Clone(steps) }
	ready := make(chan string, 1)
	o.ready = func(addr string) { ready <- addr }
	o.logOut = r.log
	o.threads = func(c *threadcache.Cache) { r.cache = c }
	ctx, cancel := context.WithCancel(context.Background())
	r.stop = cancel
	go func() { r.done <- serve(ctx, path, o); close(r.exit) }()
	select {
	case r.addr = <-ready:
	case err := <-r.done:
		cancel()
		t.Fatalf("serve returned before it was ready: %v\n%s", err, r.log)
	case <-time.After(20 * time.Second):
		cancel()
		t.Fatalf("serve was not ready in time\n%s", r.log)
	}
	t.Cleanup(func() {
		cancel()
		select {
		case <-r.exit:
		case <-time.After(20 * time.Second):
		}
	})
	return r
}

func (r *running) shutdown(t *testing.T) error {
	t.Helper()
	r.stop()
	select {
	case err := <-r.done:
		return err
	case <-time.After(30 * time.Second):
		t.Fatalf("serve did not stop\n%s", r.log)
		return nil
	}
}

// seedThreads writes n threads of a few posts into the data directory with a
// cache of its own, closed cleanly, and returns their ids. The database knows
// nothing of them, so the next serve has a gap to catch up.
func seedThreads(t *testing.T, data string, n int) []threadid.ID {
	t.Helper()
	c, err := threadcache.New(data, threadcache.Options{Sync: threadlog.SyncNever})
	if err != nil {
		t.Fatal(err)
	}
	var ids []threadid.ID
	for i := range n {
		id := threadid.ID(uint64(1_700_000_000_000+int64(i))<<16 | 1)
		th, err := c.Create(id, 1, 1_700_000_000_000)
		if err != nil {
			t.Fatal(err)
		}
		c.Release(th)
		appendTo(t, c, id, threadlog.Append{Type: threadlog.TypeThreadCreated, Author: 1, TimeMS: 1_700_000_000_000,
			Body: &threadlog.ThreadCreatedBody{Title: fmt.Sprintf("T%d", i), Slug: fmt.Sprintf("t%d", i), ForumID: 1}})
		appendTo(t, c, id, threadlog.Append{Type: threadlog.TypePost, Author: 1, TimeMS: 1_700_000_001_000,
			Body: &threadlog.PostBody{Source: "hello"}})
		ids = append(ids, id)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	return ids
}

func appendTo(t *testing.T, c *threadcache.Cache, id threadid.ID, a threadlog.Append) {
	t.Helper()
	err := c.With(id, func(th *threadcache.Thread) error {
		_, err := th.Actor.Append(a)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// reopenAll opens every thread with a fresh cache and says whether each
// index was trusted as it stood, with nothing replayed.
func reopenAll(t *testing.T, data string, ids []threadid.ID) {
	t.Helper()
	c, err := threadcache.New(data, threadcache.Options{Sync: threadlog.SyncNever})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, id := range ids {
		err := c.With(id, func(th *threadcache.Thread) error {
			if th.Outcome != threadlog.Trusted || th.Replayed != 0 {
				t.Errorf("thread %s reopened %v with %d bytes replayed; its index was not flushed", id, th.Outcome, th.Replayed)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// TestStartupOrderAndShutdown is AC3's order: config, the lock, the
// database, the stores, the subscribers, catch-up, then the listener; and
// shutdown in reverse, with the actors closed before the database.
func TestStartupOrderAndShutdown(t *testing.T) {
	path, _ := writeConfig(t, "")
	r := start(t, path, serveOpts{})

	if code, body := get(t, "http://"+r.addr+"/api/v1/me"); code != http.StatusOK {
		t.Errorf("GET /api/v1/me = %d: %s", code, body)
	}
	if code, body := get(t, "http://"+r.addr+"/"); code != http.StatusOK || !strings.Contains(body, "<html") {
		t.Errorf("GET / = %d, want the shell", code)
	}
	// The probe has run before the listener opened, so the first poll has
	// an answer (TKT-01M29CCBZ8).
	if code, body := get(t, "http://"+r.addr+"/healthz"); code != http.StatusOK || !strings.Contains(body, `"status":"ok"`) {
		t.Errorf("GET /healthz = %d: %s", code, body)
	}
	if err := r.shutdown(t); err != nil {
		t.Fatalf("shutdown: %v\n%s", err, r.log)
	}
	want := []string{"config", "lock", "database", "stores", "subscribers", "catch-up", "sign-in", "listener",
		"drain", "close bus", "stop projection", "close threads", "close database", "release lock"}
	if got := r.steps(); !slices.Equal(got, want) {
		t.Errorf("steps = %v\nwant    %v", got, want)
	}
	for _, line := range []string{"ketju started", "requests drained", "ketju stopped"} {
		if !strings.Contains(r.log.String(), line) {
			t.Errorf("the log has no %q line:\n%s", line, r.log)
		}
	}
}

// TestServeWaitsForCatchUp carries C5Y1S's AC3: a gap between the logs and
// the projection is closed before the listener opens. The logs are written
// with the database knowing nothing of them; when ready fires, every
// thread's projection_state already equals its log's size.
func TestServeWaitsForCatchUp(t *testing.T) {
	path, data := writeConfig(t, "")
	// A first start creates the database; the threads are then written
	// behind its back, which is the gap a crash leaves.
	r := start(t, path, serveOpts{})
	if err := r.shutdown(t); err != nil {
		t.Fatal(err)
	}
	ids := seedThreads(t, data, 3)

	// The projection is read while serve is still running: ready has fired,
	// so this is what the first request would have been served from.
	r2 := start(t, path, serveOpts{})
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(data, "db", "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	got := map[threadid.ID]int64{}
	err = d.Read(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		rows, err := q.ListProjectionLengths(t.Context())
		for _, row := range rows {
			got[threadid.ID(row.ThreadID)] = row.LogLength
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		fi, err := os.Stat(id.LogPath(data))
		if err != nil {
			t.Fatal(err)
		}
		if got[id] != fi.Size() {
			t.Errorf("when the listener opened, thread %s was projected to %d of %d", id, got[id], fi.Size())
		}
	}
	steps := r2.steps()
	if i, j := slices.Index(steps, "catch-up"), slices.Index(steps, "listener"); i < 0 || j < i {
		t.Errorf("catch-up did not come before the listener: %v", steps)
	}
	if !strings.Contains(r2.log.String(), "caught_up=3") && !strings.Contains(r2.log.String(), `"caught_up":3`) {
		t.Errorf("the startup line does not say three threads were caught up:\n%s", r2.log)
	}
}

// TestShutdownFlushesEveryIndex is AC2: threads appended to while serve runs
// are reopened afterwards with their indexes trusted and nothing replayed,
// because shutdown closed every actor.
func TestShutdownFlushesEveryIndex(t *testing.T) {
	path, data := writeConfig(t, "")
	ids := seedThreads(t, data, 4)
	r := start(t, path, serveOpts{})
	for _, id := range ids {
		for range 3 {
			appendTo(t, r.cache, id, threadlog.Append{Type: threadlog.TypePost, Author: 1, TimeMS: 1_700_000_002_000,
				Body: &threadlog.PostBody{Source: "more"}})
		}
	}
	if err := r.shutdown(t); err != nil {
		t.Fatalf("shutdown: %v\n%s", err, r.log)
	}
	// The actors write their header after every commit, so an in-process
	// reopen trusts it either way; what shutdown adds is closing them, which
	// fsyncs the index for a power loss no test here can stage. The cache
	// refusing work is how a test sees that it was closed.
	if err := r.cache.With(ids[0], func(*threadcache.Thread) error { return nil }); !errors.Is(err, threadcache.ErrClosed) {
		t.Errorf("after shutdown the thread cache answered %v, want ErrClosed: the actors were not closed", err)
	}
	reopenAll(t, data, ids)
}

// TestDrainTimeoutStillFlushes is AC4: a request that outlasts the drain is
// cut, and shutdown still flushes the actors and releases the lock.
func TestDrainTimeoutStillFlushes(t *testing.T) {
	path, data := writeConfig(t, "")
	ids := seedThreads(t, data, 2)
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	r := start(t, path, serveOpts{
		drain: 100 * time.Millisecond,
		wrap: func(h http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/test/block" {
					close(entered)
					select {
					case <-release:
					case <-req.Context().Done():
					}
					return
				}
				h.ServeHTTP(w, req)
			})
		},
	})
	for _, id := range ids {
		appendTo(t, r.cache, id, threadlog.Append{Type: threadlog.TypePost, Author: 1, TimeMS: 1_700_000_002_000,
			Body: &threadlog.PostBody{Source: "more"}})
	}
	cut := make(chan struct{})
	go func() {
		defer close(cut)
		if resp, err := http.Get("http://" + r.addr + "/test/block"); err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	}()
	<-entered

	began := time.Now()
	if err := r.shutdown(t); err != nil {
		t.Fatalf("shutdown: %v\n%s", err, r.log)
	}
	if took := time.Since(began); took > 5*time.Second {
		t.Errorf("shutdown took %s with a 100ms drain", took)
	}
	select {
	case <-cut:
	case <-time.After(5 * time.Second):
		t.Error("the request still running after the drain was not cut")
	}
	if !strings.Contains(r.log.String(), "requests still running after the drain") {
		t.Errorf("the log does not say the drain ran out:\n%s", r.log)
	}
	steps := r.steps()
	for _, s := range []string{"close threads", "close database", "release lock"} {
		if !slices.Contains(steps, s) {
			t.Errorf("shutdown skipped %q after the drain ran out: %v", s, steps)
		}
	}
	reopenAll(t, data, ids)
	l, err := lock.Acquire(data, "test")
	if err != nil {
		t.Fatalf("the lock was not released: %v", err)
	}
	l.Release()
}

// TestStartupFailures is AC3's other half: each step that can fail exits
// non-zero with a message naming it, never reaches the listener, and closes
// what it had opened, the lock included.
func TestStartupFailures(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(t *testing.T) string
		want  string
	}{
		{"no config", func(t *testing.T) string { return filepath.Join(t.TempDir(), "absent.toml") },
			"loading the config"},
		{"a bad config", func(t *testing.T) string {
			path, _ := writeConfig(t, "[log]\nlevel = \"loud\"\n")
			return path
		}, "loading the config"},
		{"the lock held", func(t *testing.T) string {
			path, data := writeConfig(t, "")
			if err := os.MkdirAll(data, 0o750); err != nil {
				t.Fatal(err)
			}
			l, err := lock.Acquire(data, "serve")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { l.Release() })
			return path
		}, fmt.Sprintf("locking the data directory: data directory %%s is in use by ketju serve (pid %d", os.Getpid())},
		{"a database newer than the binary", func(t *testing.T) string {
			path, data := writeConfig(t, "")
			d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(data, "db", "ketju.db")})
			if err != nil {
				t.Fatal(err)
			}
			if err := d.Migrate(t.Context()); err != nil {
				t.Fatal(err)
			}
			err = d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
				_, err := tx.Exec(`INSERT INTO schema_migrations (version, name, checksum, created_at) VALUES (9999, 'future', 'x', 1)`)
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			d.Close()
			return path
		}, "migrating the database"},
		{"the address in use", func(t *testing.T) string {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { ln.Close() })
			path, _ := writeConfig(t, "")
			b, _ := os.ReadFile(path)
			first := strings.SplitN(string(b), "\n", 2)
			b = []byte(fmt.Sprintf("listen = %q\n%s", ln.Addr().String(), first[1]))
			if err := os.WriteFile(path, b, 0o644); err != nil {
				t.Fatal(err)
			}
			return path
		}, "listening on 127.0.0.1:"},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := c.setup(t)
			readied := false
			var steps []string
			err := serve(t.Context(), path, serveOpts{
				logOut: io.Discard,
				ready:  func(string) { readied = true },
				step:   func(s string) { steps = append(steps, s) },
			})
			if err == nil {
				t.Fatal("serve started")
			}
			if exitCodeFor(err) == exitOK {
				t.Errorf("exit code 0 for %v", err)
			}
			want := c.want
			if strings.Contains(want, "%s") {
				b, _ := os.ReadFile(path)
				data := strings.Split(strings.Split(string(b), "data_dir = \"")[1], "\"")[0]
				want = fmt.Sprintf(want, data)
			}
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not contain %q", err, want)
			}
			if readied || slices.Contains(steps, "drain") {
				t.Errorf("reached the listener: steps %v", steps)
			}
			if c.name != "the lock held" && slices.Contains(steps, "lock") && !slices.Contains(steps, "release lock") {
				t.Errorf("the lock was taken and not released: steps %v", steps)
			}
		})
	}
}

func TestConfigPath(t *testing.T) {
	env := func(v string) func(string) string { return func(string) string { return v } }
	for _, c := range []struct{ flag, env, want string }{
		{"a.toml", "b.toml", "a.toml"},
		{"", "b.toml", "b.toml"},
		{"", "", "config.toml"},
	} {
		if got := configPath(c.flag, env(c.env)); got != c.want {
			t.Errorf("configPath(%q, %q) = %q, want %q", c.flag, c.env, got, c.want)
		}
	}
}

// helperServe runs this binary as `ketju serve` against path and returns the
// command and the address it reports, once it is listening.
func helperServe(t *testing.T, path string) (*exec.Cmd, string, *buffer) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "KETJU_SERVE_HELPER="+path)
	stderr := &buffer{}
	cmd.Stderr = stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatalf("the helper did not start: %v\n%s", err, stderr)
	}
	return cmd, strings.TrimSpace(line), stderr
}

// TestSIGTERMDrainsAndExitsZero is AC1: a real process, a request in
// flight, and SIGTERM. The request completes, and the process exits zero.
// It also carries the lock's serve-level checks (TKT-01M29CCBZN): a second
// serve against the same directory exits non-zero naming the first's pid,
// and after the first exits the lock is free.
func TestSIGTERMDrainsAndExitsZero(t *testing.T) {
	path, data := writeConfig(t, "")
	cmd, addr, stderr := helperServe(t, path)

	second := exec.Command(os.Args[0], "-test.run=^$")
	second.Env = append(os.Environ(), "KETJU_SERVE_HELPER="+path)
	out, err := second.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() == 0 {
		t.Errorf("a second serve against the same directory: %v\n%s", err, out)
	}
	if want := fmt.Sprintf("pid %d", cmd.Process.Pid); !strings.Contains(string(out), want) {
		t.Errorf("the second serve's refusal does not name %q:\n%s", want, out)
	}

	type result struct {
		code int
		body string
		err  error
	}
	res := make(chan result, 1)
	go func() {
		resp, err := http.Get("http://" + addr + "/test/slow")
		if err != nil {
			res <- result{err: err}
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		res <- result{code: resp.StatusCode, body: string(b)}
	}()
	time.Sleep(100 * time.Millisecond) // the request is inside its 300 ms
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	r := <-res
	if r.err != nil || r.code != http.StatusOK || r.body != "done" {
		t.Errorf("the request in flight at SIGTERM: %d %q %v", r.code, r.body, r.err)
	}
	if err := cmd.Wait(); err != nil {
		t.Errorf("serve exited with %v after SIGTERM\n%s", err, stderr)
	}
	l, err := lock.Acquire(data, "test")
	if err != nil {
		t.Fatalf("the lock was not released on graceful shutdown: %v", err)
	}
	l.Release()
}

// A thread catch-up finds corrupt is answered 503 by the running server, and
// the rest of the board keeps serving (docs/03 "Durability and recovery").
func TestServeQuarantinesACorruptThread(t *testing.T) {
	path, data := writeConfig(t, "")
	r := start(t, path, serveOpts{})
	if err := r.shutdown(t); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(data, "db", "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	err = d.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		_, err := q.CreateForum(t.Context(), dbq.CreateForumParams{Slug: "general", Name: "General",
			Visibility: "public", CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	d.Close()
	if err != nil {
		t.Fatal(err)
	}
	ids := seedThreads(t, data, 2)
	bad, good := ids[0], ids[1]

	// A first serve projects both; the log of one is then cut below its
	// projection, which no crash can do.
	r = start(t, path, serveOpts{})
	if err := r.shutdown(t); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(bad.LogPath(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(bad.LogPath(data), fi.Size()-5); err != nil {
		t.Fatal(err)
	}
	os.Remove(bad.IndexPath(data))

	r = start(t, path, serveOpts{})
	if code, body := get(t, "http://"+r.addr+"/api/v1/threads/"+bad.String()); code != http.StatusServiceUnavailable {
		t.Errorf("the corrupt thread = %d: %s", code, body)
	}
	if code, body := get(t, "http://"+r.addr+"/api/v1/threads/"+good.String()); code != http.StatusOK {
		t.Errorf("an unaffected thread = %d: %s", code, body)
	}
	// healthz reports the quarantine as degraded, and still answers 200: one
	// bad file must not take the forum down through a load balancer either.
	if code, body := get(t, "http://"+r.addr+"/healthz"); code != http.StatusOK ||
		!strings.Contains(body, `"status":"degraded"`) || strings.Contains(body, bad.String()) {
		t.Errorf("GET /healthz with a quarantined thread = %d: %s", code, body)
	}
}

// Shutdown waits for the projection runner to return before it closes the
// thread cache and the database the runner's retries use (review 470). The
// runner is held back a moment as it returns; nothing is closed under it.
func TestShutdownWaitsForTheProjectionRunner(t *testing.T) {
	path, _ := writeConfig(t, "")
	var r *running
	early := make(chan bool, 1)
	r = start(t, path, serveOpts{runReturned: func() {
		time.Sleep(200 * time.Millisecond)
		early <- slices.Contains(r.steps(), "close threads")
	}})
	if err := r.shutdown(t); err != nil {
		t.Fatalf("shutdown: %v\n%s", err, r.log)
	}
	if <-early {
		t.Error("the thread cache was closed while the projection runner was still running")
	}
}

// failingListener accepts until it is told to fail, then fails every Accept.
type failingListener struct {
	net.Listener
	fail chan struct{}
}

func (l failingListener) Accept() (net.Conn, error) {
	type result struct {
		c   net.Conn
		err error
	}
	got := make(chan result, 1)
	go func() { c, err := l.Listener.Accept(); got <- result{c, err} }()
	select {
	case r := <-got:
		return r.c, r.err
	case <-l.fail:
		return nil, errors.New("accept failed")
	}
}

// A listener that fails while a request is running shuts down like a
// signal: the request is drained before the stores are closed, and serve
// returns the listener's error (review 471).
func TestFailingListenerDrainsFirst(t *testing.T) {
	path, _ := writeConfig(t, "")
	fail := make(chan struct{})
	entered, release := make(chan struct{}), make(chan struct{})
	r := start(t, path, serveOpts{
		listen: func(ln net.Listener) net.Listener { return failingListener{ln, fail} },
		wrap: func(h http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/test/block" {
					close(entered)
					<-release
					io.WriteString(w, "done")
					return
				}
				h.ServeHTTP(w, req)
			})
		},
	})
	answered := make(chan int, 1)
	go func() {
		resp, err := http.Get("http://" + r.addr + "/test/block")
		if err != nil {
			answered <- 0
			return
		}
		resp.Body.Close()
		answered <- resp.StatusCode
	}()
	<-entered
	close(fail)

	time.Sleep(200 * time.Millisecond)
	if slices.Contains(r.steps(), "close threads") {
		t.Error("the stores were closed while a request was still running")
	}
	close(release)
	if code := <-answered; code != http.StatusOK {
		t.Errorf("the request in flight when the listener failed got %d", code)
	}
	select {
	case err := <-r.done:
		if err == nil || !strings.Contains(err.Error(), "serving: accept failed") {
			t.Errorf("serve returned %v, want the listener's error", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("serve did not return after the listener failed")
	}
	if steps := r.steps(); !slices.Contains(steps, "drain") || !slices.Contains(steps, "release lock") {
		t.Errorf("steps = %v", steps)
	}
}
