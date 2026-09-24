package threads

import (
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
)

// TestPreviewFailedLookupIsAnError: a preview during which a handle lookup
// failed is an error, not a preview that draws the mention as unresolved,
// for the reason html refuses one (review 392). A composer shown plain text
// for a person who exists would tell its author nobody will be notified.
func TestPreviewFailedLookupIsAnError(t *testing.T) {
	d := openDB(t)
	addUser(t, d, "dana")
	s := &Service{DB: d}
	r, err := s.renderSource(t.Context(), nil, "hello @dana", nil)
	if err != nil || !strings.Contains(r.HTML, `href="/u/dana"`) || len(r.Mentions) != 1 {
		t.Fatalf("with the database up: %+v, %v", r, err)
	}
	d.Close()
	if r, err := s.renderSource(t.Context(), nil, "hello @dana", nil); err == nil {
		t.Fatalf("with the database closed the preview succeeded: %+v", r)
	}
}

// TestPreviewRefusesAStranger: the service refuses a non-member itself, not
// only the handler that asks MayPreview first, because the HTML client and
// anything else that calls Preview must get the same answer.
func TestPreviewRefusesAStranger(t *testing.T) {
	s := &Service{DB: openDB(t)}
	for _, v := range []domain.Viewer{domain.Anonymous(), {PreUser: true, IdentityID: 1}, {UserID: 1}} {
		if _, err := s.Preview(t.Context(), v, PreviewRequest{Source: "hi"}); err == nil {
			t.Errorf("%+v previewed a post", v)
		}
	}
	if _, err := s.Preview(t.Context(), domain.Viewer{UserID: 1, Active: true}, PreviewRequest{Source: "hi"}); err != nil {
		t.Errorf("a member: %v", err)
	}
}
