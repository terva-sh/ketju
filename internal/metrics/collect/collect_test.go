package collect

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/events"
	"github.com/terva-sh/ketju/internal/metrics"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/service/respcache"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/db/dbtest"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
	"github.com/terva-sh/ketju/internal/web/sse"
)

// fixture is a Set registered over real sources.
type fixture struct {
	set   *Set
	root  string
	db    *db.DB
	cache *threadcache.Cache
	resp  *respcache.Cache
	rend  *render.Cache
	bus   *events.Bus
	quar  int
}

func newFixture(t *testing.T, policy threadlog.SyncPolicy) *fixture {
	t.Helper()
	f := &fixture{set: New(), root: t.TempDir()}
	f.db = dbtest.Open(t, db.Options{})
	var err error
	f.cache, err = threadcache.New(f.root, threadcache.Options{Sync: policy, SyncObserver: f.set.ObserveSync})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.cache.Close() })
	f.resp = respcache.New(1 << 20)
	f.rend = render.NewCache(1 << 20)
	f.bus = events.New(events.Options{})
	hub := sse.NewHub(sse.HubOptions{})
	if err := f.bus.Subscribe(events.SubscriberFunc{N: "test", F: func(context.Context, events.Batch) error {
		return nil
	}}, 0); err != nil {
		t.Fatal(err)
	}
	f.set.Register(Sources{Responses: f.resp, Render: f.rend, Threads: f.cache, Hub: hub, Bus: f.bus,
		Quarantined: func() int { return f.quar },
		Lag:         &ProjectionLag{DB: f.db, Root: f.root}})
	return f
}

