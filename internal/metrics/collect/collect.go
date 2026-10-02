// Package collect is what ketju exports at /metrics: the families docs/02
// "Observability" lists, wired to the caches, the thread store, the event
// bus, the live-update hub and the projection (TKT-01M29CEE7S).
//
// Two families are observed as work happens, so they exist before the
// pieces that feed them: request latency (internal/web's middleware) and
// fsync latency (the thread cache's SyncObserver). Everything else is read
// at scrape time through hooks that take no lock a request takes.
//
// No label carries a thread id, a handle, a forum slug, a request path or a
// query. The labels are a route pattern, a method from a fixed set, a status
// class, a cache name, a subscriber name, a file kind and a stream kind:
// every value comes from the code, not from a request.
package collect

import (
	"context"
	"database/sql"
	"runtime"
	runtimemetrics "runtime/metrics"
	"sync"
	"time"

	"github.com/terva-sh/ketju/internal/events"
	"github.com/terva-sh/ketju/internal/metrics"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/service/respcache"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
	"github.com/terva-sh/ketju/internal/web/sse"
)

// RequestBuckets are request latency's upper bounds, in seconds: 5 ms to
// 5 s.
var RequestBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}

// FsyncBuckets are fsync latency's upper bounds, in seconds: 0.1 ms to 1 s.
var FsyncBuckets = []float64{0.0001, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 1}

// Set is the registry and the two families observed as work happens.
type Set struct {
	Registry *metrics.Registry

	// Requests is ketju_http_request_duration_seconds{route,method,code},
	// for web.Options.Requests.
	Requests *metrics.HistogramVec

	// Fsync is ketju_threadlog_fsync_duration_seconds{file}; ObserveSync
	// feeds it.
	Fsync *metrics.HistogramVec
}

// New returns a Set whose registry holds the two observed families. Call
// Register once the sources exist.
func New() *Set {
	r := metrics.NewRegistry()
	return &Set{
		Registry: r,
		Requests: r.HistogramVec("ketju_http_request_duration_seconds",
			"Time to answer an HTTP request, by the route pattern that matched, the method and the status class. "+
				"Live-update streams are not included.",
			RequestBuckets, "route", "method", "code"),
		Fsync: r.HistogramVec("ketju_threadlog_fsync_duration_seconds",
			"Time an fsync of a thread's log or index took. Nothing is observed when storage.fsync is \"never\".",
			FsyncBuckets, "file"),
	}
}

// ObserveSync is the thread cache's SyncObserver.
func (s *Set) ObserveSync(file string, d time.Duration) {
	s.Fsync.With(file).Observe(d.Seconds())
}

// Sources are what the scrape-time families read. Every field is required.
type Sources struct {
	Responses *respcache.Cache
	Render    *render.Cache
	Threads   *threadcache.Cache
	Hub       *sse.Hub
	Bus       *events.Bus
	Lag       *ProjectionLag
	// Quarantined is the projection's QuarantinedCount.
	Quarantined func() int
}

