package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/web"
)

// chain is w's API behind the whole web.New chain: the body limit, the real
// rate limiter on a frozen clock, and the real CSRF check. The session
// position reads the viewer from a test header, because what is under test
// is what the chain and the service do with a viewer, not how a cookie
// becomes one (internal/auth/session's tests own that).
func (w *world) chain() http.Handler {
	frozen := time.UnixMilli(1_757_500_000_000)
	session := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			var v domain.Viewer
			switch who := r.Header.Get("X-Test-Viewer"); who {
			case "":
			case "pre-user":
				v = domain.Viewer{PreUser: true, IdentityID: 7, Active: true}
			case "inactive":
				v = w.viewer("alice")
				v.Active = false
			default:
				v = w.viewer(who)
			}
			next.ServeHTTP(rw, r.WithContext(domain.WithViewer(r.Context(), v)))
		})
	}
	return web.New(web.Groups{API: w.handler}, web.Options{
		Session:   session,
		RateLimit: web.RateLimit(func() time.Time { return frozen }),
	})
}

// preview POSTs body to /api/v1/render through h as who.
func preview(h http.Handler, who string, body string, header ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/render", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if who != "" {
		r.Header.Set("X-Test-Viewer", who)
	}
	for i := 0; i+1 < len(header); i += 2 {
		r.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func renderJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// problemCode reads a problem response's type, which is its code.
func problemCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var p struct {
		Code string `json:"type"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("status %d is not a problem: %v\n%s", rec.Code, err, rec.Body)
	}
	return p.Code
}

// TestPreviewMatchesTheReadPath is AC1 and AC3: each source, posted to the
// thread and then previewed with its thread_id, renders to the same bytes the
// thread page serves, and the answer decodes strictly into dto.RenderResult,
// the type the create path answers with. The corpus covers what depends on
// the thread and the users table: a live quote, a quote of a deleted post, a
// quote of a post that is not there, a known and an unknown mention.
func TestPreviewMatchesTheReadPath(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed() // #1 alice, #2 scribe, #3 bob (deleted), #4 carol
	h := w.chain()
	corpus := []string{
		"The bill was the **smaller** half of it, see [the post](https://example.com/a?b=c).",
		"Agreed with @alice, and with @nobody-here, and @ALICE again.",
		"> [quote=#1]\n> what alice said\n\nand my reply to @carol",
		"> [quote=#3]\n> what bob said\n\nquoting a deleted post",
		"> [quote=#99]\n> nothing\n\nquoting a post that is not there",
		"```go\nfmt.Println(\"<script>\")\n```\n\n<script>alert(1)</script> a footnote[^1]\n\n[^1]: here",
	}
	first := len(w.pageOf(id).Posts) + 1
	for _, src := range corpus {
		w.post(id, "carol", src, 0)
	}
	posts := w.pageOf(id).Posts
	for i, src := range corpus {
		stored := posts[first-1+i]
		if stored.Ordinal != first+i {
			t.Fatalf("post %d is at ordinal %d", first+i, stored.Ordinal)
		}
		rec := preview(h, "carol", renderJSON(t, map[string]any{"source": src, "thread_id": id.String()}))
		got := decode[dto.RenderResult](t, rec)
		if got.HTML != stored.HTML {
			t.Errorf("corpus %d: the preview renders\n%s\nand the page serves\n%s", i, got.HTML, stored.HTML)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("corpus %d: Cache-Control %q", i, cc)
		}
	}

	// The structured facts are the render's.
	got := decode[dto.RenderResult](t, preview(h, "carol", renderJSON(t, map[string]any{
		"source":    "> [quote=#1]\n> x\n\n> [quote=#3]\n> y\n\n@alice and @bob and @nobody-here",
		"thread_id": id.String(),
		"reply_to":  2,
	})))
	want := `{"mentions":[{"handle":"alice"},{"handle":"bob"}],"quotes":[{"ordinal":1,"deleted":false},{"ordinal":3,"deleted":true}],"reply_to":2}`
	got.HTML = ""
	if b := renderJSON(t, got); !strings.HasSuffix(b, want[1:]) {
		t.Errorf("structured facts: %s, want %s", b, want)
	}
}

// pageOf reads the first page of a thread as a moderator, so nothing is
// withheld from the comparison.
func (w *world) pageOf(id threadid.ID) dto.ThreadPage {
	return page(w.t, w.get(w.viewer("bob"), "/api/v1/threads/"+id.String()))
}

// TestPreviewWithoutAThread: a new thread's composer has no thread to resolve
// against, so a quote is rendered optimistically as a link (render.Context),
// where the same source previewed in a thread without that post is not.
func TestPreviewWithoutAThread(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	h := w.chain()
	src := "> [quote=#99]\n> nothing\n"
	loose := decode[dto.RenderResult](t, preview(h, "carol", renderJSON(t, map[string]any{"source": src})))
	inThread := decode[dto.RenderResult](t, preview(h, "carol", renderJSON(t, map[string]any{"source": src, "thread_id": id.String()})))
	if len(loose.Quotes) != 1 || loose.Quotes[0].Ordinal != 99 || loose.HTML == inThread.HTML {
		t.Errorf("without a thread: %+v; in the thread: %+v", loose, inThread)
	}
	if loose.ReplyTo != nil || loose.Mentions == nil || inThread.Mentions == nil {
		t.Errorf("empty facts must be empty lists and a null reply_to: %+v", loose)
	}
}

// TestPreviewNeedsAMember is AC2, through the full chain.
func TestPreviewNeedsAMember(t *testing.T) {
	w := newWorld(t)
	w.seed()
	h := w.chain()
	body := `{"source":"hello"}`
	for _, c := range []struct {
		who  string
		want int
		code problem.Code
	}{
		{"", http.StatusUnauthorized, problem.CodeUnauthenticated},
		{"pre-user", http.StatusForbidden, problem.CodeForbidden},
		{"inactive", http.StatusUnauthorized, problem.CodeUnauthenticated},
	} {
		rec := preview(h, c.who, body)
		if rec.Code != c.want || problemCode(t, rec) != string(c.code) {
			t.Errorf("%q: %d %s, want %d %s", c.who, rec.Code, rec.Body, c.want, c.code)
		}
	}
	// A stranger is told they are one before anything about the body
	// (review 522).
	for _, c := range []struct {
		who, body string
		want      problem.Code
	}{
		{"", `not json`, problem.CodeUnauthenticated},
		{"", `{"source":"x","nope":1}`, problem.CodeUnauthenticated},
		{"", `{"source":"x","thread_id":"bad"}`, problem.CodeUnauthenticated},
		{"pre-user", `{}`, problem.CodeForbidden},
		{"pre-user", `{"source":"x","reply_to":1}`, problem.CodeForbidden},
		{"inactive", `{"source":"x"} trailing`, problem.CodeUnauthenticated},
	} {
		if rec := preview(h, c.who, c.body); problemCode(t, rec) != string(c.want) {
			t.Errorf("%q with %s: %d %s, want %s", c.who, c.body, rec.Code, rec.Body, c.want)
		}
	}
	if rec := preview(h, "alice", body); rec.Code != http.StatusOK {
		t.Errorf("a member: %d %s", rec.Code, rec.Body)
	}
}

// TestPreviewRequests: what the body and the thread can get wrong, and the
// status each is.
func TestPreviewRequests(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	h := w.chain()
	in := id.String()
	for _, c := range []struct {
		name, body string
		want       problem.Code
	}{
		{"not JSON", `source=hello`, problem.CodeBadRequest},
		{"an unknown field", `{"source":"x","body":"y"}`, problem.CodeBadRequest},
		{"no source", `{"thread_id":"` + in + `"}`, problem.CodeBadRequest},
		{"two values", `{"source":"x"}{"source":"y"}`, problem.CodeBadRequest},
		{"trailing garbage", `{"source":"x"} x`, problem.CodeBadRequest},
		{"a negative reply_to", `{"source":"x","thread_id":"` + in + `","reply_to":-1}`, problem.CodeBadRequest},
		{"a malformed thread", `{"source":"x","thread_id":"nope"}`, problem.CodeBadRequest},
		{"reply_to without a thread", `{"source":"x","reply_to":1}`, problem.CodeBadRequest},
		{"no such thread", `{"source":"x","thread_id":"` + threadid.ID(12345).String() + `"}`, problem.CodeNotFound},
		{"reply_to a post that is not there", `{"source":"x","thread_id":"` + in + `","reply_to":99}`, problem.CodeValidationFailed},
		{"reply_to zero", `{"source":"x","thread_id":"` + in + `","reply_to":0}`, problem.CodeValidationFailed},
	} {
		rec := preview(h, "carol", c.body)
		if got := problemCode(t, rec); got != string(c.want) {
			t.Errorf("%s: %d %s, want %s", c.name, rec.Code, rec.Body, c.want)
		}
	}
	// A reply to a deleted post is accepted, as render.Context says.
	got := decode[dto.RenderResult](t, preview(h, "carol", `{"source":"x","thread_id":"`+in+`","reply_to":3}`))
	if got.ReplyTo == nil || *got.ReplyTo != 3 {
		t.Errorf("reply_to a deleted post: %+v", got)
	}
	// A null is absent.
	if rec := preview(h, "carol", `{"source":"x","thread_id":null,"reply_to":null}`); rec.Code != http.StatusOK {
		t.Errorf("explicit nulls: %d %s", rec.Code, rec.Body)
	}
	// A form cannot reach it (docs/07 "CSRF posture").
	if rec := preview(h, "carol", `source=x`, "Content-Type", "application/x-www-form-urlencoded"); rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("a form post: %d %s", rec.Code, rec.Body)
	}
	// Not a GET.
	r := httptest.NewRequest(http.MethodGet, "/api/v1/render", nil)
	r.Header.Set("X-Test-Viewer", "carol")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if c := problemCode(t, rec); c != string(problem.CodeNotFound) && c != string(problem.CodeMethodNotAllowed) {
		t.Errorf("GET /api/v1/render reached the handler: %d %s", rec.Code, rec.Body)
	}
}

