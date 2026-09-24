package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// addUsers creates users with a status each, beside the world's four.
func (w *world) addUsers(status string, handles ...string) {
	w.t.Helper()
	for _, h := range handles {
		w.write(func(q *dbq.Queries) error {
			id, err := q.CreateUser(w.t.Context(), dbq.CreateUserParams{Handle: h, DisplayName: "User " + h,
				Status: status, Role: domain.RoleMember, Kind: "human", CreatedAt: 1, UpdatedAt: 1})
			w.users[h] = id
			return err
		})
	}
}

// suggestWorld is the world with handles that test every edge of the match:
// '-' and a digit sort before letters, z is the last letter, "al-" ends in a '-' the mention
// parser drops, and the suspended and pending accounts are not members.
func suggestWorld(t *testing.T) (*world, http.Handler) {
	w := newWorld(t)
	w.addUsers("active", "al-x", "al9", "alex_k", "alz", "al-", "kate")
	w.addUsers("suspended", "alan")
	w.addUsers("pending", "alba")
	return w, w.chain()
}

func suggest(h http.Handler, who, query string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/users?"+query, nil)
	if who != "" {
		r.Header.Set("X-Test-Viewer", who)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func handles(s dto.UserSuggestions) string {
	var out []string
	for _, u := range s.Users {
		out = append(out, u.Handle)
	}
	return strings.Join(out, " ")
}

// TestSuggestions is AC1: a member gets up to limit active users whose handle
// starts with the prefix, case-insensitively and in handle order.
func TestSuggestions(t *testing.T) {
	_, h := suggestWorld(t)
	for _, c := range []struct{ query, want string }{
		{"prefix=al", "al-x al9 alex_k alice alz"},
		{"prefix=AL", "al-x al9 alex_k alice alz"},
		{"prefix=alz", "alz"},
		{"prefix=aL&limit=2", "al-x al9"},
		{"prefix=al-", "al-x"},
		{"prefix=alex_", "alex_k"},
		{"prefix=alice", "alice"},
		{"prefix=alicex", ""},
		{"prefix=", "al-x al9 alex_k alice alz bob carol kate"},
		{"prefix=&limit=20", "al-x al9 alex_k alice alz bob carol kate scribe"},
		{"prefix=k", "kate"},
		{"prefix=K", "kate"},
		// What no handle can start with is nobody, not an error.
		{"prefix=" + url.QueryEscape("K"), ""}, // the Kelvin sign, which ToLower folds to k
		{"prefix=_a", ""},
		{"prefix=-a", ""},
		{"prefix=" + url.QueryEscape("a%"), ""},
		{"prefix=" + url.QueryEscape("a{"), ""},
		{"prefix=" + url.QueryEscape("é"), ""},
		{"prefix=" + strings.Repeat("a", 31), ""},
	} {
		rec := suggest(h, "carol", c.query)
		got := decode[dto.UserSuggestions](t, rec)
		if handles(got) != c.want || got.Users == nil {
			t.Errorf("%s: %q (users %v), want %q", c.query, handles(got), got.Users, c.want)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("%s: Cache-Control %q", c.query, cc)
		}
	}
	// Only a GET.
	r := httptest.NewRequest(http.MethodPost, "/api/v1/users?prefix=al", strings.NewReader("{}"))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Test-Viewer", "carol")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if c := problemCode(t, rec); c != string(problem.CodeNotFound) && c != string(problem.CodeMethodNotAllowed) {
		t.Errorf("POST /api/v1/users reached the handler: %d %s", rec.Code, rec.Body)
	}

	got := decode[dto.UserSuggestions](t, suggest(h, "carol", "prefix=alice"))
	if want := (dto.Author{Handle: "alice", DisplayName: "Alice Nakamura", Role: domain.RoleMember, Kind: "human"}); got.Users[0] != want {
		t.Errorf("alice: %+v, want %+v", got.Users[0], want)
	}
}

// TestSuggestionLimit: the default is eight, and a larger limit is capped at
// twenty rather than refused.
func TestSuggestionLimit(t *testing.T) {
	w := newWorld(t)
	for i := range 25 {
		w.addUsers("active", fmt.Sprintf("u%02d", i))
	}
	h := w.chain()
	for _, c := range []struct {
		query string
		want  int
	}{
		{"prefix=u", 8}, {"prefix=u&limit=1", 1}, {"prefix=u&limit=20", 20}, {"prefix=u&limit=100", 20},
	} {
		if got := decode[dto.UserSuggestions](t, suggest(h, "carol", c.query)); len(got.Users) != c.want ||
			got.Users[0].Handle != "u00" {
			t.Errorf("%s: %d users from %q, want %d from u00", c.query, len(got.Users), handles(got), c.want)
		}
	}
}

// TestSuggestionsNeedAMember is AC1's other half, and the parameters a
// client can get wrong.
func TestSuggestionsNeedAMember(t *testing.T) {
	_, h := suggestWorld(t)
	for _, c := range []struct {
		who, query string
		want       problem.Code
	}{
		{"", "prefix=al", problem.CodeUnauthenticated},
		{"pre-user", "prefix=al", problem.CodeForbidden},
		{"inactive", "prefix=al", problem.CodeUnauthenticated},
		{"carol", "", problem.CodeBadRequest},
		{"carol", "limit=3", problem.CodeBadRequest},
		{"carol", "prefix=al&limit=0", problem.CodeBadRequest},
		{"carol", "prefix=al&limit=-1", problem.CodeBadRequest},
		{"carol", "prefix=al&limit=", problem.CodeBadRequest},
		{"carol", "prefix=al&limit=two", problem.CodeBadRequest},
	} {
		rec := suggest(h, c.who, c.query)
		if got := problemCode(t, rec); got != string(c.want) {
			t.Errorf("%q %s: %d %s, want %s", c.who, c.query, rec.Code, rec.Body, c.want)
		}
	}
}

// TestSuggestionsResolveAsMentions is AC2: every suggestion, inserted as the
// composer inserts it, renders as a mention of exactly that user through
// POST /render, which resolves handles as the read path does. The handle
// the query leaves out is shown to be one that could not have been
// mentioned: "@al-" is read as "@al", which is nobody.
func TestSuggestionsResolveAsMentions(t *testing.T) {
	w, h := suggestWorld(t)
	for i := range 3 {
		w.addUsers("active", fmt.Sprintf("x_%d-y", i))
	}
	seen := 0
	for _, prefix := range []string{"", "a", "al", "al-", "k", "x", "x_"} {
		got := decode[dto.UserSuggestions](t, suggest(h, "carol", "limit=20&prefix="+url.QueryEscape(prefix)))
		for _, u := range got.Users {
			seen++
			src := "thanks @" + u.Handle + " for this"
			r := decode[dto.RenderResult](t, preview(h, "carol", renderJSON(t, map[string]any{"source": src})))
			if len(r.Mentions) != 1 || r.Mentions[0].Handle != u.Handle ||
				!strings.Contains(r.HTML, `href="/u/`+u.Handle+`"`) {
				t.Errorf("suggested %q, but %q renders %+v", u.Handle, src, r)
			}
		}
	}
	if seen < 15 {
		t.Fatalf("only %d suggestions were checked", seen)
	}
	r := decode[dto.RenderResult](t, preview(h, "carol", `{"source":"thanks @al- for this"}`))
	if len(r.Mentions) != 0 {
		t.Errorf("@al- mentions %+v; the exclusion in UsersByHandlePrefix is no longer needed", r.Mentions)
	}
}