// Register adds the scrape-time families. Each reads only atomics, or, for
// projection lag, the read pool and the file system.
func (s *Set) Register(src Sources) {
	r := s.Registry
	caches := func(pick func(respcache.Stats, render.Stats) [2]uint64) func(emit func(metrics.Sample)) {
		return func(emit func(metrics.Sample)) {
			v := pick(src.Responses.Counters(), src.Render.Counters())
			emit(metrics.Sample{Labels: []string{"response"}, Value: float64(v[0])})
			emit(metrics.Sample{Labels: []string{"render"}, Value: float64(v[1])})
		}
	}
	cache := []string{"cache"}
	r.CounterFunc("ketju_cache_hits_total", "Lookups the response or render cache answered.", cache,
		caches(func(a respcache.Stats, b render.Stats) [2]uint64 { return [2]uint64{a.Hits, b.Hits} }))
	r.CounterFunc("ketju_cache_misses_total",
		"Lookups the response or render cache could not answer. A stale response entry counts as a miss.", cache,
		caches(func(a respcache.Stats, b render.Stats) [2]uint64 { return [2]uint64{a.Misses, b.Misses} }))
	r.CounterFunc("ketju_cache_evictions_total", "Entries the response or render cache dropped to stay within its bound.",
		cache, caches(func(a respcache.Stats, b render.Stats) [2]uint64 { return [2]uint64{a.Evictions, b.Evictions} }))
	r.CounterFunc("ketju_cache_stale_total",
		"Response cache lookups that found an entry whose render dependencies had moved.", nil,
		func(emit func(metrics.Sample)) {
			emit(metrics.Sample{Value: float64(src.Responses.Counters().Stale)})
		})

	r.CounterFunc("ketju_thread_cache_opens_total", "Threads the thread cache opened from disk.", nil,
		func(emit func(metrics.Sample)) {
			opens, _ := src.Threads.Stats()
			emit(metrics.Sample{Value: float64(opens)})
		})
	r.CounterFunc("ketju_thread_cache_evictions_total", "Threads the thread cache closed to stay within its bound.", nil,
		func(emit func(metrics.Sample)) {
			_, ev := src.Threads.Stats()
			emit(metrics.Sample{Value: float64(ev)})
		})
	r.GaugeFunc("ketju_thread_cache_open_threads", "Threads open in the thread cache now.", nil,
		func(emit func(metrics.Sample)) { emit(metrics.Sample{Value: float64(src.Threads.Len())}) })

	r.GaugeFunc("ketju_projection_lag_bytes",
		"Bytes of thread log the database projection has not applied yet, summed over every thread. "+
			"Read at most every 10 s.", nil,
		func(emit func(metrics.Sample)) { emit(metrics.Sample{Value: float64(src.Lag.Bytes())}) })
	r.GaugeFunc("ketju_threads_quarantined", "Threads quarantined because their log is shorter than their projection.",
		nil, func(emit func(metrics.Sample)) { emit(metrics.Sample{Value: float64(src.Quarantined())}) })

	sub := []string{"subscriber"}
	r.GaugeFunc("ketju_events_queued", "Events waiting in a bus subscriber's queue.", sub,
		func(emit func(metrics.Sample)) {
			for _, st := range src.Bus.Stats() {
				emit(metrics.Sample{Labels: []string{st.Name}, Value: float64(st.Queued)})
			}
		})
	r.CounterFunc("ketju_events_dropped_total",
		"Events a bus subscriber's full queue dropped; its catch-up recovers them.", sub,
		func(emit func(metrics.Sample)) {
			for _, st := range src.Bus.Stats() {
				emit(metrics.Sample{Labels: []string{st.Name}, Value: float64(st.Dropped)})
			}
		})

	r.GaugeFunc("ketju_sse_streams", "Live-update streams open now, by member or anonymous.", []string{"kind"},
		func(emit func(metrics.Sample)) {
			st := src.Hub.Stats()
			emit(metrics.Sample{Labels: []string{"anonymous"}, Value: float64(st.Anonymous)})
			emit(metrics.Sample{Labels: []string{"member"}, Value: float64(st.Open - st.Anonymous)})
		})
	r.CounterFunc("ketju_sse_refused_total", "Live-update streams a connection limit refused.", nil,
		func(emit func(metrics.Sample)) { emit(metrics.Sample{Value: float64(src.Hub.Stats().Refused)}) })
	r.CounterFunc("ketju_sse_overflowed_total", "Live-update streams closed because their queue filled.", nil,
		func(emit func(metrics.Sample)) { emit(metrics.Sample{Value: float64(src.Hub.Stats().Overflowed)}) })
	r.CounterFunc("ketju_sse_lagged_total",
		"Times the projection did not reach a live event within the wait, so its streams were closed.", nil,
		func(emit func(metrics.Sample)) { emit(metrics.Sample{Value: float64(src.Hub.Stats().Lagged)}) })

	r.GaugeFunc("ketju_goroutines", "Goroutines running now.", nil,
		func(emit func(metrics.Sample)) { emit(metrics.Sample{Value: float64(runtime.NumGoroutine())}) })
	r.GaugeFunc("ketju_heap_bytes", "Bytes of heap held by live and not yet swept objects.", nil,
		func(emit func(metrics.Sample)) {
			s := []runtimemetrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
			runtimemetrics.Read(s)
			var v float64
			if s[0].Value.Kind() == runtimemetrics.KindUint64 {
				v = float64(s[0].Value.Uint64())
			}
			emit(metrics.Sample{Value: v})
		})
}

// LagTTL is how long a projection lag result is reused, so a scraper polling
// fast cannot make the directory walk constant.
const LagTTL = 10 * time.Second

// ProjectionLag is the summed projection lag: for every thread, its log's
// length minus the length its projection has applied, when that is
// positive. A thread with no projection row counts from the end of its file
// header, as catch-up projects it. A log shorter than its projection, which
// is what a quarantined thread has, adds nothing. One statement on the read
// pool reads every projected length, and the walk reads every log's size
// from its directory entry; neither waits on a writer.
type ProjectionLag struct {
	DB   *db.DB
	Root string
	// Now is the clock the TTL is measured with; nil is time.Now.
	Now func() time.Time
	// Log receives a failed read; the gauge then keeps its last value.
	Log func(msg string, args ...any)

	mu    sync.Mutex // scrape-only
	at    time.Time
	bytes int64
}

// Bytes is the lag, from a result at most LagTTL old.
func (p *ProjectionLag) Bytes() int64 {
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.at.IsZero() && now().Sub(p.at) < LagTTL {
		return p.bytes
	}
	n, err := p.compute(context.Background())
	if err != nil {
		if p.Log != nil {
			p.Log("could not compute projection lag for the metrics endpoint", "err", err)
		}
		return p.bytes
	}
	p.at, p.bytes = now(), n
	return n
}

func (p *ProjectionLag) compute(ctx context.Context) (int64, error) {
	projected := map[threadid.ID]int64{}
	if err := p.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		rows, err := q.ListProjectionLengths(ctx)
		for _, r := range rows {
			projected[threadid.ID(r.ThreadID)] = r.LogLength
		}
		return err
	}); err != nil {
		return 0, err
	}
	logs, _, err := threadid.WalkLogs(p.Root)
	if err != nil {
		return 0, err
	}
	var sum int64
	for id, size := range logs {
		at, ok := projected[id]
		if !ok {
			at = threadlog.FileHeaderSize
		}
		if size > at {
			sum += size - at
		}
	}
	return sum, nil
}
