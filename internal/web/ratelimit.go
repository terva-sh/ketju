package web

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/terva-sh/ketju/internal/problem"
)

// Rate is a token bucket: Burst tokens, refilling one every Every.
//
// A bucket rather than a minimum interval, following docs/08: "twenty posts in
// a minute while working and then silence for hours" is the normal shape of
// bursty traffic, and a fixed interval would either reject that or permit a
// runaway.
type Rate struct {
	Every time.Duration
	Burst int
}

// The default rates.
//
// Read docs/08 before changing these, and read the note on writeRate before
// adding one. The numbers are deliberately far apart: the auth limiter is a
// credential-stuffing guard and is meant to bite, while the write limiter is a
// crash barrier nobody driving properly should ever touch.
var (
	// authRate covers /auth/*. Signing in is a handful of requests; anything
	// past this is a script working through a list. It is per IP because the
	// whole point is to constrain a caller who has not authenticated and has
	// no session to key on.
	authRate = Rate{Every: 6 * time.Second, Burst: 10}

	// writeRate is a denial-of-service ceiling, not a posting policy.
	//
	// docs/08 is explicit that humans are unlimited and that there is no
	// per-user post rate limit, on the reasoning that for a community of known
	// people it is low value and slow mode already covers the case a moderator
	// actually meets. This limiter does not contradict that and must not be
	// tightened until it does: at one write every half second with a burst of
	// sixty, no member typing prose can reach it, and a client stuck in a
	// retry loop is stopped before it fills a disk.
	//
	// The per-agent token bucket docs/08 actually specifies is a different
	// mechanism with its own settings, and belongs to TKT-01M29P9RSW with the
	// agent credential it is charged against.
	writeRate = Rate{Every: 500 * time.Millisecond, Burst: 60}
)

// maxTrackedKeys bounds how many callers a limiter remembers.
//
// Without a bound, a caller with a large address pool turns the limiter itself
// into the memory exhaustion it exists to prevent.
const maxTrackedKeys = 8192

// clientIP is the key a request is charged against.
//
// The address, and only the address. That is not a simplification -- it is
// forced by where this middleware sits.
//
// The limiter runs at chain position 5, outside session resolution, because a
// session lookup is a database query and checking the limit afterwards would
// mean an unauthenticated flood costs a query per request. The consequence is
// that no viewer exists yet when this runs: reading one here would always find
// nothing, and a key that silently never varies is worse than an honest one.
//
// Per-identity limiting therefore cannot live at this position. It needs a
// second stage after session resolution, and docs/08 says who it is for:
// humans are unlimited, agents are charged against a token bucket. That stage
// belongs to TKT-01M29P9RSW with the agent credential it charges against.
//
// X-Forwarded-For is deliberately not consulted. It is trivially spoofable, so
// trusting it without a configured list of trusted proxies would not weaken
// this limiter but remove it: every request could claim a fresh key. Reading
// it needs a trusted-proxy setting that config does not have yet.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return host
}

// bucket is one caller's allowance.
type bucket struct {
	tokens float64
	last   time.Time
}

// limiter is a token bucket per key, with bounded memory.
//
// Two generations rather than an LRU: when the live map fills it is demoted
// wholesale and a fresh one takes its place, so eviction is O(1) and no
// request ever pays for a scan. Memory is bounded at twice maxTrackedKeys.
//
// A key that falls out of both generations gets a fresh full bucket, so the
// failure mode under memory pressure is permissiveness. That is the right
// direction: the alternative is denying a member their allowance because
// somebody else filled the table, which turns the limiter into the attack.
type limiter struct {
	rate Rate
	max  int
	now  func() time.Time

	mu  sync.Mutex
	cur map[string]*bucket
	old map[string]*bucket
}

func newLimiter(r Rate, max int, now func() time.Time) *limiter {
	if now == nil {
		now = time.Now
	}
	if max <= 0 {
		max = maxTrackedKeys
	}
	return &limiter{rate: r, max: max, now: now, cur: make(map[string]*bucket)}
}

// allow spends a token. It reports whether the request may proceed and, when
// it may not, how long until the next token arrives.
func (l *limiter) allow(key string) (bool, time.Duration) {
	if l.rate.Burst <= 0 || l.rate.Every <= 0 {
		return true, 0 // an unconfigured limiter does not limit
	}

	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	b := l.lookup(key, now)

	// Refill for the time since the last request, capped at the burst.
	elapsed := now.Sub(b.last)
	if elapsed > 0 {
		b.tokens += float64(elapsed) / float64(l.rate.Every)
		if b.tokens > float64(l.rate.Burst) {
			b.tokens = float64(l.rate.Burst)
		}
	}
	b.last = now

	if b.tokens < 1 {
		// Time until the bucket holds one whole token.
		missing := 1 - b.tokens
		return false, time.Duration(missing * float64(l.rate.Every))
	}
	b.tokens--
	return true, 0
}

// lookup finds or creates a bucket, rotating generations when the live map is
// full. The caller holds the lock.
func (l *limiter) lookup(key string, now time.Time) *bucket {
	if b, ok := l.cur[key]; ok {
		return b
	}
	if b, ok := l.old[key]; ok {
		// Promote, so an active caller is not reset by the next rotation.
		l.cur[key] = b
		return b
	}
	if len(l.cur) >= l.max {
		l.old = l.cur
		l.cur = make(map[string]*bucket)
	}
	b := &bucket{tokens: float64(l.rate.Burst), last: now}
	l.cur[key] = b
	return b
}

// RateLimit returns the middleware for Options.RateLimit.
//
// Two limiters rather than one, because the two limits exist for different
// reasons and the numbers are nowhere near each other. See authRate and
// writeRate.
func RateLimit(now func() time.Time) func(http.Handler) http.Handler {
	auth := newLimiter(authRate, maxTrackedKeys, now)
	write := newLimiter(writeRate, maxTrackedKeys, now)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			l := limiterFor(r, auth, write)
			if l == nil {
				next.ServeHTTP(w, r)
				return
			}
			if ok, retry := l.allow(clientIP(r)); !ok {
				// AC3: the response says when to retry. Rounded up, so a
				// client that obeys it exactly does not arrive one tick early
				// and get refused again.
				problem.WriteRetryAfter(w, r, problem.CodeRateLimited, retry+time.Second)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// limiterFor picks the limiter a request is charged against, or nil for the
// reads that are not limited at all.
//
// Reads are unlimited on purpose. docs/08's reasoning for leaving out post
// rate limits -- that for a community of known people they are low value --
// applies with more force to reading, and a reader who refreshes quickly is
// the behaviour a forum wants.
func limiterFor(r *http.Request, auth, write *limiter) *limiter {
	if isAuthPath(r.URL.Path) {
		// Every method, including the GET that starts a login: a redirect to a
		// provider is the expensive half, and it is reachable without a POST.
		return auth
	}
	if isMutating(r.Method) && isAPIPath(r.URL.Path) {
		return write
	}
	return nil
}

func isAuthPath(p string) bool {
	return p == "/auth" || p == "/auth/" || len(p) > 6 && p[:6] == "/auth/"
}

// isMutating reports whether a method changes state.
//
// GET, HEAD and OPTIONS are the safe methods (RFC 9110). Everything else is
// treated as mutating, including methods this server does not implement: a
// limiter that has to be told about each new verb is a limiter with a gap.
func isMutating(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}
