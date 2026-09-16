package web

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/logging"
	"github.com/terva-sh/ketju/internal/problem"
)

// clock is a hand-wound clock. Rate limiting is entirely about time, and a
// test that waits for real time is a test that is slow and flaky at once.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func TestABucketAllowsItsBurstThenRefuses(t *testing.T) {
	c := newClock()
	l := newLimiter(Rate{Every: time.Second, Burst: 3}, 10, c.Now)

	for i := range 3 {
		if ok, _ := l.allow("k"); !ok {
			t.Fatalf("request %d was refused inside the burst", i+1)
		}
	}
	ok, retry := l.allow("k")
	if ok {
		t.Fatal("the burst was exceeded and the request was still allowed")
	}
	if retry <= 0 || retry > time.Second {
		t.Errorf("retry = %v, want a wait of at most one refill interval", retry)
	}
}

func TestABucketRefillsOverTime(t *testing.T) {
	c := newClock()
	l := newLimiter(Rate{Every: time.Second, Burst: 2}, 10, c.Now)

	l.allow("k")
	l.allow("k")
	if ok, _ := l.allow("k"); ok {
		t.Fatal("the bucket was empty and still allowed a request")
	}

	c.advance(time.Second)
	if ok, _ := l.allow("k"); !ok {
		t.Error("a token did not arrive after one interval")
	}
	if ok, _ := l.allow("k"); ok {
		t.Error("two tokens arrived when one interval had passed")
	}
}

// TestABucketDoesNotRefillPastItsBurst. Without the cap, a caller who waits an
// hour banks an hour of tokens and can spend them all at once, which is the
// burst the limiter exists to bound.
func TestABucketDoesNotRefillPastItsBurst(t *testing.T) {
	c := newClock()
	l := newLimiter(Rate{Every: time.Second, Burst: 2}, 10, c.Now)

	l.allow("k")
	c.advance(time.Hour)

	allowed := 0
	for range 10 {
		if ok, _ := l.allow("k"); ok {
			allowed++
		}
	}
	if allowed != 2 {
		t.Errorf("allowed %d after a long idle, want the burst of 2", allowed)
	}
}

// TestKeysAreIndependent: one noisy caller must not spend another's allowance.
func TestKeysAreIndependent(t *testing.T) {
	c := newClock()
	l := newLimiter(Rate{Every: time.Second, Burst: 1}, 10, c.Now)

	if ok, _ := l.allow("a"); !ok {
		t.Fatal("a's first request was refused")
	}
	if ok, _ := l.allow("a"); ok {
		t.Fatal("a got two tokens from a burst of one")
	}
	if ok, _ := l.allow("b"); !ok {
		t.Error("b was refused because a had spent its own allowance")
	}
}

// TestTheTableIsBounded is the memory property. Without it, a caller with a
// large address pool turns the limiter into the exhaustion it prevents.
func TestTheTableIsBounded(t *testing.T) {
	c := newClock()
	const max = 64
	l := newLimiter(Rate{Every: time.Second, Burst: 1}, max, c.Now)

	for i := range max * 20 {
		l.allow("k" + strconv.Itoa(i))
	}

	l.mu.Lock()
	total := len(l.cur) + len(l.old)
	l.mu.Unlock()

	if total > 2*max {
		t.Errorf("the limiter is tracking %d keys with a bound of %d (two generations of %d)",
			total, 2*max, max)
	}
}

// TestAnActiveKeySurvivesRotation. A caller who keeps arriving is promoted out
// of the old generation, so a flood of new keys cannot reset their bucket and
// hand them a fresh burst.
func TestAnActiveKeySurvivesRotation(t *testing.T) {
	c := newClock()
	const max = 8
	l := newLimiter(Rate{Every: time.Hour, Burst: 1}, max, c.Now)

	if ok, _ := l.allow("victim"); !ok {
		t.Fatal("the first request was refused")
	}
	// Fill exactly one generation, so victim lands in old but is not dropped.
	for i := range max {
		l.allow("filler" + strconv.Itoa(i))
	}
	if ok, _ := l.allow("victim"); ok {
		t.Error("victim's bucket was reset by other callers, handing them a fresh burst")
	}
}

func TestAnUnconfiguredLimiterDoesNotLimit(t *testing.T) {
	l := newLimiter(Rate{}, 10, newClock().Now)
	for range 100 {
		if ok, _ := l.allow("k"); !ok {
			t.Fatal("a zero-value rate refused a request")
		}
	}
}

// --- through the middleware --------------------------------------------------

func limitedServer(t *testing.T, c *clock) http.Handler {
	t.Helper()
	log, _ := testLogger()
	return New(Groups{
		API:  http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
		Auth: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	}, Options{Logger: log, RateLimit: RateLimit(c.Now)})
}

func jsonPost(path string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, path, nil)
	r.Header.Set("Content-Type", "application/json")
	return r
}

