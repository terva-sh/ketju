package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/db/dbtest"
)

// replayTables are the tables outside the projection that a replay of the
// logs could reach, each dumped whole, every column, in key order: what a
// post's attachments count in blobs (Terva review 2074 could not judge it),
// the audit rows the writer adds, the notifications and the progress the
// generator keeps.
var replayTables = []struct{ name, query string }{
	{"blobs", `SELECT * FROM blobs ORDER BY hash`},
	{"blob_uploads", `SELECT * FROM blob_uploads ORDER BY hash, user_id`},
	{"audit_log", `SELECT * FROM audit_log ORDER BY id`},
	{"notifications", `SELECT * FROM notifications ORDER BY id`},
	{"notification_progress", `SELECT * FROM notification_progress ORDER BY thread_id`},
}

// dumpReplay is the data directory's database, table by table: every
// projection table (dbtest.DumpProjections), the search index
// (dbtest.DumpSearch) and replayTables, each under its name.
func dumpReplay(t *testing.T, data string) map[string]string {
	t.Helper()
	d, err := db.OpenReadOnly(t.Context(), adminDBPath(data))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	out := map[string]string{}
	for line := range strings.Lines(dbtest.DumpProjections(t, d)) {
		table, _, _ := strings.Cut(line, ":")
		out[table] += line
	}
	out["search"] = dbtest.DumpSearch(t, d)
	if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		for _, tb := range replayTables {
			rows, err := tx.QueryContext(t.Context(), tb.query)
			if err != nil {
				return err
			}
			cols, err := rows.Columns()
			if err != nil {
				rows.Close()
				return err
			}
			var b strings.Builder
			for rows.Next() {
				v := make([]any, len(cols))
				p := make([]any, len(cols))
				for i := range v {
					p[i] = &v[i]
				}
				if err := rows.Scan(p...); err != nil {
					rows.Close()
					return err
				}
				for i, c := range cols {
					if raw, ok := v[i].([]byte); ok {
						v[i] = string(raw)
					}
					fmt.Fprintf(&b, " %s=%v", c, v[i])
				}
				b.WriteByte('\n')
			}
			if err := rows.Close(); err != nil {
				return err
			}
			out[tb.name] = b.String()
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestBackfillReplayEqualsReindex is TKT-01M41MTXWH's third criterion: a
// replay of every log over the database already there, which is what a
// backfill (migration 0030) asks the first start after an upgrade to do,
// leaves every projection table, blobs and their reference counts, the audit
// log, the notifications and their progress, and the search index exactly as
// they were, and equal to a fresh ketju reindex --projections --search of the
// same logs, table by table.
//
// The world is written through the real serve, so every path that touches
// those tables ran as it does in production: posts that quote, mention and
// attach a blob; a watched thread; a moderator's hide; a pin; a cross-forum
// split and merge; a reply as a new thread; tags and a retag; an archived
// thread. Then serve stops, and two copies are made. On the first,
// post_quotes is emptied and a backfill asked for on every thread, as 0025
// and 0030 leave a database projected before them, and serve starts again.
// The second is reindexed.
func TestBackfillReplayEqualsReindex(t *testing.T) {
	w := newP4World(t, false, ada, mona, sam, p4Kim, p3Gail)
	admin, moderator, member, kim, gail := w.as["ada"], w.as["mona"], w.as["sam"], w.as["kim"], w.as["gail"]

	v := kim.start(t, "vault", "Replayed vault", "Vault, first.")
	blob := admin.attach(t, v, "replayblob")
	admin.post(t, v, quoting(1, "Vault, first.", "@kim and @gail, see the picture."), blob)
	gail.post(t, v, "Vault 3.")
	gail.post(t, v, quoting(2, "see the picture.", "Vault 4 quotes ada."))
	a := admin.start(t, "annex", "Replayed annex", "Annex, first.")
	admin.post(t, a, "Annex 2.")
	threadIDOf(t, kim.must(t, http.StatusOK, http.MethodPost, "/api/v1/admin/threads/"+v+"/split",
		map[string]any{"ordinals": []int{3}, "title": "Replayed split", "forum": "annex"}), "thread")
	kim.must(t, http.StatusOK, http.MethodPost, "/api/v1/admin/threads/"+a+"/merge", map[string]any{"ordinals": []int{2}, "into": v})
	res := kim.continueAs(t, "annex", "Replayed continuation", "Onward.", v, 2)
	if res.status != http.StatusCreated {
		t.Fatalf("the reply as a new thread answered %d:\n%s", res.status, res)
	}

	g := member.start(t, "general", "Replayed general", "Sam opens.")
	member.must(t, http.StatusOK, http.MethodPost, "/api/v1/threads/"+g+"/watch", map[string]any{"level": "watch"})
	kim.post(t, g, quoting(1, "Sam opens.", "Kim answers @sam."))
	hidden, _ := gail.post(t, g, "A post mona hides.")
	moderator.must(t, http.StatusOK, http.MethodDelete, fmt.Sprintf("/api/v1/threads/%s/posts/%d", g, hidden),
		map[string]any{"reason": "Tone."})
	admin.post(t, g, quoting(2, "Kim answers @sam.", "Ada quotes kim."))
	admin.must(t, http.StatusOK, http.MethodPost, "/api/v1/admin/threads/"+g+"/pin", map[string]any{"scope": "forum"})
	res = kim.startTagged(t, "general", "Replayed tags", []string{"garden", "long-read"})
	if res.status != http.StatusCreated {
		t.Fatalf("the tagged thread answered %d:\n%s", res.status, res)
	}
	tagged := threadIDOf(t, res, "thread")
	kim.must(t, http.StatusOK, http.MethodPut, "/api/v1/admin/threads/"+tagged+"/tags", map[string]any{"tags": []string{"garden"}})
	z := member.start(t, "general", "Replayed archive", "Closed.")
	kim.post(t, z, quoting(1, "Closed.", "Before it closes."))
	admin.must(t, http.StatusOK, http.MethodPost, "/api/v1/admin/threads/"+z+"/archive", map[string]any{"reason": "History."})

	// Notifications are generated after the posts are projected; wait for
	// the generator before stopping serve, so the copies agree with what it
	// wrote.
	waitUntil(t, func() bool {
		d, err := db.OpenReadOnly(t.Context(), adminDBPath(w.data))
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		var behind int
		if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			return tx.QueryRowContext(t.Context(), `SELECT count(*) FROM projection_state AS p
				LEFT JOIN notification_progress AS n ON n.thread_id = p.thread_id
				WHERE n.log_length IS NULL OR n.log_length < p.log_length`).Scan(&behind)
		}); err != nil {
			t.Fatal(err)
		}
		return behind == 0
	})
	if err := w.r.shutdown(t); err != nil {
		t.Fatalf("stopping serve: %v\n%s", err, w.r.log)
	}
	before := dumpReplay(t, w.data)
	for table, want := range map[string]string{
		"post_quotes": "post_quotes:", "post_attachments": "post_attachments:", "post_moves": "post_moves:",
		"thread_continuations": "thread_continuations:", "thread_tags": `tag="garden"`, "mentions": "mentions:",
		"audit_log": "action=posts.moved", "notifications": "kind=quoted", "blobs": "refcount=1",
		"search": "doc ",
	} {
		if !strings.Contains(before[table], want) {
			t.Fatalf("the world's %s holds no %q, so the comparison would prove nothing:\n%s", table, want, before[table])
		}
	}
	for _, action := range []string{"post.hidden", "thread.pinned", "thread.retagged", "thread.archived"} {
		if !strings.Contains(before["audit_log"], "action="+action) {
			t.Fatalf("the world's audit_log has no %s:\n%s", action, before["audit_log"])
		}
	}
	for _, kind := range []string{"mention", "thread_you_watch"} {
		if !strings.Contains(before["notifications"], "kind="+kind) {
			t.Fatalf("the world's notifications have no %s:\n%s", kind, before["notifications"])
		}
	}

	// The replay: serve's first start over a database whose backfill is
	// asked for on every thread, with post_quotes empty.
	replayed := restore(t, w.data)
	rd, err := db.Open(t.Context(), db.Options{Path: adminDBPath(replayed)})
	if err != nil {
		t.Fatal(err)
	}
	var threads int
	if err := rd.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		if _, err := tx.ExecContext(t.Context(), `DELETE FROM post_quotes`); err != nil {
			return err
		}
		r, err := tx.ExecContext(t.Context(), `UPDATE projection_state SET backfill = 1`)
		if err != nil {
			return err
		}
		n, err := r.RowsAffected()
		threads = int(n)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := rd.Close(); err != nil {
		t.Fatal(err)
	}
	// Nobody signs in, so the copy's serve needs no provider.
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(fmt.Sprintf("listen = %q\ndata_dir = %q\n[storage]\nfsync = \"never\"\n",
		freePort(t), replayed)), 0o644); err != nil {
		t.Fatal(err)
	}
	r := start(t, path, serveOpts{})
	if err := r.shutdown(t); err != nil {
		t.Fatalf("serve on the replayed copy: %v\n%s", err, r.log)
	}
	if line := catchUpLine.FindString(r.log.String()); !strings.Contains(line, fmt.Sprintf(`"backfilled":%d,`, threads)) ||
		!strings.Contains(line, `"corrupt":0,`) || !strings.Contains(line, `"failed":0,`) {
		t.Fatalf("the replaying start's catch-up, over %d threads: %s", threads, line)
	}

	// The reindex: a fresh rebuild of the same logs.
	reindexed := restore(t, w.data)
	if _, errOut, err := reindexRun(t, reindexOpts{}, "--root", reindexed, "--projections", "--search"); err != nil {
		t.Fatalf("reindex: %v\n%s", err, errOut)
	}

	got, want := dumpReplay(t, replayed), dumpReplay(t, reindexed)
	for table, rows := range before {
		if got[table] != want[table] {
			t.Errorf("%s: the replay over the database differs from a reindex:\n%s\n--- reindex ---\n%s", table, got[table], want[table])
		}
		if got[table] != rows {
			t.Errorf("%s: the replay changed the rows that were there:\n%s\n--- before ---\n%s", table, got[table], rows)
		}
	}
	if len(got) != len(before) || len(want) != len(before) {
		t.Errorf("dumped %d tables after the replay and %d after the reindex, want %d", len(got), len(want), len(before))
	}
}
