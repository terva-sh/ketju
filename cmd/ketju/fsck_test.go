package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// threadAt writes a thread with a given id, so a directory can hold several.
func threadAt(t *testing.T, root string, id threadid.ID, posts int) string {
	t.Helper()
	if err := os.MkdirAll(id.Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := id.LogPath(root)
	lf, err := os.OpenFile(logPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer lf.Close()
	if _, err := lf.Write(threadlog.NewFileHeader(uint64(id), 1_700_000_000_000, 7).AppendTo(nil)); err != nil {
		t.Fatal(err)
	}
	xf, err := os.OpenFile(id.IndexPath(root), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer xf.Close()

	a := threadlog.NewActor(&threadlog.Index{Header: threadlog.IndexHeader{ThreadID: uint64(id)}},
		threadlog.FileHeaderSize, lf, xf, threadlog.SyncNever)
	for i := range posts {
		if _, err := a.Append(threadlog.Append{
			Type: threadlog.TypePost, Author: uint64(i + 1),
			Body: &threadlog.PostBody{Source: "a message of ordinary length"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	return logPath
}

func fsckJSON(t *testing.T, args ...string) (fsckSummary, string, error) {
	t.Helper()
	var buf bytes.Buffer
	err := fsckMain(&buf, append([]string{"--json"}, args...))
	var sum fsckSummary
	if e := json.Unmarshal(buf.Bytes(), &sum); e != nil {
		t.Fatalf("output is not JSON: %v\n%s", e, buf.String())
	}
	return sum, buf.String(), err
}

func fsckText(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	err := fsckMain(&buf, args)
	return buf.String(), err
}

// Criterion: every log is walked and every CRC verified, with a summary naming
// any damaged thread.
func TestFsckWalksEveryThread(t *testing.T) {
	root := t.TempDir()
	ids := []threadid.ID{0x3f7a9c21e0000001, 0x3f7a9c21e0000002, 0x4000000000000009}
	for i, id := range ids {
		threadAt(t, root, id, 3+i)
	}

	sum, _, err := fsckJSON(t, "--root", root)
	if err != nil {
		t.Fatalf("fsck on a clean directory: %v", err)
	}
	if sum.Threads != 3 {
		t.Errorf("walked %d threads, want 3", sum.Threads)
	}
	if sum.Posts != 3+4+5 {
		t.Errorf("counted %d posts, want 12", sum.Posts)
	}
	if sum.Damaged != 0 {
		t.Errorf("%d threads reported damaged on a clean directory", sum.Damaged)
	}
	if exitCodeFor(err) != exitOK {
		t.Errorf("exit code %d, want %d", exitCodeFor(err), exitOK)
	}
}

// Criterion: exit code distinguishes clean from corruption, and the damaged
// thread is named.
func TestFsckNamesTheDamagedThreadAndExitsCorrupt(t *testing.T) {
	root := t.TempDir()
	good := threadid.ID(0x3f7a9c21e0000001)
	bad := threadid.ID(0x3f7a9c21e0000002)
	threadAt(t, root, good, 3)
	badPath := threadAt(t, root, bad, 4)

	raw, err := os.ReadFile(badPath)
	if err != nil {
		t.Fatal(err)
	}
	// A body byte: the checksum covers it, the length does not, so the frame
	// stays findable and only its CRC fails.
	raw[threadlog.FileHeaderSize+threadlog.FrameFixedSize+2] ^= 0xff
	if err := os.WriteFile(badPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	sum, _, err := fsckJSON(t, "--root", root)
	if err == nil {
		t.Fatal("fsck exited clean over a damaged thread")
	}
	if !errors.As(err, &errCorrupt{}) {
		t.Errorf("error is %v, want errCorrupt", err)
	}
	if got := exitCodeFor(err); got != exitCorrupt {
		t.Errorf("exit code %d, want %d", got, exitCorrupt)
	}
	if sum.Damaged != 1 {
		t.Fatalf("%d damaged threads, want 1", sum.Damaged)
	}

	// The report has to say WHICH one, or an operator with a thousand threads
	// learns only that something is wrong.
	var named bool
	for _, r := range sum.Reports {
		if r.ID == bad.String() && r.Damaged > 0 {
			named = true
		}
		if r.ID == good.String() && r.Damaged > 0 {
			t.Errorf("the undamaged thread %s was reported damaged", good)
		}
	}
	if !named {
		t.Errorf("the damaged thread %s is not named in the report", bad)
	}

	text, _ := fsckText(t, "--root", root)
	if !strings.Contains(text, bad.String()) {
		t.Errorf("text output does not name %s:\n%s", bad, text)
	}
}

// A torn tail is an interrupted write, not corruption, and must be reported
// as its own thing. Calling it damage would send somebody looking for a
// failing disk after an ordinary power cut.
func TestFsckSeparatesATornTailFromDamage(t *testing.T) {
	root := t.TempDir()
	id := threadid.ID(0x3f7a9c21e0000001)
	p := threadAt(t, root, id, 5)

	// Where the last complete record starts, so the expected torn count can be
	// derived rather than guessed. It is the bytes PAST the last complete
	// record, not the bytes removed — those are different numbers and the
	// useful one is how much unusable tail an operator is looking at.
	clean, err := dumpLines(t, p)
	if err != nil {
		t.Fatal(err)
	}
	lastStart := clean[len(clean)-1].Offset

	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	const cut = 9
	if err := os.WriteFile(p, raw[:len(raw)-cut], 0o644); err != nil {
		t.Fatal(err)
	}
	wantTorn := int64(len(raw)-cut) - lastStart

	sum, _, err := fsckJSON(t, "--root", root)
	if err != nil {
		t.Fatalf("a torn tail was reported as corruption: %v", err)
	}
	if sum.Damaged != 0 {
		t.Errorf("%d threads damaged; a torn tail is not damage", sum.Damaged)
	}
	if sum.Truncated != 1 {
		t.Fatalf("%d threads reported torn, want 1", sum.Truncated)
	}
	if sum.Reports[0].Truncated != wantTorn {
		t.Errorf("reported %d torn bytes, want %d (the partial record left behind)",
			sum.Reports[0].Truncated, wantTorn)
	}

	text, _ := fsckText(t, "--root", root)
	if !strings.Contains(text, "TORN") {
		t.Errorf("text output does not mark it torn:\n%s", text)
	}
}

// Criterion: indexes are rebuilt. And the rebuilt index has to be good enough
// that the next open trusts it — an index that still triggers a replay has not
// been repaired.
func TestFsckRebuildsAnIndexThatIsThenTrusted(t *testing.T) {
	root := t.TempDir()
	id := threadid.ID(0x3f7a9c21e0000001)
	logPath := threadAt(t, root, id, 12)

	if err := os.Remove(id.IndexPath(root)); err != nil {
		t.Fatal(err)
	}

	sum, _, err := fsckJSON(t, "--root", root, "--rebuild")
	if err != nil {
		t.Fatal(err)
	}
	if sum.Rebuilt != 1 {
		t.Fatalf("%d indexes rebuilt, want 1", sum.Rebuilt)
	}

	f, err := os.Open(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fi, _ := f.Stat()
	raw, err := os.ReadFile(id.IndexPath(root))
	if err != nil {
		t.Fatalf("no index was written: %v", err)
	}
	rec, err := threadlog.Open(f, fi.Size(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Outcome != threadlog.Trusted {
		t.Errorf("the rebuilt index opened as %v, want Trusted", rec.Outcome)
	}
	if rec.Index.Len() != 12 {
		t.Errorf("rebuilt index holds %d posts, want 12", rec.Index.Len())
	}
}

// Without --rebuild nothing is written. A default that repairs is a default
// that surprises somebody running a check.
func TestFsckWritesNothingByDefault(t *testing.T) {
	root := t.TempDir()
	id := threadid.ID(0x3f7a9c21e0000001)
	threadAt(t, root, id, 4)

	before, err := os.ReadFile(id.IndexPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(id.IndexPath(root), append(before, 'x'), 0o644); err != nil {
		t.Fatal(err)
	}
	stamped, _ := os.ReadFile(id.IndexPath(root))

	if _, _, err := fsckJSON(t, "--root", root); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(id.IndexPath(root))
	if !bytes.Equal(stamped, after) {
		t.Error("fsck rewrote an index without --rebuild")
	}
}

// Criterion: safe to run against a live data directory.
//
// A log growing mid-scan must not be reported as damage. The size is taken
// once at open, so an append landing after that is simply not counted yet —
// which is the difference between safe and merely unlikely to break.
func TestFsckIsSafeWhileAThreadIsBeingAppendedTo(t *testing.T) {
	root := t.TempDir()
	id := threadid.ID(0x3f7a9c21e0000001)
	threadAt(t, root, id, 5)

	// Reopen the thread and append while fsck is about to look at it.
	lf, err := os.OpenFile(id.LogPath(root), os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer lf.Close()
	fi, _ := lf.Stat()
	raw, _ := os.ReadFile(id.IndexPath(root))
	rec, err := threadlog.Open(lf, fi.Size(), raw)
	if err != nil {
		t.Fatal(err)
	}
	xf, err := os.OpenFile(id.IndexPath(root), os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer xf.Close()
	a := threadlog.NewActor(rec.Index, rec.LogLength, lf, xf, threadlog.SyncNever)

	for i := range 20 {
		if _, err := a.Append(threadlog.Append{
			Type: threadlog.TypePost, Author: 9,
			Body: &threadlog.PostBody{Source: "appended during the scan"},
		}); err != nil {
			t.Fatal(err)
		}
		if i == 3 {
			sum, _, err := fsckJSON(t, "--root", root)
			if err != nil {
				t.Fatalf("fsck reported a problem on a live thread: %v", err)
			}
			if sum.Damaged != 0 {
				t.Errorf("a live append was reported as damage")
			}
			if sum.Posts < 5 {
				t.Errorf("counted %d posts, want at least the 5 that were durable before", sum.Posts)
			}
		}
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
}

// A file sitting in the wrong shard, or not named after an id, is a real
// fault: something moved it or wrote it by hand.
func TestFsckReportsAMisplacedLog(t *testing.T) {
	root := t.TempDir()
	id := threadid.ID(0x3f7a9c21e0000001)
	threadAt(t, root, id, 2)

	wrong := filepath.Join(root, threadid.ThreadsDir, "00", "00")
	if err := os.MkdirAll(wrong, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wrong, id.Hex()+".log"),
		threadlog.NewFileHeader(uint64(id), 1, 1).AppendTo(nil), 0o644); err != nil {
		t.Fatal(err)
	}

	sum, _, err := fsckJSON(t, "--root", root)
	if err == nil {
		t.Fatal("a log in the wrong shard was accepted")
	}
	if sum.Damaged != 1 {
		t.Errorf("%d threads flagged, want 1", sum.Damaged)
	}
}

func TestFsckUsageErrors(t *testing.T) {
	if _, err := fsckText(t, "--root", filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("a missing root was accepted")
	} else if got := exitCodeFor(err); got != exitUsage {
		t.Errorf("exit code %d for a missing root, want %d", got, exitUsage)
	}
	if _, err := fsckText(t, "extra"); err == nil {
		t.Error("a positional argument was accepted")
	}
}

// Damage in a length field, which stops the walk, is corruption rather than a
// torn tail when it does not reach the end of the file.
func TestFsckTreatsAnUnusableLengthAsCorruption(t *testing.T) {
	root := t.TempDir()
	id := threadid.ID(0x3f7a9c21e0000001)
	p := threadAt(t, root, id, 6)

	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint32(raw[threadlog.FileHeaderSize:], 2)
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	sum, _, err := fsckJSON(t, "--root", root)
	if err == nil {
		t.Fatal("an unusable length was accepted")
	}
	if sum.Damaged != 1 {
		t.Errorf("%d damaged, want 1", sum.Damaged)
	}
	if sum.Truncated != 0 {
		t.Errorf("reported as torn; a length below the minimum is corruption")
	}
}
