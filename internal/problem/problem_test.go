package problem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/events"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// TestEveryDeclaredCodeIsInTheCatalogue reads this package's own source and
// checks that every Code constant has a catalogue entry.
//
// A hand-maintained list here would not catch the failure it exists for: if
// somebody adds a constant and forgets the entry, they would forget the list
// too. Parsing the source means the check cannot drift, and the failure it
// prevents is a real one -- Write falls back to CodeInternal for an unknown
// code, so a missing entry turns a 404 into a silent 500.
func TestEveryDeclaredCodeIsInTheCatalogue(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "problem.go", nil, 0)
	if err != nil {
		t.Fatalf("parse problem.go: %v", err)
	}

	var declared []string
	ast.Inspect(f, func(n ast.Node) bool {
		gd, ok := n.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			return true
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			id, ok := vs.Type.(*ast.Ident)
			if !ok || id.Name != "Code" {
				continue
			}
			for _, v := range vs.Values {
				lit, ok := v.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("unquote %s: %v", lit.Value, err)
				}
				declared = append(declared, s)
			}
		}
		return true
	})

	if len(declared) == 0 {
		t.Fatal("found no Code constants in problem.go; this test is not measuring anything")
	}
	for _, d := range declared {
		if _, ok := catalogue[Code(d)]; !ok {
			t.Errorf("code %q is declared but has no catalogue entry, so Write would answer it with a 500", d)
		}
	}
	if len(declared) != len(catalogue) {
		t.Errorf("%d declared codes but %d catalogue entries; the catalogue has an entry nothing names",
			len(declared), len(catalogue))
	}
}

// TestEveryCatalogueEntryIsUsable checks the table itself rather than its
// callers: a status in range, and a title that is actually there.
func TestEveryCatalogueEntryIsUsable(t *testing.T) {
	for _, c := range Codes() {
		e := catalogue[c]
		if e.Status < 400 || e.Status > 599 {
			t.Errorf("%s has status %d; a problem detail is an error response", c, e.Status)
		}
		if strings.TrimSpace(e.Title) == "" {
			t.Errorf("%s has no title", c)
		}
		if !strings.HasSuffix(e.Title, ".") {
			t.Errorf("%s title %q does not read as a sentence", c, e.Title)
		}
	}
}

// TestNoInternalDetailReachesTheBody is AC3, and it is the test this package
// exists for.
//
// Each error below wraps a real domain sentinel in the kind of text a handler
// would actually produce -- a query, a file path, a constraint name. The body
// must contain none of it. Reading err.Error() into the response is a one-line
// change that would look like an improvement, and this is what stops it.
func TestNoInternalDetailReachesTheBody(t *testing.T) {
	secrets := []string{
		"SELECT * FROM identities WHERE provider_key",
		"/var/lib/ketju/threads/01J8.log",
		"UNIQUE constraint failed: identities.sub",
		"threadlog:",
		"goroutine 1 [running]",
	}

	errs := []error{
		fmt.Errorf("SELECT * FROM identities WHERE provider_key = ?: %w", threadlog.ErrChecksum),
		fmt.Errorf("open /var/lib/ketju/threads/01J8.log: %w", threadlog.ErrBadMagic),
		fmt.Errorf("UNIQUE constraint failed: identities.sub: %w", threadlog.ErrMalformed),
		fmt.Errorf("goroutine 1 [running]: %w", threadid.ErrTimestampRange),
		fmt.Errorf("SELECT * FROM identities WHERE provider_key = ?: %w", render.ErrTooLarge),
		Coded(CodeValidationFailed, errors.New("UNIQUE constraint failed: identities.sub")),
	}

	for i, err := range errs {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			rec := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/api/v1/threads", nil)
			WriteError(context.Background(), rec, r, discardLogger(), err)

			body := rec.Body.String()
			for _, s := range secrets {
				if strings.Contains(body, s) {
					t.Errorf("the response body leaked %q:\n%s", s, body)
				}
			}
			// Belt and braces: the whole error text, not just the samples.
			if strings.Contains(body, err.Error()) {
				t.Errorf("the response body is the error text:\n%s", body)
			}
		})
	}
}