// TestALimitedResponseSaysWhenToRetry is the second half of AC3.
func TestALimitedResponseSaysWhenToRetry(t *testing.T) {
	c := newClock()
	h := limitedServer(t, c)

	var last *httptest.ResponseRecorder
	for range authRate.Burst + 5 {
		last = httptest.NewRecorder()
		h.ServeHTTP(last, httptest.NewRequest(http.MethodGet, "/auth/google/start", nil))
	}

	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 once the burst is spent", last.Code)
	}
	ra := last.Header().Get("Retry-After")
	if ra == "" {
		t.Fatal("a limited response carried no Retry-After, so a client cannot know when to come back")
	}
	secs, err := strconv.Atoi(ra)
	if err != nil || secs < 1 {
		t.Errorf("Retry-After = %q, want whole seconds of at least 1", ra)
	}
	if ct := last.Header().Get("Content-Type"); ct != problem.ContentType {
		t.Errorf("content type = %q, want a problem response", ct)
	}
}

// TestObeyingRetryAfterWorks: a client that waits exactly as long as it was
// told must get through. If it did not, the header would be advice that makes
// things worse.
func TestObeyingRetryAfterWorks(t *testing.T) {
	c := newClock()
	h := limitedServer(t, c)

	var rec *httptest.ResponseRecorder
	for range authRate.Burst + 1 {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/google/start", nil))
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}

	secs, _ := strconv.Atoi(rec.Header().Get("Retry-After"))
	c.advance(time.Duration(secs) * time.Second)

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/google/start", nil))
	if rec.Code == http.StatusTooManyRequests {
		t.Error("a client that waited exactly as long as it was told was refused again")
	}
}

// TestAuthAndWritesAreLimitedSeparately. Signing in and posting have limits
// orders of magnitude apart; sharing one bucket would apply the wrong one to
// both.
func TestAuthAndWritesAreLimitedSeparately(t *testing.T) {
	c := newClock()
	h := limitedServer(t, c)

	// Spend the auth allowance completely.
	for range authRate.Burst + 5 {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/auth/google/start", nil))
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, jsonPost(APIPrefix+"threads"))
	if rec.Code == http.StatusTooManyRequests {
		t.Error("spending the auth allowance also blocked a write")
	}
}

// TestReadsAreNotLimited. docs/08's reasoning against post rate limits applies
// with more force to reading, and a reader who refreshes quickly is the
// behaviour a forum wants.
func TestReadsAreNotLimited(t *testing.T) {
	c := newClock()
	h := limitedServer(t, c)

	for i := range writeRate.Burst * 10 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, APIPrefix+"threads", nil))
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("read %d was rate limited", i+1)
		}
	}
}

// TestNoViewerIsVisibleToTheLimiter is why the limiter keys on the address and
// nothing else, and it pins the constraint that forces it.
//
// The limiter sits outside session resolution on purpose: a session lookup is
// a database query, and limiting after it would mean an unauthenticated flood
// costs a query per request. The consequence is that no viewer exists yet when
// the limiter runs. If somebody later moves session resolution outward to make
// per-identity limiting possible here, this test fails and tells them what
// they just traded away.
func TestNoViewerIsVisibleToTheLimiter(t *testing.T) {
	log, _ := testLogger()

	sawViewer := false
	probe := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := logging.ViewerFrom(r.Context()); ok {
				sawViewer = true
			}
			next.ServeHTTP(w, r)
		})
	}
	session := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(logging.ContextWithViewer(r.Context(), 42)))
		})
	}

	reachedHandler := false
	h := New(Groups{
		API: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := logging.ViewerFrom(r.Context()); ok {
				reachedHandler = true
			}
		}),
	}, Options{Logger: log, RateLimit: probe, Session: session})

	h.ServeHTTP(httptest.NewRecorder(), jsonPost(APIPrefix+"threads"))

	if sawViewer {
		t.Error("the limiter can see the viewer, which means session resolution now runs outside it; " +
			"an unauthenticated flood now costs a database query per request")
	}
	if !reachedHandler {
		t.Error("the handler saw no viewer, so this test proved nothing about ordering")
	}
}

// TestUnauthenticatedCallersAreKeyedByAddress is the other half: with no
// viewer, all a limiter has is where the request came from.
func TestUnauthenticatedCallersAreKeyedByAddress(t *testing.T) {
	c := newClock()
	h := limitedServer(t, c)

	spend := func(addr string, n int) int {
		limited := 0
		for range n {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/auth/google/start", nil)
			req.RemoteAddr = addr
			h.ServeHTTP(rec, req)
			if rec.Code == http.StatusTooManyRequests {
				limited++
			}
		}
		return limited
	}

	if got := spend("198.51.100.1:5000", authRate.Burst+5); got == 0 {
		t.Fatal("the first address was never limited, so this test is not measuring anything")
	}
	if got := spend("198.51.100.2:5000", 1); got != 0 {
		t.Error("a second address was limited by the first's traffic")
	}
}

// TestASpoofedForwardedForCannotMintFreshKeys.
//
// X-Forwarded-For is trivially spoofable. Trusting it without a configured
// list of trusted proxies would not weaken the limiter but remove it: every
// request could claim a new key and never be limited at all.
func TestASpoofedForwardedForCannotMintFreshKeys(t *testing.T) {
	c := newClock()
	h := limitedServer(t, c)

	limited := 0
	for i := range authRate.Burst + 10 {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/auth/google/start", nil)
		req.RemoteAddr = "198.51.100.9:5000"
		// A different claimed address every time.
		req.Header.Set("X-Forwarded-For", "10.0.0."+strconv.Itoa(i))
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			limited++
		}
	}
	if limited == 0 {
		t.Error("rotating X-Forwarded-For bypassed the limiter entirely")
	}
}
