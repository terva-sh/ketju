package threadlog

import (
	"bytes"
	"flag"
	"fmt"
	"math/rand"
	"testing"
	"time"
)

// The index is built two ways: replayed from the log when it is missing or
// stale, and maintained incrementally by the actor as records are appended.
// The whole design rests on those two agreeing. If they ever diverge, either
// the index lies or a reindex loses data, and both are silent in production
// until somebody opens a thread and finds a post that will not load.
//
// Apply is shared by both paths so they agree by construction. This suite is
// what checks that the construction actually holds, over sequences nobody
// thought to write by hand.
//
// What it cannot catch is worth stating. Because both paths call the same
// Apply, a bug inside Apply changes both identically and every comparison here
// still passes — verified by mutating Apply and watching the suite stay green.
// What it does catch is everything around the fold: the actor's ordinal and
// revision assignment, the offsets it applies against, the header it writes,
// and the sidecar it maintains. Mutating the actor to apply a record one byte
// off, or to write a sidecar entry one slot along, is caught on the first
// sequence.
//
// So this is a test of the two paths agreeing, not of either being right.
// Apply's own correctness is held by the unit tests beside it, and by the
// crash and recovery suites that check the log against what was acknowledged.

var (
	propCases = flag.Int("prop.cases", 300,
		"number of random record sequences to check for replay equivalence")
	propLen = flag.Int("prop.len", 60,
		"maximum records in one generated sequence")
	propSeed = flag.Int64("prop.seed", 0,
		"seed for the sequence generator; 0 picks one from the clock")
)

// Criterion: random sequences of every record type produce byte-identical
// indexes from replay and from incremental maintenance.
//
// Bounded by default so it belongs in CI. A longer run is -prop.cases=50000,
// and a failure prints the exact flags that reproduce it.
func TestReplayEqualsIncrementalMaintenance(t *testing.T) {
	base := *propSeed
	if base == 0 {
		base = time.Now().UnixNano()
	}
	t.Logf("base seed %d (-prop.seed=%d to repeat this run)", base, base)

	for i := range *propCases {
		seed := base + int64(i)
		if err := checkOneSequence(seed, *propLen); err != nil {
			t.Fatalf("sequence %d of %d disagreed.\n%v\n\nreproduce with:\n  go test ./internal/store/threadlog/ -run TestReplayEqualsIncrementalMaintenance -prop.seed=%d -prop.cases=1",
				i+1, *propCases, err, seed)
		}
	}
}

// generated is one record the generator decided to append, kept so a failure
// can describe the sequence that produced it.
type generated struct {
	kind string
	ord  uint32
}