// TestTheErrorStillReachesTheLog is the other half of the previous test. Not
// leaking the error to the client is only correct if it is not simply lost --
// otherwise the safe behaviour would be to discard errors entirely.
func TestTheErrorStillReachesTheLog(t *testing.T) {
	var buf strings.Builder
	log := slog.New(slog.NewTextHandler(&buf, nil))

	err := fmt.Errorf("open /var/lib/ketju/threads/01J8.log: %w", threadlog.ErrBadMagic)
	rec := httptest.NewRecorder()
	WriteError(context.Background(), rec, httptest.NewRequest(http.MethodGet, "/", nil), log, err)

	if !strings.Contains(buf.String(), "01J8.log") {
		t.Errorf("the error did not reach the log, so it is lost rather than protected:\n%s", buf.String())
	}
}

// TestDomainErrorsMapToTheirCode covers the table in domain.go. Each case
// names why that status and not another.
func TestDomainErrorsMapToTheirCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want Code
	}{
		{"a missing ordinal is a missing object", threadlog.ErrNoSuchOrdinal, CodeNotFound},
		{"a malformed id is the client's mistake", threadid.ErrMalformed, CodeBadRequest},
		{"posting to a closed thread conflicts with its state", threadlog.ErrActorClosed, CodeConflict},
		{"a source over the limit is too large", render.ErrTooLarge, CodePayloadTooLarge},
		{"a closed cache is retryable", threadcache.ErrClosed, CodeUnavailable},
		{"a closed bus is retryable", events.ErrClosed, CodeUnavailable},
		{"a deadline is retryable", context.DeadlineExceeded, CodeUnavailable},
		{"a cancelled request is retryable", context.Canceled, CodeUnavailable},
		{"a busy database is retryable", db.ErrBusy, CodeUnavailable},
		{"a database newer than the binary is unavailable", db.ErrSchemaNewer, CodeUnavailable},
		{"inconsistent migrations are a server fault", db.ErrMigrations, CodeInternal},
		{"corruption is a server fault", threadlog.ErrChecksum, CodeInternal},
		{"an unknown record type is a server fault", threadlog.ErrUnknownRecordType, CodeInternal},
		{"an unmapped error is a server fault", errors.New("something new"), CodeInternal},
		{"a nil error is a bug, not a success", nil, CodeInternal},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Wrapped, because that is how a handler would hand it over.
			err := tc.err
			if err != nil {
				err = fmt.Errorf("handling request: %w", err)
			}
			if got := FromError(err); got != tc.want {
				t.Errorf("FromError = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestACodedErrorOverridesTheTable checks that an explicit tag beats a
// sentinel the error happens to wrap. A handler that validated an input and
// said so is more specific than the storage error underneath it.
func TestACodedErrorOverridesTheTable(t *testing.T) {
	// Wraps ErrNoSuchOrdinal, which the table maps to not-found.
	err := Coded(CodeForbidden, fmt.Errorf("reply target: %w", threadlog.ErrNoSuchOrdinal))
	if got := FromError(err); got != CodeForbidden {
		t.Errorf("FromError = %s, want the explicit tag %s to win", got, CodeForbidden)
	}
	// The wrapped error is still reachable, so callers can still inspect it.
	if !errors.Is(err, threadlog.ErrNoSuchOrdinal) {
		t.Error("Coded broke the error chain; errors.Is no longer finds the cause")
	}
}

// TestACodedErrorWithAnUnknownCodeIsInternal stops a typo from producing a
// response whose type a client cannot switch on.
func TestACodedErrorWithAnUnknownCodeIsInternal(t *testing.T) {
	if got := FromError(Coded(Code("not-a-real-code"), errors.New("x"))); got != CodeInternal {
		t.Errorf("FromError = %s, want %s", got, CodeInternal)
	}
}

func TestCodedOfNilIsNil(t *testing.T) {
	if err := Coded(CodeForbidden, nil); err != nil {
		t.Errorf("Coded(_, nil) = %v, want nil", err)
	}
}

// TestAnUnknownCodeStillProducesASwitchableType checks Write's fallback. A
// blank type would be worse than a wrong one: the client's switch would have
// no arm for it and would fall through to its generic handler with no idea
// why.
func TestAnUnknownCodeStillProducesASwitchableType(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, httptest.NewRequest(http.MethodGet, "/", nil), Code("nonsense"))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	var p Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if p.Type != CodeInternal {
		t.Errorf("type = %q, want %q", p.Type, CodeInternal)
	}
}

// TestTheStatusIsRepeatedInTheBody is RFC 9457's reason for the member: a body
// copied out of its response is still self-describing.
func TestTheStatusIsRepeatedInTheBody(t *testing.T) {
	for _, c := range Codes() {
		rec := httptest.NewRecorder()
		Write(rec, httptest.NewRequest(http.MethodGet, "/", nil), c)

		var p Problem
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
			t.Fatalf("%s: body is not JSON: %v", c, err)
		}
		if p.Status != rec.Code {
			t.Errorf("%s: body says %d, response says %d", c, p.Status, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != ContentType {
			t.Errorf("%s: content type = %q, want %q", c, ct, ContentType)
		}
		if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
			t.Errorf("%s: cache-control = %q, want no-store", c, cc)
		}
	}
}

// TestAHeadResponseCarriesNoBody keeps the Content-Length honest.
func TestAHeadResponseCarriesNoBody(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, httptest.NewRequest(http.MethodHead, "/api/v1/threads", nil), CodeNotFound)

	if rec.Body.Len() != 0 {
		t.Errorf("HEAD produced a body: %s", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != ContentType {
		t.Errorf("HEAD dropped the content type: %q", ct)
	}
}

// TestRetryAfterIsWholeSeconds covers the header docs/07 promises with a 429.
func TestRetryAfterIsWholeSeconds(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{30 * time.Second, "30"},
		{1500 * time.Millisecond, "2"}, // rounds
		{0, ""},                        // no header rather than "0"
		{-5 * time.Second, ""},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		WriteRetryAfter(rec, httptest.NewRequest(http.MethodPost, "/", nil), CodeRateLimited, tc.in)

		if got := rec.Header().Get("Retry-After"); got != tc.want {
			t.Errorf("Retry-After for %v = %q, want %q", tc.in, got, tc.want)
		}
		if rec.Code != http.StatusTooManyRequests {
			t.Errorf("status = %d, want 429", rec.Code)
		}
	}
}

// TestUnmappedErrorsAreLoggedLouder: an unmapped error means the table has a
// gap, which is a different event from a client sending a bad id. If both
// logged at the same level the gap would never be noticed.
func TestUnmappedErrorsAreLoggedLouder(t *testing.T) {
	levelFor := func(err error) string {
		var buf strings.Builder
		log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		WriteError(context.Background(), httptest.NewRecorder(),
			httptest.NewRequest(http.MethodGet, "/", nil), log, err)
		for _, f := range strings.Fields(buf.String()) {
			if strings.HasPrefix(f, "level=") {
				return strings.TrimPrefix(f, "level=")
			}
		}
		return ""
	}

	if got := levelFor(errors.New("nobody has classified this")); got != "ERROR" {
		t.Errorf("an unmapped error logged at %q, want ERROR", got)
	}
	if got := levelFor(threadid.ErrMalformed); got != "WARN" {
		t.Errorf("a mapped client error logged at %q, want WARN", got)
	}
}

// TestWriteErrorSurvivesANilLogger keeps an error path from becoming a second
// crash, which is the worst possible moment for one.
func TestWriteErrorSurvivesANilLogger(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(context.Background(), rec, httptest.NewRequest(http.MethodGet, "/", nil),
		nil, errors.New("boom"))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(new(strings.Builder), nil))
}
