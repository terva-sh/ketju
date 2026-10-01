package threads

import (
	"context"
	"encoding/json"
	"maps"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/service/threads/lens"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// withResolvers serves exactly rs for the rest of the test.
func withResolvers(t *testing.T, rs map[lens.Kind]lensResolver) {
	saved := lensResolvers
	lensResolvers = rs
	t.Cleanup(func() { lensResolvers = saved })
}

// TestLensResolverRegistration is the extension point: a kind with no row in
// lensResolvers is the canonical page with lens: null, and the same request
// with a row is a lens page, whose block names the resolved kind and value
// and whose posts are the ones the filter kept.
func TestLensResolverRegistration(t *testing.T) {
	w := newRepliesWorld(t)
	c := w.cache()
	id := threadid.ID(uint64(1_757_500_000_000)<<16 | 1)
	w.thread(c, id, rand.New(rand.NewPCG(1, 2)), 12, true)
	s := w.service(c, nil)
	v := domain.Viewer{UserID: w.author, Role: domain.RoleMember, Active: true, SessionKey: "s"}
	canonical, err := s.View(t.Context(), v, id, Query{}, "")
	if err != nil {
		t.Fatal(err)
	}

	samples := map[lens.Kind]string{lens.Unread: "unread:3", lens.Author: "author:alice", lens.Chain: "chain:4",
		lens.Since: "since:0", lens.Mentions: "mentions", lens.Mod: "mod"}
	for kind, raw := range samples {
		without := maps.Clone(lensResolvers)
		delete(without, kind)
		withResolvers(t, without)
		got, err := s.View(t.Context(), v, id, Query{Lens: raw}, "")
		if err != nil {
			t.Fatalf("%s with no resolver: %v", raw, err)
		}
		if got.Body.Lens != nil || got.ETag != canonical.ETag {
			t.Errorf("%s with no resolver: lens %+v, etag %s; want the canonical page", raw, got.Body.Lens, got.ETag)
		}

		var seen lens.Spec
		with := maps.Clone(lensResolvers)
		with[kind] = func(_ context.Context, _ *Service, _ lensViewer, spec lens.Spec) (resolvedLens, error) {
			seen = spec
			return resolvedLens{kind: spec.Kind, value: "resolved", key: "k:" + spec.String(),
				filter: func(ix *threadlog.Index) (lens.Filter, error) {
					// The filter may read the index before it returns.
					return lens.Ordinals(uint32(ix.Len()), 2), nil
				}}, nil
		}
		withResolvers(t, with)
		got, err = s.View(t.Context(), v, id, Query{Lens: raw}, "")
		if err != nil {
			t.Fatalf("%s with a resolver: %v", raw, err)
		}
		if seen != lens.Parse(raw) {
			t.Errorf("%s: the resolver was handed %+v", raw, seen)
		}
		l := got.Body.Lens
		if l == nil || l.Kind != string(kind) || l.Value != "resolved" || l.Shown != 2 || l.Hidden != 10 {
			t.Fatalf("%s with a resolver: lens %+v", raw, l)
		}
		var ordinals []int
		for _, p := range got.Body.Posts {
			ordinals = append(ordinals, p.Ordinal)
		}
		if !slices.Equal(ordinals, []int{2, 12}) {
			t.Errorf("%s with a resolver: posts %v, want [2 12]", raw, ordinals)
		}
	}
}

// TestCanonicalPageUnchangedByTheFramework: with no lens, the page is the
// one it was before the framework: lens null, and the same bytes as a
// request with a lens the build does not serve.
func TestCanonicalPageUnchangedByTheFramework(t *testing.T) {
	w := newRepliesWorld(t)
	c := w.cache()
	id := threadid.ID(uint64(1_757_500_000_000)<<16 | 1)
	w.thread(c, id, rand.New(rand.NewPCG(3, 4)), 30, true)
	s := w.service(c, nil)
	withResolvers(t, map[lens.Kind]lensResolver{})
	for _, q := range []Query{{}, {Page: 2}, {Ordinal: 27}} {
		plain, err := s.View(t.Context(), domain.Anonymous(), id, q, "")
		if err != nil {
			t.Fatal(err)
		}
		q.Lens = "author:alice"
		lensed, err := s.View(t.Context(), domain.Anonymous(), id, q, "")
		if err != nil {
			t.Fatal(err)
		}
		a, _ := json.Marshal(plain.Body)
		b, _ := json.Marshal(lensed.Body)
		if plain.Body.Lens != nil || string(a) != string(b) || plain.ETag != lensed.ETag {
			t.Errorf("%+v: an unserved lens changed the page", q)
		}
	}
}

// TestUnreadLensKeyNamesBoundary: the unread lens's cache key names the
// boundary it resolved to, so two members at different positions never
// share an entry, even before the page's ordinal digest, which is keyed
// beside it, tells their pages apart (TKT-01M29CTY4J). A bare unread and
// unread:B at the same B resolve alike, and a viewer who is not a member
// resolves to the empty lens whatever position a row holds for them.
func TestUnreadLensKeyNamesBoundary(t *testing.T) {
	member := func(id int64) access {
		return access{viewer: domain.Viewer{UserID: id, Role: domain.RoleMember, Active: true}}
	}
	resolve := func(v lensViewer, raw string) resolvedLens {
		t.Helper()
		rl, err := resolveUnread(t.Context(), nil, v, lens.Parse(raw))
		if err != nil {
			t.Fatal(err)
		}
		return rl
	}
	at10, at20 := resolve(lensViewer{member(1), 10}, "unread"), resolve(lensViewer{member(2), 20}, "unread")
	if at10.key != "unread:10" || at10.value != "10" || at20.key != "unread:20" || at20.value != "20" {
		t.Fatalf("bare unread at 10 and 20 resolved to %q/%q and %q/%q", at10.key, at10.value, at20.key, at20.value)
	}
	if held := resolve(lensViewer{member(2), 20}, "unread:10"); held.key != at10.key || held.value != at10.value {
		t.Fatalf("unread:10 asked by a member at 20 resolved to %q/%q, want unread:10", held.key, held.value)
	}
	none := resolve(lensViewer{member(3), 0}, "unread")
	if none.key != "unread:0" || none.value != "0" {
		t.Fatalf("bare unread with no position resolved to %q/%q, want unread:0", none.key, none.value)
	}
	ix := threadlog.NewIndex(threadlog.IndexHeader{PostCount: 1}, []threadlog.IndexEntry{{Offset: 64, Revision: 1}})
	for _, v := range []domain.Viewer{domain.Anonymous(), {UserID: 4, Role: domain.RoleMember}} {
		rl := resolve(lensViewer{access{viewer: v}, 7}, "unread")
		f, err := rl.filter(ix)
		if err != nil || rl.value != "" || rl.key == at10.key || rl.key == none.key || lens.Apply(ix, f).Len() != 0 {
			t.Fatalf("%+v: bare unread resolved to %q/%q, want the empty lens", v, rl.key, rl.value)
		}
	}
}
