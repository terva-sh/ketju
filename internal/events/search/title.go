package search

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// PutTitle writes thread id's title row as title inside tx, exactly as the
// indexer writes one: run through render.DropSentinels and trimmed, carrying
// the thread's creator and creation time from its log's first frame, and
// dropped when nothing is left. `ketju redact` rewrites a redacted title's
// row with it before it deletes its journal, whatever the row held before
// (docs/03 "Hard redaction", TKT-01M3NS122F).
func (x *Indexer) PutTitle(ctx context.Context, tx *sql.Tx, id threadid.ID, title string) error {
	title = strings.TrimSpace(render.DropSentinels(title))
	if title == "" {
		return db.DropSearchDoc(ctx, tx, int64(id), 0)
	}
	opened, err := x.open(id.LogPath(x.root))
	if err != nil {
		return err
	}
	f := &readRecorder{logFile: opened}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	c, err := firstFrame(f, fi.Size())
	if err != nil {
		return f.fault(fmt.Errorf("thread %s: %w", id, err))
	}
	return db.PutSearchDoc(ctx, tx, db.SearchDoc{
		Thread: int64(id), Ordinal: 0, AuthorID: int64(c.actor), CreatedAt: c.ms, Title: title,
	})
}
