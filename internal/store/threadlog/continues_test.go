package threadlog

import (
	"encoding/hex"
	"errors"
	"reflect"
	"testing"
)

// TestThreadCreatedContinuesRoundTrip (TKT-01M29CYSC0): ThreadCreated's
// optional continues key is pinned by a golden, in CBOR and in JSON, with it
// and without it: a body without it encodes as it did before the key
// existed, so no log written earlier changes meaning.
func TestThreadCreatedContinuesRoundTrip(t *testing.T) {
	plain := &ThreadCreatedBody{Title: "T", Slug: "t", ForumID: 7}
	continues := &ThreadCreatedBody{Title: "T", Slug: "t", ForumID: 7, Continues: &PostRef{ThreadID: 1<<40 | 3, Ordinal: 70000}}
	for _, c := range []struct {
		name string
		body *ThreadCreatedBody
		json string
		cbor string
	}{
		{"without", plain, `{"title":"T","slug":"t","forum_id":7}`, "a3657469746c65615464736c7567617468666f72756d5f696407"},
		{"with", continues, `{"title":"T","slug":"t","forum_id":7,"continues":{"thread_id":1099511627779,"ordinal":70000}}`,
			"a4657469746c65615464736c7567617468666f72756d5f69640769636f6e74696e756573a2697468726561645f69641b0000010000000003676f7264696e616c1a00011170"},
	} {
		for _, enc := range []BodyEncoding{EncodingCBOR, EncodingJSON} {
			raw, err := EncodeBody(enc, c.body)
			if err != nil {
				t.Fatal(err)
			}
			want, got := c.json, string(raw)
			if enc == EncodingCBOR {
				want, got = c.cbor, hex.EncodeToString(raw)
			}
			if got != want {
				t.Errorf("%s %s body is %s, the golden is %s", c.name, encName(enc), got, want)
			}
			back, err := DecodeBody(TypeThreadCreated, enc, raw)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(back, c.body) {
				t.Errorf("%s %s round trip gave %#v", c.name, encName(enc), back)
			}
		}
	}
}

// TestThreadCreatedContinuesMalformed (TKT-01M29CYSC0): CheckThreadCreated
// refuses a continues naming thread 0, ordinal 0 or the record's own thread,
// takes a body as a pointer or a value, and passes one with no continues;
// the actor refuses such a record, and a ThreadCreated naming an ordinal,
// before writing it.
func TestThreadCreatedContinuesMalformed(t *testing.T) {
	const self = 9
	for _, c := range []struct {
		name string
		ref  *PostRef
		ok   bool
	}{
		{"none", nil, true},
		{"another thread's post", &PostRef{ThreadID: 4, Ordinal: 2}, true},
		{"thread 0", &PostRef{ThreadID: 0, Ordinal: 2}, false},
		{"ordinal 0", &PostRef{ThreadID: 4, Ordinal: 0}, false},
		{"its own thread", &PostRef{ThreadID: self, Ordinal: 2}, false},
	} {
		for _, body := range []any{&ThreadCreatedBody{Title: "t", Continues: c.ref}, ThreadCreatedBody{Title: "t", Continues: c.ref}} {
			err := CheckThreadCreated(body, self)
			if (err == nil) != c.ok || (err != nil && !errors.Is(err, ErrMalformed)) {
				t.Errorf("%s, %T: %v, want ok=%v", c.name, body, err, c.ok)
			}
		}
	}
	if err := CheckThreadCreated(&PostBody{}, self); !errors.Is(err, ErrMalformed) {
		t.Errorf("a Post body: %v, want ErrMalformed", err)
	}
	if err := CheckThreadCreated((*ThreadCreatedBody)(nil), self); !errors.Is(err, ErrMalformed) {
		t.Errorf("a nil body: %v, want ErrMalformed", err)
	}

	a, lg, _, _ := newThread(t, SyncGroup)
	id := a.ix.Header.ThreadID
	n := len(lg.bytes())
	for _, c := range []struct {
		name string
		app  Append
	}{
		{"its own thread", Append{Type: TypeThreadCreated, Body: &ThreadCreatedBody{Title: "t", Continues: &PostRef{ThreadID: id, Ordinal: 1}}}},
		{"ordinal 0", Append{Type: TypeThreadCreated, Body: &ThreadCreatedBody{Title: "t", Continues: &PostRef{ThreadID: id + 1}}}},
		{"a frame ordinal", Append{Type: TypeThreadCreated, Ordinal: 1, Body: &ThreadCreatedBody{Title: "t"}}},
	} {
		if _, err := a.Append(c.app); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: %v, want ErrMalformed", c.name, err)
		}
	}
	if len(lg.bytes()) != n {
		t.Errorf("the refusals wrote %d bytes", len(lg.bytes())-n)
	}
	if _, _, err := a.frameFor(Append{Type: TypeThreadCreated, Body: &ThreadCreatedBody{Title: "t",
		Continues: &PostRef{ThreadID: id + 1, Ordinal: 3}}}, nil); err != nil {
		t.Errorf("a well-formed continues: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
}