// checkOneSequence builds one random sequence, then compares the actor's index
// with one replayed from the log the actor wrote.
func checkOneSequence(seed int64, maxLen int) error {
	r := rand.New(rand.NewSource(seed))

	rec0 := &recorder{}
	lg := &fakeLog{rec: rec0, data: NewFileHeader(1, 1_700_000_000_000, 7).AppendTo(nil)}
	ixf := &fakeIndex{rec: rec0}
	a := NewActor(&Index{Header: IndexHeader{ThreadID: 1}}, int64(len(lg.data)), lg, ixf, SyncNever)

	n := 1 + r.Intn(maxLen)
	script := make([]generated, 0, n)

	// posts is the number of posts appended so far, which is also the highest
	// legal ordinal. Tracking it here rather than asking the index keeps the
	// generator honest about what a caller could actually have known.
	var posts uint32

	for range n {
		app, g, ok := nextRecord(r, posts)
		if !ok {
			continue
		}
		res, err := a.Append(app)
		if err != nil {
			a.Close()
			return fmt.Errorf("appending %s (ordinal %d) after %d records: %w", g.kind, g.ord, len(script), err)
		}
		if app.Type == TypePost {
			posts = res.Ordinal
			g.ord = res.Ordinal
		}
		script = append(script, g)
		// Nothing is appended after a ThreadArchived (TKT-01M29CRJ64).
		if app.Type == TypeThreadArchived {
			break
		}
	}

	if err := a.Close(); err != nil {
		return fmt.Errorf("closing the actor: %w", err)
	}

	incremental := a.Index().AppendTo(nil)

	// Replay from the log alone. Passing a nil index is what a missing or
	// unusable sidecar looks like, which is the case the replay path exists
	// for.
	logBytes := lg.bytes()
	rec, err := Open(bytes.NewReader(logBytes), int64(len(logBytes)), nil)
	if err != nil {
		return fmt.Errorf("replaying %d records: %w\n%s", len(script), err, describe(script))
	}
	if rec.Outcome != Rebuilt {
		return fmt.Errorf("replay outcome is %v, want Rebuilt; the test is not exercising replay", rec.Outcome)
	}
	replayed := rec.Index.AppendTo(nil)
	// The rebuild is an index ketju writes too (threadcache writes it back),
	// so the parser, entry check included, must take it (TKT-01M3VRFRFW).
	if _, err := ParseIndex(replayed); err != nil {
		return fmt.Errorf("parsing the rebuilt index: %w\n%s", err, describe(script))
	}

	if !bytes.Equal(incremental, replayed) {
		return fmt.Errorf("index mismatch over %d records\n%s\n%s",
			len(script), describe(script), diffIndexes(a.Index(), rec.Index))
	}

	// The sidecar the actor wrote must also agree. It is a third copy of the
	// same facts, and the one an operator's next start will actually read.
	onDisk, err := ParseIndex(ixf.bytes())
	if err != nil {
		return fmt.Errorf("parsing the index the actor wrote: %w\n%s", err, describe(script))
	}
	if got := onDisk.AppendTo(nil); !bytes.Equal(got, incremental) {
		return fmt.Errorf("the index file disagrees with the in-memory index over %d records\n%s\n%s",
			len(script), describe(script), diffIndexes(a.Index(), onDisk))
	}

	return nil
}

// nextRecord picks the next append. posts is how many posts exist already,
// which bounds what an edit, delete or restore may legally name.
//
// Criterion: sequences include edits and deletes of already-edited posts, and
// out-of-order ordinal references. Both fall out of choosing the ordinal
// uniformly from everything that exists rather than from the tail — a post is
// as likely to be edited twice, or edited long after later posts arrived, as
// it is to be touched once in order.
func nextRecord(r *rand.Rand, posts uint32) (Append, generated, bool) {
	ts := int64(1_700_000_000_000) + int64(r.Intn(1<<20))
	author := uint64(1 + r.Intn(5))

	// Weighted so a sequence accumulates posts to operate on, while still
	// spending most of its length on the mutations that are interesting.
	roll := r.Intn(100)
	switch {
	case posts == 0 || roll < 35:
		return Append{
			Type: TypePost, Author: author, TimeMS: ts,
			Flags: FrameFlags(r.Intn(2)), // exercises the machine-authored bit
			Body: &PostBody{
				Source:      fmt.Sprintf("post text %d", r.Intn(1<<16)),
				ReplyTo:     replyTo(r, posts),
				Mentions:    maybeMentions(r),
				Attachments: maybeAttachments(r),
			},
		}, generated{kind: "Post"}, true

	case roll < 60:
		ord := 1 + uint32(r.Intn(int(posts)))
		return Append{
			Type: TypeEdit, Ordinal: ord, Author: author, TimeMS: ts,
			Body: &EditBody{Source: fmt.Sprintf("edited %d", r.Intn(1<<16))},
		}, generated{kind: "Edit", ord: ord}, true

	case roll < 75:
		ord := 1 + uint32(r.Intn(int(posts)))
		return Append{
			Type: TypeDelete, Ordinal: ord, Author: author, TimeMS: ts,
			// A third are a moderator's, which hide rather than delete
			// (TKT-01M29CRJ5J), and a third the system's auto-hide
			// (TKT-01M29CP2PJ).
			Body: deleteBody(r.Intn(3)),
		}, generated{kind: "Delete", ord: ord}, true

	case roll < 85:
		ord := 1 + uint32(r.Intn(int(posts)))
		return Append{
			Type: TypeRestore, Ordinal: ord, Author: author, TimeMS: ts,
			Body: &RestoreBody{Reason: "appealed"},
		}, generated{kind: "Restore", ord: ord}, true

	default:
		app, kind := threadEvent(r, ts, author)
		return app, generated{kind: kind}, true
	}
}

