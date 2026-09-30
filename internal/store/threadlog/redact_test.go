package threadlog

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"
)

const (
	hashA = "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111"
	hashB = "bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222"
)

// secrets are the text redactFixture writes into post 2 and the titles, each
// of which must be gone from the bytes afterwards.
var postSecrets = []string{"SECRET-POST-TWO", "SECRET-EDIT-TWO", "SECRET-REASON", "SECRET-JSON-EDIT", hashA, hashB}

var titleSecrets = []string{"Secret Title One", "secret-title-one", "Secret Title Two", "secret-title-two",
	"Secret Title Three", "secret-title-three"}

// redactFixture is a log whose post 2 has three revisions, one in JSON,
// with mentions, attachments, a reason and a grace edit, beside a post 1
// and 3 that keep their text, and a thread renamed twice.
func redactFixture(t *testing.T) []byte {
	t.Helper()
	b := NewFileHeader(0x3f7a9c21e0000001, 1_700_000_000_000, 7).AppendTo(nil)
	ts := int64(1_700_000_000_000)
	add := func(h FrameHeader, v any) {
		t.Helper()
		if h.Encoding == 0 {
			h.Encoding = EncodingCBOR
		}
		if h.Revision == 0 {
			h.Revision = 1
		}
		ts++
		h.TimestampMS = ts
		body, err := EncodeBody(h.Encoding, v)
		if err != nil {
			t.Fatal(err)
		}
		if b, err = AppendFrame(b, h, body); err != nil {
			t.Fatal(err)
		}
	}
	m := []uint64{4343}
	add(FrameHeader{Type: TypeThreadCreated, Actor: 1},
		&ThreadCreatedBody{Title: "Secret Title One", Slug: "secret-title-one", ForumID: 7, Tags: []string{"meta"}})
	add(FrameHeader{Type: TypePost, Ordinal: 1, Actor: 1}, &PostBody{Source: "plain first"})
	add(FrameHeader{Type: TypePost, Ordinal: 2, Actor: 2, Flags: FrameFlagMachineAuthored},
		&PostBody{Source: "SECRET-POST-TWO", ReplyTo: 1, Mentions: []uint64{4242}, Attachments: []string{hashA, hashB}})
	add(FrameHeader{Type: TypeEdit, Ordinal: 2, Revision: 2, Actor: 2},
		&EditBody{Source: "SECRET-EDIT-TWO", Reason: "SECRET-REASON", ReplyTo: 1, Mentions: &m, Grace: true})
	add(FrameHeader{Type: TypeEdit, Ordinal: 2, Revision: 3, Actor: 9, Encoding: EncodingJSON},
		&EditBody{Source: "SECRET-JSON-EDIT", Reason: "x"})
	add(FrameHeader{Type: TypeThreadRenamed, Actor: 9}, &ThreadRenamedBody{Title: "Secret Title Two", Slug: "secret-title-two"})
	add(FrameHeader{Type: TypePost, Ordinal: 3, Actor: 1}, &PostBody{Source: "keeps its text", ReplyTo: 2})
	add(FrameHeader{Type: TypeDelete, Ordinal: 3, Actor: 1}, &DeleteBody{Reason: "mine"})
	add(FrameHeader{Type: TypeThreadRenamed, Actor: 9}, &ThreadRenamedBody{Title: "Secret Title Three", Slug: "secret-title-three"})
	return b
}

var testRedact = RedactOptions{TimeMS: 1_800_000_000_000, Operator: 7, Reason: "court order", Journal: "01K6DHZ3Q8N6T2V4X9B7M5C1RE"}

func redactPosts(t *testing.T, log []byte, ordinals ...uint32) ([]byte, Rewrite) {
	t.Helper()
	var out bytes.Buffer
	rw, err := RedactPosts(bytes.NewReader(log), int64(len(log)), &out, ordinals, testRedact)
	if err != nil {
		t.Fatalf("RedactPosts: %v", err)
	}
	if rw.Size != int64(out.Len()) {
		t.Fatalf("Rewrite.Size = %d, wrote %d", rw.Size, out.Len())
	}
	return out.Bytes(), rw
}

