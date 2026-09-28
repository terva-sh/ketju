package blob

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathFollowsTheLayout(t *testing.T) {
	h := "ab" + strings.Repeat("0", 62)
	got, err := Path("/data", h)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/data", "blobs", "sha256", "ab", h); got != want {
		t.Fatalf("Path = %s, want %s", got, want)
	}
}

func TestPathRefusesWhatIsNotAHash(t *testing.T) {
	for _, h := range []string{
		"",
		strings.Repeat("a", 63),
		strings.Repeat("a", 65),
		strings.Repeat("A", 64), // the table stores lowercase only
		"../" + strings.Repeat("a", 61),
		strings.Repeat("g", 64),
	} {
		if _, err := Path("/data", h); !errors.Is(err, ErrBadHash) {
			t.Errorf("Path(%q) = %v, want ErrBadHash", h, err)
		}
	}
}
