package session

import (
	"encoding/hex"
	"regexp"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db/sqltypes"
)

// TestTagNamesTheSessionAndNothingElse holds Session.Tag (TKT-01M3QVWKAG):
// the same for one session every time, different for two, and neither the
// stored hash nor the SessionKey a cache is keyed by.
func TestTagNamesTheSessionAndNothingElse(t *testing.T) {
	var a, b sqltypes.SessionHash
	for i := range a {
		a[i], b[i] = byte(i), byte(i)
	}
	b[31]++
	sa, sb := Session{hash: a}, Session{hash: b}

	if sa.Tag() != (Session{hash: a, Kind: KindPreUser, Provider: "github"}).Tag() {
		t.Fatal("the tag changed with something other than the session")
	}
	if sa.Tag() == sb.Tag() {
		t.Fatalf("two sessions share the tag %s", sa.Tag())
	}
	if !regexp.MustCompile(`^[0-9a-f]{24}$`).MatchString(sa.Tag()) {
		t.Fatalf("tag %q is not 24 lowercase hex digits", sa.Tag())
	}
	if sa.Tag() == key(a) {
		t.Fatal("the tag is the SessionKey")
	}
	if strings.Contains(hex.EncodeToString(a[:]), sa.Tag()) {
		t.Fatal("the tag is part of the stored hash")
	}
}
