package blobs_test

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// fileOf is a text file of exactly size bytes whose content depends on seed,
// so that files of one size are different blobs.
func fileOf(size int, seed int) []byte {
	head := fmt.Appendf(nil, "file %d\n", seed)
	return append(head, bytes.Repeat([]byte("a"), size-len(head))...)
}

// TestUploadBudget is TKT-01M3V73KM1: new bytes that would take an
// account's unattached bytes over Budget are refused with upload_budget and
// leave no file or row. Bytes already stored are never refused for it,
// attaching a file frees its bytes, a draft's file still counts, and another
// account's files do not.
func TestUploadBudget(t *testing.T) {
	s := newStore(t)
	s.svc.Budget = 3000

	var hashes []string
	for i := range 3 {
		h, err := s.upload("alice", "f.txt", fileOf(1000, i))
		if err != nil {
			t.Fatalf("upload %d, within the bound: %v", i, err)
		}
		hashes = append(hashes, h)
	}

	// A draft names the first file: saving a draft adds no reference, so
	// its bytes still count.
	var forum int64
	s.read(func(q *dbq.Queries) error {
		f, err := q.GetForumBySlug(t.Context(), "general")
		forum = f.ID
		return err
	})
	if err := s.db.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `INSERT INTO drafts (user_id, forum_id, source, attachments, created_at, updated_at)
			VALUES (?, ?, 'held', json_array(?), 1, 1)`, s.users["alice"], forum, hashes[0])
		return err
	}); err != nil {
		t.Fatal(err)
	}

	before, rows := s.files(), s.count(`SELECT count(*) FROM blobs`)
	over := fileOf(1000, 3)
	if _, err := s.upload("alice", "over.txt", over); fieldCode(err) != "file upload_budget" {
		t.Fatalf("an upload past the bound: %v", err)
	}
	if after := s.files(); !slices.Equal(after, before) {
		t.Fatalf("a refused upload changed the blob files from %v to %v", before, after)
	}
	if n := s.count(`SELECT count(*) FROM blobs`); n != rows {
		t.Fatalf("a refused upload left %d blobs rows, want %d", n, rows)
	}
	if n := s.count(`SELECT count(*) FROM blob_uploads WHERE hash = ?`, sum(over)); n != 0 {
		t.Fatalf("a refused upload left %d upload rows", n)
	}
	if staged := s.staged(); len(staged) != 0 {
		t.Fatalf("a refused upload left staged files %v", staged)
	}

	// One byte is still over; the bound counts bytes, not files.
	if _, err := s.upload("alice", "byte.txt", []byte("b")); fieldCode(err) != "file upload_budget" {
		t.Fatalf("one byte past the bound: %v", err)
	}

	// Bytes already stored use no new disk: alice's own, and carol's.
	if _, err := s.upload("alice", "again.txt", fileOf(1000, 1)); err != nil {
		t.Fatalf("re-uploading her own stored bytes at the bound: %v", err)
	}
	carols := fileOf(500, 40)
	if _, err := s.upload("carol", "c.txt", carols); err != nil {
		t.Fatal(err)
	}
	if _, err := s.upload("alice", "carols.txt", carols); err != nil {
		t.Fatalf("uploading bytes carol stored, at the bound: %v", err)
	}

	// Carol's own files are hers: alice at the bound does not stop her.
	if _, err := s.upload("carol", "d.txt", fileOf(2000, 41)); err != nil {
		t.Fatalf("carol, under her own bound: %v", err)
	}

	// Attaching a file to a post frees its bytes for the next upload.
	if err := s.svc.AddRefs(t.Context(), hashes[1:2]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.upload("alice", "over.txt", over); err != nil {
		t.Fatalf("an upload after attaching one file: %v", err)
	}
	if _, err := s.upload("alice", "next.txt", fileOf(1000, 5)); fieldCode(err) != "file upload_budget" {
		t.Fatalf("the bound is full again: %v", err)
	}
}

// TestUploadBudgetRace: two uploads that each fit alone, but not together,
// run at once. The write connection serializes them, so exactly one is
// stored, and the refused one leaves no staged file. Run it under -race.
func TestUploadBudgetRace(t *testing.T) {
	for round := range 10 {
		s := newStore(t)
		s.svc.Budget = 1500
		files := [][]byte{fileOf(1000, 2*round), fileOf(1000, 2*round+1)}
		errs := make([]error, len(files))
		var start, done sync.WaitGroup
		start.Add(1)
		for i, f := range files {
			done.Go(func() {
				start.Wait()
				_, errs[i] = s.upload("alice", "f.txt", f)
			})
		}
		start.Done()
		done.Wait()

		var stored, refused int
		for _, err := range errs {
			switch {
			case err == nil:
				stored++
			case fieldCode(err) == "file upload_budget":
				refused++
			default:
				t.Fatalf("round %d: %v", round, err)
			}
		}
		if stored != 1 || refused != 1 {
			t.Fatalf("round %d: %d stored and %d refused, want one of each", round, stored, refused)
		}
		if n := s.count(`SELECT count(*) FROM blobs`); n != 1 || len(s.files()) != 1 {
			t.Fatalf("round %d: %d rows and files %v, want one of each", round, n, s.files())
		}
		if staged := s.staged(); len(staged) != 0 {
			t.Fatalf("round %d: staged files left: %v", round, staged)
		}
	}
}

// plan is the query plan of query with args, one detail line a step.
func (s *store) plan(query string, args ...any) []string {
	s.t.Helper()
	var plan []string
	if err := s.db.Read(s.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(s.t.Context(), "EXPLAIN QUERY PLAN "+query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				return err
			}
			plan = append(plan, detail)
		}
		return rows.Err()
	}); err != nil {
		s.t.Fatal(err)
	}
	return plan
}

// wantPlan holds that plan has a step starting with each of want, and no
// step that scans a table.
func wantPlan(t *testing.T, plan []string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !slices.ContainsFunc(plan, func(p string) bool { return strings.HasPrefix(p, w) }) {
			t.Errorf("no step %q in plan %q", w, plan)
		}
	}
	for _, p := range plan {
		if strings.HasPrefix(p, "SCAN") {
			t.Errorf("plan %q scans: %q", plan, p)
		}
	}
}

func nullID(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: true} }

// forumID is the id of the store's forum, general.
func forumID(s *store) int64 {
	s.t.Helper()
	var id int64
	s.read(func(q *dbq.Queries) error {
		f, err := q.GetForumBySlug(s.t.Context(), "general")
		id = f.ID
		return err
	})
	return id
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// pngBomb is a PNG signature and an IHDR declaring 20000 x 20000 pixels,
// and nothing else: what a header check must refuse before decoding.
func pngBomb() []byte {
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], 20000)
	binary.BigEndian.PutUint32(ihdr[4:], 20000)
	ihdr[8], ihdr[9] = 8, 6
	b.Write(binary.BigEndian.AppendUint32(nil, uint32(len(ihdr))))
	crc := crc32.NewIEEE()
	crc.Write([]byte("IHDR"))
	crc.Write(ihdr)
	b.WriteString("IHDR")
	b.Write(ihdr)
	b.Write(binary.BigEndian.AppendUint32(nil, crc.Sum32()))
	return b.Bytes()
}