func (f *fixture) scrape(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	if err := f.set.Registry.WriteText(&b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// value is a sample's value, by its name and label text as the exposition
// writes them, and false when it is not there.
func value(scrape, series string) (float64, bool) {
	for l := range strings.SplitSeq(scrape, "\n") {
		if rest, ok := strings.CutPrefix(l, series+" "); ok {
			v, err := strconv.ParseFloat(rest, 64)
			return v, err == nil
		}
	}
	return 0, false
}

// docRow is one row of docs/02's metrics table.
type docRow struct {
	typ, unit string
	labels    []string
}

var docRowRE = regexp.MustCompile("^\\| `(ketju_[a-z0-9_]+)` \\| (counter|gauge|histogram) \\| ([a-z]+|—) \\| ([^|]*)\\|")

// docTable reads the metrics table in docs/02 "Observability".
func docTable(t *testing.T) map[string]docRow {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "02-architecture.md"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]docRow{}
	for l := range strings.SplitSeq(string(b), "\n") {
		m := docRowRE.FindStringSubmatch(l)
		if m == nil {
			if strings.HasPrefix(l, "| `ketju_") {
				t.Errorf("docs/02 has a metric row this test cannot read: %q", l)
			}
			continue
		}
		var labels []string
		for _, s := range strings.Split(m[4], ",") {
			if s = strings.Trim(strings.TrimSpace(s), "`"); s != "" && s != "—" {
				labels = append(labels, s)
			}
		}
		if _, dup := out[m[1]]; dup {
			t.Errorf("docs/02 lists %s twice", m[1])
		}
		out[m[1]] = docRow{typ: m[2], unit: m[3], labels: labels}
	}
	if len(out) == 0 {
		t.Fatal("docs/02 has no metrics table")
	}
	return out
}

// TestMetricNamesFollowConventions: every name serve registers follows the
// Prometheus conventions, carries its unit as a suffix, and is exactly the
// set docs/02 lists, with the type and labels it lists.
func TestMetricNamesFollowConventions(t *testing.T) {
	f := newFixture(t, threadlog.SyncNever)
	reg := f.set.Registry
	doc := docTable(t)
	names := reg.Names()
	var documented []string
	for n := range doc {
		documented = append(documented, n)
	}
	slices.Sort(documented)
	if !slices.Equal(names, documented) {
		t.Errorf("registered %v\ndocs/02 lists %v", names, documented)
	}
	for _, n := range names {
		typ, _ := reg.TypeOf(n)
		if err := metrics.CheckName(n, typ); err != nil {
			t.Error(err)
		}
		row, ok := doc[n]
		if !ok {
			continue
		}
		if string(typ) != row.typ {
			t.Errorf("%s is a %s; docs/02 says %s", n, typ, row.typ)
		}
		if got := reg.LabelsOf(n); !slices.Equal(got, row.labels) {
			t.Errorf("%s has labels %v; docs/02 says %v", n, got, row.labels)
		}
		base := strings.TrimSuffix(n, "_total")
		switch row.unit {
		case "—":
			for _, u := range []string{"_seconds", "_bytes"} {
				if strings.HasSuffix(base, u) {
					t.Errorf("%s carries a unit docs/02 does not list", n)
				}
			}
		default:
			if !strings.HasSuffix(base, "_"+row.unit) {
				t.Errorf("%s is in %s and must end in _%s", n, row.unit, row.unit)
			}
		}
	}
}

// writeLog writes a log of size bytes for id, which is all the lag reads.
func writeLog(t *testing.T, root string, id threadid.ID, size int) {
	t.Helper()
	if err := os.MkdirAll(id.Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(id.LogPath(root), make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setProjected(t *testing.T, d *db.DB, id threadid.ID, n int64) {
	t.Helper()
	if err := d.Write(context.Background(), func(q *dbq.Queries, _ *sql.Tx) error {
		return q.SetProjectionState(context.Background(), dbq.SetProjectionStateParams{
			ThreadID: int64(id), LogLength: n, UpdatedAt: 1})
	}); err != nil {
		t.Fatal(err)
	}
}

// TestProjectionLagSumsGaps: two threads ahead of their projection by known
// amounts give their sum; a thread the projection never saw counts from its
// header; a current thread adds nothing, and so does a quarantined one,
// whose log is shorter than its projection. The result is reused for the
// TTL.
func TestProjectionLagSumsGaps(t *testing.T) {
	f := newFixture(t, threadlog.SyncNever)
	id := func(n uint64) threadid.ID { return threadid.ID((1_700_000_000_000+n)<<16 | 1) }
	writeLog(t, f.root, id(1), 1000)
	setProjected(t, f.db, id(1), 900) // 100 behind
	writeLog(t, f.root, id(2), 5000)
	setProjected(t, f.db, id(2), 4000) // 1000 behind
	writeLog(t, f.root, id(3), 700)
	setProjected(t, f.db, id(3), 700) // current
	writeLog(t, f.root, id(4), 300)
	setProjected(t, f.db, id(4), 800) // quarantined: shorter than projected
	writeLog(t, f.root, id(5), threadlog.FileHeaderSize+20)
	setProjected(t, f.db, id(6), 50) // a log that is gone
	f.quar = 2

	now := time.Unix(1000, 0)
	lag := &ProjectionLag{DB: f.db, Root: f.root, Now: func() time.Time { return now }}
	if got, want := lag.Bytes(), int64(100+1000+20); got != want {
		t.Errorf("lag = %d, want %d", got, want)
	}
	writeLog(t, f.root, id(3), 900)
	if got := lag.Bytes(); got != 1120 {
		t.Errorf("within the TTL lag = %d, want the reused 1120", got)
	}
	now = now.Add(LagTTL)
	if got := lag.Bytes(); got != 1320 {
		t.Errorf("after the TTL lag = %d, want 1320", got)
	}

	s := f.scrape(t)
	if v, ok := value(s, "ketju_projection_lag_bytes"); !ok || v != 1320 {
		t.Errorf("ketju_projection_lag_bytes = %v, %t; want 1320", v, ok)
	}
	if v, ok := value(s, "ketju_threads_quarantined"); !ok || v != 2 {
		t.Errorf("ketju_threads_quarantined = %v, %t; want 2", v, ok)
	}
}

// TestFsyncLatencyObserved: with group commit, appends add observations to
// file="log", a Delete that rewrites an entry fsyncs the index before its
// header, and closing an actor fsyncs the index. Under "never", nothing is
// observed.
func TestFsyncLatencyObserved(t *testing.T) {
	f := newFixture(t, threadlog.SyncGroup)
	id := threadid.ID(1_700_000_000_000<<16 | 1)
	th, err := f.cache.Create(id, 1, 1_700_000_000_000)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []threadlog.Append{
		{Type: threadlog.TypeThreadCreated, Author: 1, TimeMS: 1_700_000_000_000,
			Body: &threadlog.ThreadCreatedBody{Title: "T", Slug: "t", ForumID: 1}},
		{Type: threadlog.TypePost, Author: 1, TimeMS: 1_700_000_001_000, Body: &threadlog.PostBody{Source: "hi"}},
	} {
		if _, err := th.Actor.Append(a); err != nil {
			t.Fatal(err)
		}
	}
	f.cache.Release(th)
	s := f.scrape(t)
	if v, _ := value(s, `ketju_threadlog_fsync_duration_seconds_count{file="log"}`); v < 2 {
		t.Errorf("log fsyncs observed = %v, want at least 2:\n%s", v, s)
	}
	if err := f.cache.Close(); err != nil {
		t.Fatal(err)
	}
	s = f.scrape(t)
	if v, _ := value(s, `ketju_threadlog_fsync_duration_seconds_count{file="index"}`); v < 1 {
		t.Errorf("index fsyncs observed = %v, want at least 1 after close:\n%s", v, s)
	}

	never := newFixture(t, threadlog.SyncNever)
	th, err = never.cache.Create(id, 1, 1_700_000_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := th.Actor.Append(threadlog.Append{Type: threadlog.TypeThreadCreated, Author: 1, TimeMS: 1,
		Body: &threadlog.ThreadCreatedBody{Title: "T", Slug: "t", ForumID: 1}}); err != nil {
		t.Fatal(err)
	}
	never.cache.Release(th)
	never.cache.Close()
	if n := never.set.Fsync.Series(); n != 0 {
		t.Errorf("fsync \"never\" observed %d series", n)
	}
}

// TestScrapeTakesNoRequestPathLock is the concurrent half of the criterion:
// scrapes run beside appends, thread opens and cache lookups under -race,
// and finish. That each hook takes no lock is held where the lock is, by
// tests that hold it: threadcache TestStatsTakeNoLock, respcache and render
// TestCountersTakeNoLock, sse TestStatsTakeNoLock, events
// TestStatsTakeNoLockOnceStarted and projection TestQuarantinedCountTakesNoLock.
func TestScrapeTakesNoRequestPathLock(t *testing.T) {
	f := newFixture(t, threadlog.SyncNever)
	if err := f.bus.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.bus.Close(context.Background()) })
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for w := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := threadid.ID(uint64(1_700_000_000_000+w)<<16 | 1)
			th, err := f.cache.Create(id, 1, 1)
			if err != nil {
				t.Error(err)
				return
			}
			f.cache.Release(th)
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				err := f.cache.With(id, func(th *threadcache.Thread) error {
					_, err := th.Actor.Append(threadlog.Append{Type: threadlog.TypePost, Author: 1, TimeMS: int64(i + 1),
						Body: &threadlog.PostBody{Source: "x"}})
					return err
				})
				if err != nil {
					t.Error(err)
					return
				}
				f.resp.Get(respcache.Key{Thread: id}, nil)
				f.rend.Get(render.Key{})
				f.bus.Publish(events.Event{Thread: id})
			}
		}()
	}
	for range 20 {
		f.scrape(t)
	}
	close(stop)
	wg.Wait()
	s := f.scrape(t)
	if v, ok := value(s, "ketju_thread_cache_open_threads"); !ok || v != 4 {
		t.Errorf("open threads = %v, %t; want 4", v, ok)
	}
}
