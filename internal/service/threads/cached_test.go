package threads

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"math/rand/v2"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/service/respcache"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// TestCachedPayloadHoldsNoViewer is AC4 on the stored bytes themselves: a
// member with a read position and a watch views a page, and what the
// response cache holds for it is the posts and nothing of the viewer's.
func TestCachedPayloadHoldsNoViewer(t *testing.T) {
	w := newRepliesWorld(t)
	c := w.cache()
	id := threadid.ID(uint64(1_757_500_000_000)<<16 | 1)
	length := w.thread(c, id, rand.New(rand.NewPCG(1, 2)), 12, true)
	s := w.service(c, nil)
	s.Responses = respcache.New(1 << 20)
	if err := w.db.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		if err := q.UpsertReadPosition(t.Context(), dbq.UpsertReadPositionParams{
			UserID: w.author, ThreadID: int64(id), LastReadOrdinal: 5, LastReadAt: 1}); err != nil {
			return err
		}
		_, err := tx.ExecContext(t.Context(),
			`INSERT INTO watches (user_id, thread_id, level, created_at, updated_at) VALUES (?, ?, 'watch', 1, 1)`,
			w.author, int64(id))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	v := domain.Viewer{UserID: w.author, Role: domain.RoleMember, Active: true, SessionKey: "s"}
	res, err := s.View(t.Context(), v, id, Query{Page: 1}, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Body.Viewer.LastReadOrdinal != 5 || res.Body.Viewer.Watch != "watch" || !res.Body.Viewer.CanReply {
		t.Fatalf("the viewer block is %+v; the test needs one with something in it", res.Body.Viewer)
	}

	authors, err := s.users(t.Context(), map[int64]bool{w.author: true})
	if err != nil {
		t.Fatal(err)
	}
	digest, err := authorsDigest(authors)
	if err != nil {
		t.Fatal(err)
	}
	// The thread's twelve posts are page 1's ordinals.
	ordinals := make([]uint32, 12)
	for i := range ordinals {
		ordinals[i] = uint32(i) + 1
	}
	stored, ok := s.Responses.Get(respcache.Key{Thread: id, LogLength: length, Projected: length, Page: 1,
		PageSize: DefaultPageSize, Selection: ordinalsDigest(ordinals), Renderer: render.Version,
		Class: ClassMember, Authors: digest}, nil)
	if !ok {
		t.Fatalf("no entry under the key the page should have filled; stats %+v", s.Responses.Stats())
	}
	for _, field := range []string{`"viewer"`, `"last_read_ordinal"`, `"last_read_at"`, `"watch"`, `"can_reply"`, `"can_moderate"`} {
		if bytes.Contains(stored, []byte(field)) {
			t.Errorf("the cached payload carries %s: %s", field, stored)
		}
	}
	var posts []dto.Post
	if err := json.Unmarshal(stored, &posts); err != nil {
		t.Fatalf("the cached payload is not a list of posts: %v", err)
	}
	if want, _ := json.Marshal(res.Body.Posts); !bytes.Equal(stored, want) {
		t.Fatalf("the cached payload is not the page's posts\n got %s\nwant %s", stored, want)
	}
}

// TestAuthorsDigest: the digest does not depend on map order, and moves
// with any field of any author.
func TestAuthorsDigest(t *testing.T) {
	base := map[int64]dto.Author{
		1: {Handle: "alice", DisplayName: "Alice", Role: "member", Kind: "human"},
		2: {Handle: "bob", DisplayName: "Bob", Role: "moderator", Kind: "human"},
	}
	want, err := authorsDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	for range 20 {
		if got, _ := authorsDigest(base); got != want {
			t.Fatal("the digest depends on map order")
		}
	}
	for name, change := range map[string]func(*dto.Author){
		"handle":       func(a *dto.Author) { a.Handle = "alicia" },
		"display name": func(a *dto.Author) { a.DisplayName = "A." },
		"avatar":       func(a *dto.Author) { a.Avatar = "blob" },
		"role":         func(a *dto.Author) { a.Role = "admin" },
		"kind":         func(a *dto.Author) { a.Kind = "agent" },
	} {
		m := map[int64]dto.Author{1: base[1], 2: base[2]}
		a := m[1]
		change(&a)
		m[1] = a
		if got, _ := authorsDigest(m); got == want {
			t.Errorf("changing the %s left the digest alone", name)
		}
	}
	swapped := map[int64]dto.Author{1: base[2], 2: base[1]}
	if got, _ := authorsDigest(swapped); got == want {
		t.Error("swapping which user wrote which posts left the digest alone")
	}
}
