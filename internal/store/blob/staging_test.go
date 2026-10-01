package blob

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestCommitSyncsNewShardInItsParent is Terva review 1732's first finding:
// the first upload into a shard creates the shard, and a power cut after
// the row commits must not take the shard's entry, and the file with it.
// Commit syncs every directory from the shard up to root before the rename,
// and the shard after it; nothing above root.
func TestCommitSyncsNewShardInItsParent(t *testing.T) {
	var synced []string
	real := syncDir
	t.Cleanup(func() { syncDir = real })
	syncDir = func(dir string) error {
		synced = append(synced, dir)
		return real(dir)
	}

	root := t.TempDir()
	hash := "cd" + strings.Repeat("1", 62)
	f, err := Stage(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("bytes"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	placed, err := Commit(f.Name(), root, hash)
	if err != nil || !placed {
		t.Fatalf("Commit = %v, %v", placed, err)
	}
	shard := filepath.Join(root, Dir, Algorithm, "cd")
	want := []string{
		filepath.Join(root, Dir, Algorithm),
		filepath.Join(root, Dir),
		root,
		shard,
	}
	if !slices.Equal(synced, want) {
		t.Fatalf("synced %q, want %q", synced, want)
	}
	if _, err := os.Stat(filepath.Join(shard, hash)); err != nil {
		t.Fatal(err)
	}

	// A second blob in the same shard syncs the same chain: the shard may
	// be one a crashed process created and never synced.
	synced = nil
	g, err := Stage(root)
	if err != nil {
		t.Fatal(err)
	}
	g.Close()
	if _, err := Commit(g.Name(), root, "cd"+strings.Repeat("2", 62)); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(synced, want) {
		t.Fatalf("second commit synced %q, want %q", synced, want)
	}
}

// TestSweepTmpSkipsHeldFiles: a staged file is held from Stage to Unstage,
// and SweepTmp leaves it whatever its age, at startup's zero included;
// unstaged, the same file goes (Terva review 1735).
func TestSweepTmpSkipsHeldFiles(t *testing.T) {
	root := t.TempDir()
	f, err := Stage(root)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	now := time.Now()
	old := now.Add(-3 * time.Hour)
	if err := os.Chtimes(f.Name(), old, old); err != nil {
		t.Fatal(err)
	}
	for _, age := range []time.Duration{time.Hour, 0} {
		if n, err := SweepTmp(root, age, now); err != nil || n != 0 {
			t.Fatalf("SweepTmp(%v) = %d, %v; want the held file kept", age, n, err)
		}
	}
	Unstage(f.Name())
	if n, err := SweepTmp(root, time.Hour, now); err != nil || n != 1 {
		t.Fatalf("SweepTmp = %d, %v; want the unstaged file removed", n, err)
	}
}
