package threads

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/render"
)

// golden encodes as internal/dto's golden test does.
func golden(t *testing.T, v any) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func renderCtx(replyTo *uint32) render.Context {
	return render.Context{
		Posts:   render.ResolverFunc(func(o uint32) (bool, bool) { return o >= 1 && o <= 62, o == 62 }),
		Handles: render.HandleFunc(func(h string) (int64, string, bool) { return 7, "alice", strings.EqualFold(h, "alice") }),
		ReplyTo: replyTo,
	}
}

// The committed POST /render goldens are what a real render converts to,
// not a shape written by hand beside the renderer.
func TestRenderDTOMatchesTheGoldens(t *testing.T) {
	reply := uint32(61)
	cases := map[string]struct {
		source  string
		replyTo *uint32
	}{
		"render.member":       {"> [quote=#61]\n> the quoted words\n\n> [quote=#62]\n> gone\n\nAgreed, @Alice.", &reply},
		"render-plain.member": {"Hello.", nil},
	}
	for name, c := range cases {
		r, err := render.RenderWith(c.source, renderCtx(c.replyTo))
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile("../../dto/testdata/golden/" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		if got := golden(t, RenderDTO(r)); !bytes.Equal(got, want) {
			t.Errorf("%s:\ngot  %s\nwant %s", name, got, want)
		}
	}
}

// AC2's half that exists before the endpoints do: the same source and thread
// convert to the same facts every time, through the one conversion both
// paths use. The endpoint-level check is TKT-01M29CA9D4's AC1 and AC3.
func TestTheSameSourceGivesTheSameFacts(t *testing.T) {
	reply := uint32(3)
	src := "> [quote=#2]\n> x\n\n@alice @bob @alice"
	a, err := render.RenderWith(src, renderCtx(&reply))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := render.RenderWith(src, renderCtx(&reply))
	if !reflect.DeepEqual(RenderDTO(a), RenderDTO(b)) {
		t.Error("two renders of one source converted differently")
	}
	got := RenderDTO(a)
	// alice once, however often she is named; bob is nobody.
	want := dto.RenderResult{HTML: a.HTML, Mentions: []dto.MentionRef{{Handle: "alice"}},
		Quotes: []dto.QuoteRef{{Ordinal: 2}}, ReplyTo: got.ReplyTo}
	if !reflect.DeepEqual(got, want) || got.ReplyTo == nil || *got.ReplyTo != 3 {
		t.Errorf("RenderDTO = %+v", got)
	}
}
