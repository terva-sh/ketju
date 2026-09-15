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

// writeThread builds a real log and index on disk and returns their paths.
func writeThread(t *testing.T, dir string, posts int) (logPath, idxPath string) {
	t.Helper()
	id := threadid.ID(0x3f7a9c21e0000001)
	if err := os.MkdirAll(id.Dir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	logPath, idxPath = id.LogPath(dir), id.IndexPath(dir)

	lf, err := os.OpenFile(logPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer lf.Close()
	if _, err := lf.Write(threadlog.NewFileHeader(uint64(id), 1_700_000_000_000, 7).AppendTo(nil)); err != nil {
		t.Fatal(err)
	}
	xf, err := os.OpenFile(idxPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer xf.Close()

	a := threadlog.NewActor(&threadlog.Index{Header: threadlog.IndexHeader{ThreadID: uint64(id)}},
		threadlog.FileHeaderSize, lf, xf, threadlog.SyncNever)
	if _, err := a.Append(threadlog.Append{
		Type: threadlog.TypeThreadCreated, Author: 1,
		Body: &threadlog.ThreadCreatedBody{Title: "Why we left Slack", Slug: "why-we-left-slack", ForumID: 7},
	}); err != nil {
		t.Fatal(err)
	}
	for i := range posts {
		if _, err := a.Append(threadlog.Append{
			Type: threadlog.TypePost, Author: uint64(10 + i),
			Body: &threadlog.PostBody{Source: "post number " + string(rune('a'+i))},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	return logPath, idxPath
}

func dumpLines(t *testing.T, args ...string) ([]dumpLine, error) {
	t.Helper()
	var buf bytes.Buffer
	err := dump(&buf, args)

	var out []dumpLine
	for _, raw := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if raw == "" {
			continue
		}
		var l dumpLine
		if e := json.Unmarshal([]byte(raw), &l); e != nil {
			t.Fatalf("line is not JSON: %v\n%s", e, raw)
		}
		out = append(out, l)
	}
	return out, err
}

// Criterion: output is one JSON object per line carrying the frame header
// fields and the decoded body. And: offsets are included so a line can be
// correlated with an index entry.
func TestDumpPrintsOneObjectPerRecord(t *testing.T) {
	dir := t.TempDir()
	logPath, idxPath := writeThread(t, dir, 5)

	lines, err := dumpLines(t, logPath)
	if err != nil {
		t.Fatalf("dump: %v", err)
	}
	if len(lines) != 6 {
		t.Fatalf("got %d lines, want 6 (one thread event and five posts)", len(lines))
	}

	if lines[0].TypeName != "ThreadCreated" {
		t.Errorf("first record is %q, want ThreadCreated", lines[0].TypeName)
	}
	body, ok := lines[0].Body.(map[string]any)
	if !ok {
		t.Fatalf("first record has no decoded body: %+v", lines[0].Body)
	}
	if body["title"] != "Why we left Slack" {
		t.Errorf("decoded title is %v", body["title"])
	}

	for i, l := range lines[1:] {
		if l.TypeName != "Post" {
			t.Errorf("record %d is %q, want Post", i+1, l.TypeName)
		}
		if l.Ordinal != uint32(i+1) {
			t.Errorf("record %d has ordinal %d, want %d", i+1, l.Ordinal, i+1)
		}
		if l.Error != "" {
			t.Errorf("record %d reported an error: %s", i+1, l.Error)
		}
	}

	// Offsets must line up with the index, which is the whole reason they are
	// printed: a reader correlates a dump line with an entry.
	raw, err := os.ReadFile(idxPath)
	if err != nil {
		t.Fatal(err)
	}
	ix, err := threadlog.ParseIndex(raw)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 5 {
		e, err := ix.Entry(uint32(i + 1))
		if err != nil {
			t.Fatal(err)
		}
		l := lines[i+1]
		if uint64(l.Offset) != e.Offset {
			t.Errorf("ordinal %d: dump says offset %d, index says %d", i+1, l.Offset, e.Offset)
		}
		if uint32(l.Size) != e.FrameLength {
			t.Errorf("ordinal %d: dump says size %d, index says %d", i+1, l.Size, e.FrameLength)
		}
	}
}

// Criterion: the command works on a log whose index is missing or corrupt.
//
// It never opens the index at all, which is the property being checked — dump
// exists for when the index is the thing that is wrong.
func TestDumpIgnoresTheIndexEntirely(t *testing.T) {
	dir := t.TempDir()
	logPath, idxPath := writeThread(t, dir, 4)

	want, err := dumpLines(t, logPath)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name   string
		mangle func()
	}{
		{"missing", func() { os.Remove(idxPath) }},
		{"truncated", func() { os.WriteFile(idxPath, []byte("xx"), 0o644) }},
		{"garbage", func() { os.WriteFile(idxPath, bytes.Repeat([]byte{0xff}, 4096), 0o644) }},
		{"a directory", func() { os.Remove(idxPath); os.Mkdir(idxPath, 0o755) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.mangle()
			got, err := dumpLines(t, logPath)
			if err != nil {
				t.Fatalf("dump with a %s index: %v", tc.name, err)
			}
			if len(got) != len(want) {
				t.Fatalf("got %d lines with a %s index, want %d", len(got), tc.name, len(want))
			}
			for i := range got {
				if got[i].Offset != want[i].Offset || got[i].TypeName != want[i].TypeName {
					t.Errorf("line %d differs with a %s index", i, tc.name)
				}
			}
		})
	}
}

// Criterion: a record failing CRC is reported on its own line rather than
// aborting the dump.
func TestDumpReportsABadChecksumAndKeepsGoing(t *testing.T) {
	dir := t.TempDir()
	logPath, _ := writeThread(t, dir, 6)

	clean, err := dumpLines(t, logPath)
	if err != nil {
		t.Fatal(err)
	}
	victim := clean[3] // a post in the middle

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	// Flip a byte inside the body, which the checksum covers and the length
	// does not, so the frame stays findable and only its CRC fails.
	raw[victim.Offset+int64(threadlog.FrameFixedSize)+1] ^= 0xff
	if err := os.WriteFile(logPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := dumpLines(t, logPath)
	if err == nil {
		t.Error("dump exited clean over a damaged record; it must report the damage")
	} else if !errors.As(err, &errCorrupt{}) {
		t.Errorf("error is %v, want errCorrupt", err)
	}

	if len(got) != len(clean) {
		t.Fatalf("got %d lines, want %d — the dump stopped at the damaged record instead of "+
			"reporting it and continuing", len(got), len(clean))
	}
	bad := got[3]
	if bad.Error == "" {
		t.Errorf("the damaged record carries no error: %+v", bad)
	}
	if bad.Fatal {
		t.Error("a checksum failure was marked fatal; the next record's offset was still known")
	}
	if bad.Offset != victim.Offset {
		t.Errorf("damaged line is at offset %d, want %d", bad.Offset, victim.Offset)
	}
	// Everything after it still came through, which is the point.
	for i := 4; i < len(got); i++ {
		if got[i].Error != "" {
			t.Errorf("line %d after the damage reported an error: %s", i, got[i].Error)
		}
		if got[i].Offset != clean[i].Offset {
			t.Errorf("line %d after the damage is at %d, want %d", i, got[i].Offset, clean[i].Offset)
		}
	}
}

// A length that cannot be trusted stops the walk, because nothing after it can
// be located. That is reported as fatal rather than silently ending.
func TestDumpStopsAtAnUnfindableNextRecord(t *testing.T) {
	dir := t.TempDir()
	logPath, _ := writeThread(t, dir, 6)

	clean, err := dumpLines(t, logPath)
	if err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint32(raw[clean[3].Offset:], 0xffff_0000)
	if err := os.WriteFile(logPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := dumpLines(t, logPath)
	if err == nil {
		t.Error("an unusable length was not reported")
	}
	if len(got) != 4 {
		t.Fatalf("got %d lines, want 4 (three good then the fatal one)", len(got))
	}
	if !got[3].Fatal {
		t.Errorf("the record with an impossible length is not marked fatal: %+v", got[3])
	}
}

// A thread id resolves against --root; a path is taken as a path. Getting the
// two confused is the likeliest way to mistype this command.
func TestDumpResolvesIdsAndPaths(t *testing.T) {
	dir := t.TempDir()
	logPath, _ := writeThread(t, dir, 2)
	id := threadid.ID(0x3f7a9c21e0000001)

	byID, err := dumpLines(t, "--root", dir, id.String())
	if err != nil {
		t.Fatalf("dump by id: %v", err)
	}
	byPath, err := dumpLines(t, logPath)
	if err != nil {
		t.Fatalf("dump by path: %v", err)
	}
	if len(byID) != len(byPath) || len(byID) == 0 {
		t.Fatalf("id gave %d lines, path gave %d", len(byID), len(byPath))
	}

	// An id with no --root has to say so rather than looking for a file named
	// after the id in the working directory.
	if _, err := dumpLines(t, id.String()); err == nil {
		t.Error("a bare thread id with no --root was accepted")
	} else if !strings.Contains(err.Error(), "--root") {
		t.Errorf("error %q does not mention --root", err)
	}

	if _, err := dumpLines(t, filepath.Join(dir, "nope.log")); err == nil {
		t.Error("a missing file was accepted")
	}
}