// TestPreviewHiddenThread: a thread the viewer cannot read is not-found, as
// on every thread endpoint, so a preview cannot probe for one.
func TestPreviewHiddenThread(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	w.hideForum()
	rec := preview(w.chain(), "carol", `{"source":"> [quote=#1]\n> x","thread_id":"`+id.String()+`"}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("a thread hidden from the viewer: %d %s", rec.Code, rec.Body)
	}
}

// TestPreviewLimits is AC4: the body limit is the post limit, whether the
// Content-Length says so or the body runs past it, and the endpoint is
// charged to the per-address write limiter.
func TestPreviewLimits(t *testing.T) {
	w := newWorld(t)
	w.seed()
	h := w.chain()

	// A body exactly at the limit is read; one byte over is refused.
	at := `{"source":"` + strings.Repeat("a", web.MaxPostBody-len(`{"source":""}`)) + `"}`
	if len(at) != web.MaxPostBody {
		t.Fatalf("the body is %d bytes", len(at))
	}
	if rec := preview(h, "carol", at); rec.Code != http.StatusOK {
		t.Errorf("a body of exactly MaxPostBody: %d %.200s", rec.Code, rec.Body)
	}
	over := at[:len(at)-2] + `a"}`
	if rec := preview(h, "carol", over); problemCode(t, rec) != string(problem.CodePayloadTooLarge) {
		t.Errorf("a Content-Length one over: %d %.200s", rec.Code, rec.Body)
	}
	// A chunked body declares no length, so only the read can catch it.
	r := httptest.NewRequest(http.MethodPost, "/api/v1/render", io.MultiReader(strings.NewReader(over)))
	r.ContentLength = -1
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Test-Viewer", "carol")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if problemCode(t, rec) != string(problem.CodePayloadTooLarge) {
		t.Errorf("a chunked body one over: %d %.200s", rec.Code, rec.Body)
	}

	// The write bucket: sixty from a fresh address, and the sixty-first
	// waits. The clock is frozen, so nothing refills in between.
	burst := func(addr string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/render", bytes.NewReader([]byte(`{"source":"hi"}`)))
		r.RemoteAddr = addr
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Test-Viewer", "carol")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}
	for i := range 60 {
		if rec := burst("198.51.100.7:4000"); rec.Code != http.StatusOK {
			t.Fatalf("request %d of the burst: %d %s", i+1, rec.Code, rec.Body)
		}
	}
	rec = burst("198.51.100.7:4000")
	if problemCode(t, rec) != string(problem.CodeRateLimited) || rec.Header().Get("Retry-After") == "" {
		t.Errorf("the sixty-first: %d %s, Retry-After %q", rec.Code, rec.Body, rec.Header().Get("Retry-After"))
	}
	if rec := burst("198.51.100.8:4000"); rec.Code != http.StatusOK {
		t.Errorf("another address is its own bucket: %d %s", rec.Code, fmt.Sprint(rec.Body))
	}
}

// hideForum makes the seeded forum restricted, which no seeded member has
// read on.
func (w *world) hideForum() {
	w.write(func(q *dbq.Queries) error {
		return q.SetForumVisibility(w.t.Context(), dbq.SetForumVisibilityParams{
			Visibility: domain.VisibilityRestricted, UpdatedAt: 2, ID: w.forum})
	})
}
