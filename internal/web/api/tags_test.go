package api

import (
	"bytes"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for a thread's tags on create (TKT-01M3B12ZZ4): docs/04
// `thread_tags`, docs/08 "Thread tags" and docs/07's POST
// /forums/{slug}/threads row.

// createdTags is the tags of the ThreadCreated record that opens id's log.
func (w *world) createdTags(id threadid.ID) []string {
	w.t.Helper()
	f, err := os.ReadFile(id.LogPath(w.root))
	if err != nil {
		w.t.Fatal(err)
	}
	var tags []string
	found := false
	if err := threadlog.ScanFrames(bytes.NewReader(f), int64(len(f)), func(r threadlog.ScanRecord) bool {
		if r.Err != nil {
			w.t.Fatal(r.Err)
		}
		if r.Header.Type != threadlog.TypeThreadCreated {
			return true
		}
		v, err := threadlog.DecodeBody(r.Header.Type, r.Header.Encoding, r.Body)
		if err != nil {
			w.t.Fatal(err)
		}
		tags, found = v.(*threadlog.ThreadCreatedBody).Tags, true
		return false
	}); err != nil {
		w.t.Fatal(err)
	}
	if !found {
		w.t.Fatalf("%s's log has no ThreadCreated", id)
	}
	return tags
}

// listedTags is the tags the general forum's thread list shows for id, as
// who reads it.
func (w *world) listedTags(who string, id string) []string {
	w.t.Helper()
	l := decode[dto.ThreadList](w.t, w.get(w.viewer(who), threadsPath("general")))
	for _, r := range l.Threads {
		if r.ID == id {
			return r.Tags
		}
	}
	w.t.Fatalf("thread %s is not in the general forum's list", id)
	return nil
}

// tagsAgree checks that the create answer, the thread page, the forum's
// thread list and the log's ThreadCreated all hold want.
func (w *world) tagsAgree(t *testing.T, res dto.CreatedThread, want []string) {
	t.Helper()
	id, err := threadid.Parse(res.Thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Thread.Tags, want) {
		t.Errorf("the answer's tags are %q, want %q", res.Thread.Tags, want)
	}
	if got := page(t, newHandlerFor(w).get(w.viewer("carol"), "/api/v1/threads/"+res.Thread.ID)).Thread.Tags; !slices.Equal(got, want) {
		t.Errorf("the thread page's tags are %q, want %q", got, want)
	}
	if got := w.listedTags("carol", res.Thread.ID); !slices.Equal(got, want) {
		t.Errorf("the thread list's tags are %q, want %q", got, want)
	}
	if got := w.createdTags(id); !slices.Equal(got, want) {
		t.Errorf("the log's ThreadCreated holds %q, want %q", got, want)
	}
}

// TestCreateThreadWithTags: tags are trimmed, folded, kept once and sorted,
// and the answer, the page, the list and the log agree.
func TestCreateThreadWithTags(t *testing.T) {
	w := newWorld(t)
	res := created[dto.CreatedThread](t, send(w.chain(), "carol", http.MethodPost, threadsPath("general"),
		`{"title":"Raised beds","source":"Cedar or larch?","tags":["Garden"," long-read","garden","GARDEN "]}`))
	w.tagsAgree(t, res, []string{"garden", "long-read"})

	// No tags, an empty list and null are all none.
	for _, tags := range []string{``, `,"tags":[]`, `,"tags":null`} {
		res := created[dto.CreatedThread](t, send(w.chain(), "carol", http.MethodPost, threadsPath("general"),
			`{"title":"Untagged","source":"s"`+tags+`}`))
		if res.Thread.Tags == nil || len(res.Thread.Tags) != 0 {
			t.Errorf("%q: tags %#v, want []", tags, res.Thread.Tags)
		}
	}

	// Exactly eight distinct tags, among more entries, is fine.
	res = created[dto.CreatedThread](t, send(w.chain(), "carol", http.MethodPost, threadsPath("general"),
		`{"title":"Eight","source":"s","tags":["h","g","f","e","d","c","b","a","A","H"]}`))
	w.tagsAgree(t, res, []string{"a", "b", "c", "d", "e", "f", "g", "h"})
}

// TestCreateThreadSixSeedTagsCreatable: the six tags the e2e seed writes
// straight to a log (cmd/ketju/e2e.go seedLongTags, copied here because
// that file builds only with -tags e2e) are accepted through the API too.
func TestCreateThreadSixSeedTagsCreatable(t *testing.T) {
	w := newWorld(t)
	res := created[dto.CreatedThread](t, send(w.chain(), "carol", http.MethodPost, threadsPath("general"),
		`{"title":"Comparing rain barrels","source":"s","tags":["garden","water","hardware","long-read","photos","budgets"]}`))
	w.tagsAgree(t, res, []string{"budgets", "garden", "hardware", "long-read", "photos", "water"})
}

// TestCreateThreadTagRefusals: each refusal names the field and code docs/07
// gives, and none of them writes a thread.
func TestCreateThreadTagRefusals(t *testing.T) {
	w := newWorld(t)
	nine := `"a","b","c","d","e","f","g","h","i"`
	for _, c := range []struct{ name, tags, want string }{
		{"a number", `[1]`, "validation-failed: tags.0 wrong_type"},
		{"null in the list", `["ok",null]`, "validation-failed: tags.1 wrong_type"},
		{"a list in the list", `["ok",["x"]]`, "validation-failed: tags.1 wrong_type"},
		{"an object", `{"a":"b"}`, "validation-failed: tags wrong_type"},
		{"a string", `"garden"`, "validation-failed: tags wrong_type"},
		{"an empty tag", `[""]`, "validation-failed: tags.0 invalid_tag"},
		{"spaces only", `["   "]`, "validation-failed: tags.0 invalid_tag"},
		{"an underscore", `["ok","a_b"]`, "validation-failed: tags.1 invalid_tag"},
		{"a double hyphen", `["a--b"]`, "validation-failed: tags.0 invalid_tag"},
		{"a leading hyphen", `["-a"]`, "validation-failed: tags.0 invalid_tag"},
		{"non-ASCII", `["café"]`, "validation-failed: tags.0 invalid_tag"},
		{"the Kelvin sign", `["K"]`, "validation-failed: tags.0 invalid_tag"},
		{"25 characters", `["` + strings.Repeat("x", 25) + `"]`, "validation-failed: tags.0 too_long"},
		{"nine tags", `[` + nine + `]`, "validation-failed: tags too_many"},
		{"a mix", `["ok","a b","` + strings.Repeat("y", 30) + `","fine","Bad!"]`,
			"validation-failed: tags.1 invalid_tag, tags.2 too_long, tags.4 invalid_tag"},
		{"a mix past the count", `[` + nine + `,"x_y"]`, "validation-failed: tags.9 invalid_tag, tags too_many"},
	} {
		rec := send(w.chain(), "carol", http.MethodPost, threadsPath("general"), `{"title":"t","source":"s","tags":`+c.tags+`}`)
		if got := fieldErrors(t, rec); got != c.want {
			t.Errorf("%s: %d %s, want %s", c.name, rec.Code, got, c.want)
		}
	}
	if n := len(w.events.list()); n != 0 {
		t.Errorf("the refusals published %d events", n)
	}
	logs := 0
	if err := walkLogs(w.root, func() { logs++ }); err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	if logs != 0 {
		t.Errorf("%d thread logs on disk after the refusals, want none", logs)
	}
}

// TestReservedTagOnCreate: docs/08 "Thread tags". A reserved tag is
// accepted from a moderator of the forum, by role or through a group, and
// from an agent account. Anyone else, a moderator of another forum included,
// is refused with reserved_tag at the tag's index. The decision is perm's at
// the time of the request, not the session's.
func TestReservedTagOnCreate(t *testing.T) {
	w := newWorld(t)
	var elsewhere int64
	w.write(func(q *dbq.Queries) error {
		var err error
		elsewhere, err = q.CreateForum(t.Context(), dbq.CreateForumParams{Slug: "elsewhere", Name: "Elsewhere",
			Visibility: domain.VisibilityPublic, CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	w.forumModerator("otto", elsewhere)

	// gina moderates general through a group of her own.
	w.addUsers("active", "gina")
	var g int64
	w.write(func(q *dbq.Queries) error {
		var err error
		g, err = q.CreateGroup(t.Context(), dbq.CreateGroupParams{Name: "Garden mods", Slug: "garden-mods", CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	e := perm.New(w.db)
	if err := e.AddMember(t.Context(), audit.By{}, g, w.users["gina"]); err != nil {
		t.Fatal(err)
	}
	if err := e.Grant(t.Context(), audit.By{}, w.forum, g, domain.PermModerate); err != nil {
		t.Fatal(err)
	}

	body := `{"title":"Nightly digest","source":"s","tags":["digest","Agent"]}`
	for who, ok := range map[string]bool{"bob": true, "gina": true, "scribe": true, "carol": false, "otto": false} {
		rec := send(w.chain(), who, http.MethodPost, threadsPath("general"), body)
		if !ok {
			if got := fieldErrors(t, rec); got != "validation-failed: tags.1 reserved_tag" {
				t.Errorf("%s: %d %s, want reserved_tag on tags.1", who, rec.Code, got)
			}
			continue
		}
		res := created[dto.CreatedThread](t, rec)
		if !slices.Equal(res.Thread.Tags, []string{"agent", "digest"}) {
			t.Errorf("%s: tags %q", who, res.Thread.Tags)
		}
	}

	// Every reserved tag in the request is named, and a reserved tag beside
	// a malformed one is listed with it, in index order.
	rec := send(w.chain(), "carol", http.MethodPost, threadsPath("general"), `{"title":"t","source":"s","tags":["agent","a b","AGENT"]}`)
	if got := fieldErrors(t, rec); got != "validation-failed: tags.0 reserved_tag, tags.1 invalid_tag, tags.2 reserved_tag" {
		t.Errorf("two reserved tags and a bad one: %s", got)
	}

	// Once gina's grant is revoked, her next request is refused.
	if err := e.Revoke(t.Context(), audit.By{}, w.forum, g, domain.PermModerate); err != nil {
		t.Fatal(err)
	}
	rec = send(w.chain(), "gina", http.MethodPost, threadsPath("general"), body)
	if got := fieldErrors(t, rec); got != "validation-failed: tags.1 reserved_tag" {
		t.Errorf("gina after the revoke: %d %s", rec.Code, got)
	}

	// Three threads were created, two records each, and nothing else.
	if n := len(w.events.list()); n != 6 {
		t.Errorf("%d events, want the three accepted threads' 6", n)
	}
}

// TestCreateThreadTagsSurviveReindex: the tags are the log's, so a
// projection rebuilt from the logs lists the same ones.
func TestCreateThreadTagsSurviveReindex(t *testing.T) {
	w := newWorld(t)
	res := created[dto.CreatedThread](t, send(w.chain(), "carol", http.MethodPost, threadsPath("general"),
		`{"title":"Seed swap","source":"s","tags":["Seeds","swap","seeds"]}`))
	want := []string{"seeds", "swap"}
	w.tagsAgree(t, res, want)
	w.reindex()
	w.tagsAgree(t, res, want)
}
