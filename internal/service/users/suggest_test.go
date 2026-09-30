package users

import (
	"path/filepath"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbtest"
)

// TestSuggestRefusesAStranger: the service refuses a non-member itself, not
// only the handler that asks MaySuggest first, because anything else that
// calls Suggest must get the same answer.
func TestSuggestRefusesAStranger(t *testing.T) {
	d := dbtest.Open(t, db.Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	s := &Service{DB: d}
	for _, v := range []domain.Viewer{domain.Anonymous(), {PreUser: true, IdentityID: 1}, {UserID: 1}} {
		if _, err := s.Suggest(t.Context(), v, "a", 0); err == nil {
			t.Errorf("%+v was given suggestions", v)
		}
	}
	if _, err := s.Suggest(t.Context(), domain.Viewer{UserID: 1, Active: true}, "a", 0); err != nil {
		t.Errorf("a member: %v", err)
	}
}
