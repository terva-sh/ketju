package blobs_test

import (
	"bytes"
	"database/sql"
	"fmt"
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

// TestUnattachedBytesUsesItsIndex: the bound's sum reads one account's
// unattached blobs through blobs_uploader_unreferenced, never a scan of
// every blob. The statement is UnattachedBytes's, from queries/blobs.sql.
func TestUnattachedBytesUsesItsIndex(t *testing.T) {
	s := newStore(t)
	const q = `SELECT CAST(coalesce(sum(size), 0) AS INTEGER) FROM blobs WHERE uploaded_by = ? AND refcount = 0`
	var plan []string
	if err := s.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+q, s.users["alice"])
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
		t.Fatal(err)
	}
	if len(plan) != 1 || !strings.HasPrefix(plan[0], "SEARCH blobs USING INDEX blobs_uploader_unreferenced (uploaded_by=?)") {
		t.Fatalf("plan = %q", plan)
	}
}