func frames(t *testing.T, log []byte) []ScanRecord {
	t.Helper()
	var out []ScanRecord
	if err := ScanFrames(bytes.NewReader(log), int64(len(log)), func(r ScanRecord) bool {
		if r.Err != nil {
			t.Fatalf("frame at %d: %v", r.Offset, r.Err)
		}
		out = append(out, r)
		return true
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestRedactKeepsEveryFrameLength: the new log is the old one, frame for
// frame at the same offsets and lengths, plus one Redacted frame, and every
// frame not of the post is byte-identical.
func TestRedactKeepsEveryFrameLength(t *testing.T) {
	old := redactFixture(t)
	nu, rw := redactPosts(t, old, 2)
	before, after := frames(t, old), frames(t, nu)
	if len(after) != len(before)+1 {
		t.Fatalf("%d frames after, %d before: want exactly one more", len(after), len(before))
	}
	for i, b := range before {
		a := after[i]
		if a.Offset != b.Offset || a.Size != b.Size {
			t.Errorf("frame %d moved: %d+%d, was %d+%d", i, a.Offset, a.Size, b.Offset, b.Size)
		}
		if a.Header != b.Header {
			t.Errorf("frame %d header changed: %+v, was %+v", i, a.Header, b.Header)
		}
		ofPost := b.Header.Ordinal == 2 && (b.Header.Type == TypePost || b.Header.Type == TypeEdit)
		if same := bytes.Equal(nu[a.Offset:a.Offset+int64(a.Size)], old[b.Offset:b.Offset+int64(b.Size)]); same == ofPost {
			t.Errorf("frame %d (%s of %d): byte-identical = %v", i, b.Header.Type, b.Header.Ordinal, same)
		}
	}
	if int64(len(nu)) != int64(len(old))+int64(after[len(after)-1].Size) {
		t.Errorf("the log grew by %d, not by the marker's %d", len(nu)-len(old), after[len(after)-1].Size)
	}
	last := after[len(after)-1]
	v, err := DecodeBody(last.Header.Type, last.Header.Encoding, last.Body)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := v.(*RedactedBody)
	if !ok || last.Header.Ordinal != 2 || last.Header.Actor != 7 || m.Ordinal != 2 ||
		!slices.Equal(m.Revisions, []uint32{1, 2, 3}) || m.Reason != "court order" || m.Journal != testRedact.Journal {
		t.Errorf("marker %+v %+v", last.Header, v)
	}
	if rw.Blanked != 3 || !slices.Equal(rw.Revisions[2], []uint32{1, 2, 3}) {
		t.Errorf("Rewrite = %+v", rw)
	}
}

// TestRedactBlanksEveryRevision: no byte of any revision's source, mention,
// reason or attachment list is left, and what is structure survives.
func TestRedactBlanksEveryRevision(t *testing.T) {
	old := redactFixture(t)
	for _, s := range postSecrets {
		if !bytes.Contains(old, []byte(s)) {
			t.Fatalf("the fixture does not hold %q", s)
		}
	}
	nu, _ := redactPosts(t, old, 2)
	for _, s := range postSecrets {
		if bytes.Contains(nu, []byte(s)) {
			t.Errorf("%q survived the redaction", s)
		}
	}
	for _, s := range []string{"plain first", "keeps its text", "Secret Title Three"} {
		if !bytes.Contains(nu, []byte(s)) {
			t.Errorf("%q, not the post's, was lost", s)
		}
	}
	for _, r := range frames(t, nu) {
		if r.Header.Ordinal != 2 || (r.Header.Type != TypePost && r.Header.Type != TypeEdit) {
			continue
		}
		v, err := DecodeBody(r.Header.Type, r.Header.Encoding, r.Body)
		if err != nil {
			t.Fatal(err)
		}
		switch b := v.(type) {
		case *PostBody:
			if b.Source != "" || b.Mentions != nil || b.Attachments != nil || b.ReplyTo != 1 {
				t.Errorf("blanked Post = %+v", b)
			}
		case *EditBody:
			if b.Source != "" || b.Reason != "" || b.Mentions != nil {
				t.Errorf("blanked Edit = %+v", b)
			}
			if r.Header.Revision == 2 && (!b.Grace || b.ReplyTo != 1) {
				t.Errorf("revision 2 lost its grace or reply_to: %+v", b)
			}
		}
		if a, err := AttachmentsOf(r.Header, r.Body); err != nil || a != nil {
			t.Errorf("attachments after = %v, %v", a, err)
		}
	}
}

// TestRedactedBodiesDecode: a blanked body is exactly as long as the body it
// replaces and decodes to the empty body, in both encodings, from the
// smallest body a writer produces through bodies whose padding crosses every
// CBOR length-header boundary.
func TestRedactedBodiesDecode(t *testing.T) {
	var sizes []int
	for n := 1; n < 300; n++ {
		sizes = append(sizes, n)
	}
	for _, edge := range []int{23, 24, 255, 256, 65535, 65536, 200_000} {
		sizes = append(sizes, edge-2, edge-1, edge, edge+1, edge+2, edge+9, edge+10, edge+11)
	}
	for _, enc := range []BodyEncoding{EncodingCBOR, EncodingJSON} {
		for _, n := range sizes {
			for _, c := range []struct {
				h FrameHeader
				v any
			}{
				{FrameHeader{Type: TypePost, Encoding: enc}, &PostBody{Source: strings.Repeat("x", n)}},
				{FrameHeader{Type: TypePost, Encoding: enc}, &PostBody{Source: strings.Repeat("y", n), ReplyTo: 70000,
					Origin: &PostOrigin{ThreadID: 1 << 50, Ordinal: 3}}},
				{FrameHeader{Type: TypeEdit, Encoding: enc}, &EditBody{Source: strings.Repeat("z", n), Grace: true, ReplyTo: 2}},
				{FrameHeader{Type: TypeThreadRenamed, Encoding: enc}, &ThreadRenamedBody{Title: strings.Repeat("t", n), Slug: "s"}},
			} {
				raw, err := EncodeBody(enc, c.v)
				if err != nil {
					t.Fatal(err)
				}
				blank, err := BlankBody(c.h, raw)
				if err != nil {
					t.Fatalf("%s %s of %d: %v", encName(enc), c.h.Type, n, err)
				}
				if len(blank) != len(raw) {
					t.Fatalf("%s %s of %d: %d bytes, was %d", encName(enc), c.h.Type, n, len(blank), len(raw))
				}
				v, err := DecodeBody(c.h.Type, enc, blank)
				if err != nil {
					t.Fatalf("%s %s of %d: the blank body does not decode: %v", encName(enc), c.h.Type, n, err)
				}
				switch b := v.(type) {
				case *PostBody:
					if want := c.v.(*PostBody); b.Source != "" || b.ReplyTo != want.ReplyTo ||
						(want.Origin != nil && (b.Origin == nil || *b.Origin != *want.Origin)) {
						t.Fatalf("%s Post of %d decodes to %+v", encName(enc), n, b)
					}
				case *EditBody:
					if b.Source != "" || !b.Grace || b.ReplyTo != 2 {
						t.Fatalf("%s Edit of %d decodes to %+v", encName(enc), n, b)
					}
				case *ThreadRenamedBody:
					if *b != (ThreadRenamedBody{}) {
						t.Fatalf("%s rename of %d decodes to %+v", encName(enc), n, b)
					}
				}
				f, err := AppendFrame(nil, c.h, blank)
				if err != nil {
					if n < 200_000 {
						t.Fatal(err)
					}
					continue
				}
				if _, _, _, err := ParseFrame(f); err != nil {
					t.Fatalf("the frame does not parse: %v", err)
				}
			}
		}
	}

	// The smallest body any writer produces is exactly enough.
	raw := mustEncode(t, &PostBody{Source: "x"})
	if len(raw) != 10 {
		t.Fatalf("{source:\"x\"} is %d bytes; the padding argument in docs/03 assumes 10", len(raw))
	}
	if _, err := BlankBody(FrameHeader{Type: TypePost, Encoding: EncodingCBOR}, raw); err != nil {
		t.Errorf("the smallest body cannot be blanked: %v", err)
	}
	// And a body with no text to give up cannot be padded: refused, not
	// written at another length.
	if _, err := BlankBody(FrameHeader{Type: TypePost, Encoding: EncodingCBOR}, mustEncode(t, &PostBody{ReplyTo: 1})); err == nil {
		t.Error("a body shorter than its padding was blanked")
	}
}

// TestRedactRefusesADamagedLog: a redaction runs on a sound log or not at
// all.
func TestRedactRefusesADamagedLog(t *testing.T) {
	good := redactFixture(t)
	fs := frames(t, good)
	flip := bytes.Clone(good)
	flip[fs[3].Offset+FrameFixedSize+2] ^= 0x40

	critical := bytes.Clone(good)
	body := mustEncode(t, &ThreadLockedBody{})
	critical, _ = AppendFrame(critical, FrameHeader{Type: 0x81, Encoding: EncodingCBOR, Revision: 1}, body)

	redacted, _ := redactPosts(t, good, 2)

	for _, c := range []struct {
		name     string
		log      []byte
		ordinals []uint32
	}{
		{"a checksum failure", flip, []uint32{3}},
		{"a torn tail", good[:len(good)-3], []uint32{2}},
		{"an unknown critical record", critical, []uint32{2}},
		{"a post already redacted", redacted, []uint32{2}},
		{"no such post", good, []uint32{9}},
		{"ordinal 0", good, []uint32{0}},
		{"no header", good[:20], []uint32{2}},
	} {
		t.Run(c.name, func(t *testing.T) {
			var out bytes.Buffer
			_, err := RedactPosts(bytes.NewReader(c.log), int64(len(c.log)), &out, c.ordinals, testRedact)
			if !errors.Is(err, ErrRedact) {
				t.Errorf("err = %v, want ErrRedact", err)
			}
		})
	}
}

// TestRedactTwoOrdinalsInOneLog: two posts in one log are one rewrite with
// one marker each, in ordinal order, and the markers are what a resume reads.
func TestRedactTwoOrdinalsInOneLog(t *testing.T) {
	old := redactFixture(t)
	nu, rw := redactPosts(t, old, 3, 2)
	after := frames(t, nu)
	var marked []uint32
	for _, r := range after[len(frames(t, old)):] {
		if r.Header.Type != TypeRedacted {
			t.Fatalf("appended a %s", r.Header.Type)
		}
		marked = append(marked, r.Header.Ordinal)
	}
	if !slices.Equal(marked, []uint32{2, 3}) {
		t.Errorf("markers for %v, want [2 3]", marked)
	}
	if !slices.Equal(rw.Revisions[3], []uint32{1}) {
		t.Errorf("post 3's revisions = %v", rw.Revisions[3])
	}
	got, err := RedactedOrdinals(bytes.NewReader(nu), int64(len(nu)))
	if err != nil || len(got) != 2 || !got[2] || !got[3] {
		t.Errorf("RedactedOrdinals = %v, %v", got, err)
	}
	if got, _ := RedactedOrdinals(bytes.NewReader(old), int64(len(old))); len(got) != 0 {
		t.Errorf("the old log has markers %v", got)
	}
}

// TestRedactedIndexExtendedEqualsRebuilt: the index of the old log, extended
// by the marker, equals the index rebuilt from the new log, so a thread
// opened after a redaction reads the same however its index was found.
func TestRedactedIndexExtendedEqualsRebuilt(t *testing.T) {
	old := redactFixture(t)
	before, err := Open(bytes.NewReader(old), int64(len(old)), nil)
	if err != nil {
		t.Fatal(err)
	}
	e2, _ := before.Index.Entry(2)
	if !e2.Flags.Has(EntryHasAttachments) || !e2.Flags.Has(EntryEdited) {
		t.Fatalf("the fixture's post 2 has flags %#x", e2.Flags)
	}
	nu, _ := redactPosts(t, old, 2)
	extended, err := Open(bytes.NewReader(nu), int64(len(nu)), before.Index.AppendTo(nil))
	if err != nil {
		t.Fatal(err)
	}
	if extended.Outcome != Extended {
		t.Fatalf("outcome %s, want extended", extended.Outcome)
	}
	rebuilt, err := Open(bytes.NewReader(nu), int64(len(nu)), nil)
	if err != nil {
		t.Fatal(err)
	}
	sameIndex(t, extended.Index, rebuilt.Index, "extended against rebuilt")
	e, _ := rebuilt.Index.Entry(2)
	if !e.Flags.Has(EntryRedacted) || e.Flags.Has(EntryHasAttachments) || e.Flags.Visible() {
		t.Errorf("post 2 has flags %#x", e.Flags)
	}
	for _, o := range []uint32{1, 3} {
		a, _ := before.Index.Entry(o)
		b, _ := rebuilt.Index.Entry(o)
		if a != b {
			t.Errorf("post %d changed: %+v, was %+v", o, b, a)
		}
	}
	// The reader never reads a redacted post's padding, moderator or not.
	p, err := NewReader(bytes.NewReader(nu), rebuilt.Index).Hidden(2)
	if err != nil || !p.Tombstone || p.Source != "" {
		t.Errorf("Hidden(2) = %+v, %v", p, err)
	}
	// History still lists every revision, with no content.
	revs, err := NewReader(bytes.NewReader(nu), rebuilt.Index).History(2, int64(len(nu)))
	if err != nil || len(revs) != 3 {
		t.Fatalf("History = %v, %v", revs, err)
	}
	for _, r := range revs {
		if r.Source != "" || r.Reason != "" {
			t.Errorf("revision %d has content: %+v", r.Revision, r)
		}
	}
}

// TestRedactedIsCritical: a build whose catalogue does not have Redacted
// refuses the thread rather than serve it from an index that calls the post
// visible.
func TestRedactedIsCritical(t *testing.T) {
	nu, _ := redactPosts(t, redactFixture(t), 2)
	saved := catalogue[TypeRedacted]
	delete(catalogue, TypeRedacted)
	t.Cleanup(func() { catalogue[TypeRedacted] = saved })

	if _, err := Open(bytes.NewReader(nu), int64(len(nu)), nil); !errors.Is(err, ErrCriticalRecordType) {
		t.Errorf("an older build opened a redacted log: %v", err)
	}
}

// TestRedactTitleBlanksEveryTitleFrame: no byte of any title or slug is
// left, the forum and tags survive, and every frame keeps its length and
// its checksum.
func TestRedactTitleBlanksEveryTitleFrame(t *testing.T) {
	old := redactFixture(t)
	for _, s := range titleSecrets {
		if !bytes.Contains(old, []byte(s)) {
			t.Fatalf("the fixture does not hold %q", s)
		}
	}
	var out bytes.Buffer
	rw, err := RedactTitle(bytes.NewReader(old), int64(len(old)), &out,
		ThreadRenamedBody{Title: "Title removed", Slug: "title-removed"}, testRedact)
	if err != nil {
		t.Fatal(err)
	}
	nu := out.Bytes()
	if rw.Blanked != 3 || rw.Size != int64(len(nu)) {
		t.Errorf("Rewrite = %+v", rw)
	}
	for _, s := range titleSecrets {
		if bytes.Contains(nu, []byte(s)) {
			t.Errorf("%q survived the redaction", s)
		}
	}
	if !bytes.Contains(nu, []byte("SECRET-POST-TWO")) {
		t.Error("a title redaction touched a post")
	}
	before, after := frames(t, old), frames(t, nu)
	if len(after) != len(before)+2 {
		t.Fatalf("%d frames, want %d", len(after), len(before)+2)
	}
	for i, b := range before {
		if a := after[i]; a.Offset != b.Offset || a.Size != b.Size || a.Header != b.Header {
			t.Errorf("frame %d changed shape", i)
		}
	}
	v, _ := DecodeBody(TypeThreadCreated, EncodingCBOR, after[0].Body)
	if c := v.(*ThreadCreatedBody); c.Title != "" || c.Slug != "" || c.ForumID != 7 || !slices.Equal(c.Tags, []string{"meta"}) {
		t.Errorf("ThreadCreated = %+v", c)
	}
	mk, rn := after[len(after)-2], after[len(after)-1]
	mv, _ := DecodeBody(mk.Header.Type, mk.Header.Encoding, mk.Body)
	if m, ok := mv.(*RedactedBody); !ok || !m.Title || m.Ordinal != 0 || mk.Header.Ordinal != 0 || m.Journal != testRedact.Journal {
		t.Errorf("marker = %+v %+v", mk.Header, mv)
	}
	rv, _ := DecodeBody(rn.Header.Type, rn.Header.Encoding, rn.Body)
	if r, ok := rv.(*ThreadRenamedBody); !ok || r.Title != "Title removed" || rn.Header.Actor != 7 {
		t.Errorf("replacement = %+v %+v", rn.Header, rv)
	}
	if got, _ := RedactedOrdinals(bytes.NewReader(nu), int64(len(nu))); len(got) != 0 {
		t.Errorf("a title marker counts as posts %v", got)
	}
}

// TestTitleMarkerTouchesNoPost: the title marker and its rename change no
// index entry.
func TestTitleMarkerTouchesNoPost(t *testing.T) {
	old := redactFixture(t)
	before, err := Open(bytes.NewReader(old), int64(len(old)), nil)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if _, err := RedactTitle(bytes.NewReader(old), int64(len(old)), &out,
		ThreadRenamedBody{Title: "T", Slug: "t"}, testRedact); err != nil {
		t.Fatal(err)
	}
	after, err := Open(bytes.NewReader(out.Bytes()), int64(out.Len()), before.Index.AppendTo(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(after.Index.entries, before.Index.entries) {
		t.Error("a title marker changed an index entry")
	}
	// A title marker naming a post is a log that disagrees with itself.
	ix := &Index{entries: []IndexEntry{{Revision: 1}}}
	bad := mustEncode(t, &RedactedBody{Title: true})
	if err := ix.Apply(FrameHeader{Type: TypeRedacted, Encoding: EncodingCBOR, Ordinal: 1}, bad, 0, 0); !errors.Is(err, ErrMalformed) {
		t.Errorf("a title marker at ordinal 1: %v", err)
	}
}

// TestActorRefusesRedactions: the actor never writes a marker, and a
// redacted post takes no Edit, Delete or Restore, whatever the caller's
// preconditions. A reply to it is still a post.
func TestActorRefusesRedactions(t *testing.T) {
	a, _, _, _ := newThread(t, SyncNever)
	if _, err := a.Append(post("one")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Append(Append{Type: TypeRedacted, Ordinal: 1, Body: &RedactedBody{Ordinal: 1}}); !errors.Is(err, ErrMalformed) {
		t.Errorf("the actor appended a Redacted: %v", err)
	}
	a.Close()

	nu, _ := redactPosts(t, redactFixture(t), 2)
	rec, err := Open(bytes.NewReader(nu), int64(len(nu)), nil)
	if err != nil {
		t.Fatal(err)
	}
	r := &recorder{}
	actor := NewActor(rec.Index, int64(len(nu)), &fakeLog{rec: r, data: nu}, &fakeIndex{rec: r}, SyncNever)
	defer actor.Close()
	for _, ap := range []Append{
		{Type: TypeEdit, Ordinal: 2, Body: &EditBody{Source: "back"}},
		{Type: TypeDelete, Ordinal: 2, Body: &DeleteBody{}},
		{Type: TypeRestore, Ordinal: 2, Body: &RestoreBody{}},
	} {
		if _, err := actor.Append(ap); !errors.Is(err, ErrPostState) {
			t.Errorf("%s of a redacted post: %v", ap.Type, err)
		}
	}
	if _, err := actor.Append(Append{Type: TypePost, Body: &PostBody{Source: "a reply", ReplyTo: 2}}); err != nil {
		t.Errorf("a reply to a redacted post: %v", err)
	}
}

// TestAttachmentInventoryReadsEveryRevision: an Edit carrying an attachments
// key, which no Edit does in this build, is read as a Post's is.
func TestAttachmentInventoryReadsEveryRevision(t *testing.T) {
	for _, enc := range []BodyEncoding{EncodingCBOR, EncodingJSON} {
		raw, err := EncodeBody(enc, map[string]any{"source": "x", "attachments": []string{hashA}})
		if err != nil {
			t.Fatal(err)
		}
		got, err := AttachmentsOf(FrameHeader{Type: TypeEdit, Encoding: enc}, raw)
		if err != nil || !slices.Equal(got, []string{hashA}) {
			t.Errorf("%s Edit: %v, %v", encName(enc), got, err)
		}
		raw, _ = EncodeBody(enc, &PostBody{Source: "x", Attachments: []string{hashB}})
		if got, _ := AttachmentsOf(FrameHeader{Type: TypePost, Encoding: enc}, raw); !slices.Equal(got, []string{hashB}) {
			t.Errorf("%s Post: %v", encName(enc), got)
		}
	}
	if got, _ := AttachmentsOf(FrameHeader{Type: TypeDelete, Encoding: EncodingCBOR}, nil); got != nil {
		t.Errorf("a Delete has attachments %v", got)
	}
}