// threadEvent returns one of the thread-level records. None of them touches a
// post entry, so every one is a chance for the two paths to disagree about
// doing nothing — which is exactly the kind of difference that would otherwise
// go unnoticed.
func threadEvent(r *rand.Rand, ts int64, author uint64) (Append, string) {
	type ev struct {
		t    RecordType
		name string
		body any
	}
	all := []ev{
		{TypeThreadCreated, "ThreadCreated", &ThreadCreatedBody{Title: "t", Slug: "s", ForumID: 7}},
		{TypeThreadRenamed, "ThreadRenamed", &ThreadRenamedBody{Title: "new", Slug: "new"}},
		{TypeThreadMoved, "ThreadMoved", &ThreadMovedBody{ForumID: uint64(r.Intn(9))}},
		{TypeThreadLocked, "ThreadLocked", &ThreadLockedBody{Reason: "heated"}},
		{TypeThreadUnlocked, "ThreadUnlocked", &ThreadUnlockedBody{Reason: "cooled"}},
		{TypeThreadPinned, "ThreadPinned", &ThreadPinnedBody{Scope: "forum", Until: ts}},
		{TypeThreadUnpinned, "ThreadUnpinned", &ThreadUnpinnedBody{Scope: "forum"}},
		{TypeThreadTagged, "ThreadTagged", &ThreadTaggedBody{Tags: []string{"a", "b"}}},
		{TypeThreadArchived, "ThreadArchived", &ThreadArchivedBody{Reason: "done"}},
	}
	e := all[r.Intn(len(all))]
	return Append{Type: e.t, Author: author, TimeMS: ts, Body: e.body}, e.name
}

func replyTo(r *rand.Rand, posts uint32) uint32 {
	if posts == 0 || r.Intn(3) != 0 {
		return 0
	}
	return 1 + uint32(r.Intn(int(posts)))
}

func maybeMentions(r *rand.Rand) []uint64 {
	if r.Intn(4) != 0 {
		return nil
	}
	out := make([]uint64, 1+r.Intn(3))
	for i := range out {
		out[i] = uint64(r.Intn(1000))
	}
	return out
}

func maybeAttachments(r *rand.Rand) []string {
	if r.Intn(5) != 0 {
		return nil
	}
	out := make([]string, 1+r.Intn(2))
	for i := range out {
		out[i] = fmt.Sprintf("sha256:%064x", r.Intn(1<<30))
	}
	return out
}

func describe(script []generated) string {
	var b bytes.Buffer
	b.WriteString("sequence:")
	for i, g := range script {
		if i%8 == 0 {
			b.WriteString("\n  ")
		}
		if g.ord != 0 {
			fmt.Fprintf(&b, "%s(%d) ", g.kind, g.ord)
		} else {
			fmt.Fprintf(&b, "%s ", g.kind)
		}
	}
	return b.String()
}

// diffIndexes names the first field that differs, because "1840 bytes differ"
// sends whoever reads it back to a hex dump.
func diffIndexes(a, b *Index) string {
	if a.Header != b.Header {
		return fmt.Sprintf("headers differ:\n  incremental %+v\n  replayed    %+v", a.Header, b.Header)
	}
	if a.Len() != b.Len() {
		return fmt.Sprintf("post counts differ: incremental %d, replayed %d", a.Len(), b.Len())
	}
	for i := 1; i <= a.Len(); i++ {
		ea, err1 := a.Entry(uint32(i))
		eb, err2 := b.Entry(uint32(i))
		if err1 != nil || err2 != nil {
			return fmt.Sprintf("entry %d unreadable: %v / %v", i, err1, err2)
		}
		if ea != eb {
			return fmt.Sprintf("entry %d differs:\n  incremental %+v\n  replayed    %+v", i, ea, eb)
		}
	}
	return "indexes compare equal field by field but their bytes differ, which means AppendTo is not a function of the fields"
}

// A sequence with no posts at all still has to agree. Thread events change no
// entry, so both paths must produce an empty index rather than one of them
// inventing something.
func TestThreadEventsOnlyStillAgree(t *testing.T) {
	testThreadEventsOnlyStillAgree(t)
}

// Criterion (TKT-01M3VRFRFW): ParseIndex refuses an entry that cannot
// describe a frame in the log its header covers, and no index ketju writes
// is refused. checkOneSequence parses the index the actor wrote and the one
// a full rebuild gives, over the property test's random sequences of posts,
// edits, deletes, restores and thread events; a redacted thread and one of
// thread events alone parse as well.
func TestEveryWrittenIndexPassesTheEntryCheck(t *testing.T) {
	base := *propSeed
	if base == 0 {
		base = time.Now().UnixNano()
	}
	t.Logf("base seed %d (-prop.seed=%d to repeat this run)", base, base)
	for i := range *propCases {
		if err := checkOneSequence(base+int64(i), *propLen); err != nil {
			t.Fatalf("sequence %d: %v\n\nreproduce with -prop.seed=%d -prop.cases=1", i+1, err, base+int64(i))
		}
	}

	lg, _ := threadWith(t, 5,
		Append{Type: TypeEdit, Ordinal: 2, Author: 42, Body: &EditBody{Source: "two, edited"}},
		Append{Type: TypeEdit, Ordinal: 4, Author: 42, Body: &EditBody{Source: "four, edited"}})
	raw := lg.bytes()
	var out bytes.Buffer
	if _, err := RedactPosts(bytes.NewReader(raw), int64(len(raw)), &out, []uint32{2, 3},
		RedactOptions{TimeMS: 1_800_000_000_000, Operator: 9, Reason: "court order", Journal: "01K6DHZ3Q8N6T2V4X9B7M5C1RE"}); err != nil {
		t.Fatal(err)
	}
	red, err := Open(bytes.NewReader(out.Bytes()), int64(out.Len()), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseIndex(red.Index.AppendTo(nil)); err != nil {
		t.Errorf("the index of a redacted thread: %v", err)
	}

	testThreadEventsOnlyStillAgree(t)
}

func testThreadEventsOnlyStillAgree(t *testing.T) {
	t.Helper()
	r := rand.New(rand.NewSource(99))
	rec0 := &recorder{}
	lg := &fakeLog{rec: rec0, data: NewFileHeader(1, 1, 7).AppendTo(nil)}
	ixf := &fakeIndex{rec: rec0}
	a := NewActor(&Index{Header: IndexHeader{ThreadID: 1}}, int64(len(lg.data)), lg, ixf, SyncNever)

	for range 20 {
		app, _ := threadEvent(r, 1_700_000_000_000, 3)
		if _, err := a.Append(app); err != nil {
			t.Fatal(err)
		}
		// Nothing is appended after a ThreadArchived (TKT-01M29CRJ64).
		if app.Type == TypeThreadArchived {
			break
		}
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	logBytes := lg.bytes()
	rec, err := Open(bytes.NewReader(logBytes), int64(len(logBytes)), nil)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Index.Len() != 0 {
		t.Errorf("replay invented %d entries from thread events alone", rec.Index.Len())
	}
	if !bytes.Equal(a.Index().AppendTo(nil), rec.Index.AppendTo(nil)) {
		t.Errorf("indexes differ:\n%s", diffIndexes(a.Index(), rec.Index))
	}
	if _, err := ParseIndex(ixf.bytes()); err != nil {
		t.Errorf("parsing the index the actor wrote: %v", err)
	}
}

// deleteBody is a Delete of kind k: 0 an author's, 1 a moderator's, 2 the
// auto-hide's.
func deleteBody(k int) *DeleteBody {
	return &DeleteBody{Reason: "spam", ByModerator: k == 1, Auto: k == 2}
}
