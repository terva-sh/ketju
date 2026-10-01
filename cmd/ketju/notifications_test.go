package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// notificationRows is every notifications and notification_progress row, one
// line each.
func notificationRows(t *testing.T, d *db.DB) string {
	t.Helper()
	var b strings.Builder
	err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		for _, q := range []string{
			`SELECT id || ' ' || user_id || ' ' || kind || ' ' || coalesce(thread_id, '') || ' ' ||
				coalesce(ordinal, '') || ' ' || at || ' ' || coalesce(read_at, '') FROM notifications ORDER BY id`,
			`SELECT thread_id || ' ' || log_length || ' ' || updated_at FROM notification_progress ORDER BY thread_id`,
		} {
			rows, err := tx.QueryContext(t.Context(), q)
			if err != nil {
				return err
			}
			for rows.Next() {
				var s string
				if err := rows.Scan(&s); err != nil {
					rows.Close()
					return err
				}
				fmt.Fprintln(&b, s)
			}
			if err := rows.Close(); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// TestServeGeneratesNotifications is the generator as serve wires it
// (TKT-01M29CRJ2T): a reply through the API that names a member reaches
// their notification list and their /me badge through the event bus, with
// no post text, and a delete through the API takes it off both. Marking it
// read clears the badge.
func TestServeGeneratesNotifications(t *testing.T) {
	s := startExit(t, mona, sam)
	writer, reader := s.member(t, sam), s.member(t, mona)
	id := newThread(t, writer)
	const words = "a sentence only the thread page shows"
	res := reply(t, writer, id, "@mona, "+words)
	if res.status != http.StatusCreated {
		t.Fatalf("replying answered %d:\n%s", res.status, res)
	}
	ordinal, _ := created(t, res)

	type note struct {
		ID      int64  `json:"id"`
		Kind    string `json:"kind"`
		Ordinal *int   `json:"ordinal"`
		Read    bool   `json:"read"`
	}
	list := func() ([]note, string) {
		t.Helper()
		res := reader.do(t, http.MethodGet, "/api/v1/notifications", nil)
		if res.status != http.StatusOK || res.header.Get("Cache-Control") != "no-store" {
			t.Fatalf("GET /notifications answered %d, Cache-Control %q:\n%s", res.status,
				res.header.Get("Cache-Control"), res)
		}
		var out struct {
			Notifications []note `json:"notifications"`
		}
		res.decode(t, &out)
		return out.Notifications, res.String()
	}
	var got []note
	var body string
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if got, body = list(); len(got) > 0 || time.Now().After(deadline) {
			break
		}
	}
	if len(got) != 1 || got[0].Kind != "mention" || got[0].Ordinal == nil || *got[0].Ordinal != ordinal {
		t.Fatalf("mona's notifications: %s", body)
	}
	if strings.Contains(body, words) {
		t.Errorf("the notification carries the post's text: %s", body)
	}
	if n := reader.me(t).Notifications; n != 1 {
		t.Errorf("mona's badge is %d, want 1", n)
	}
	if n := writer.me(t).Notifications; n != 0 {
		t.Errorf("sam's badge is %d for his own post", n)
	}

	del := writer.do(t, http.MethodDelete, fmt.Sprintf("/api/v1/threads/%s/posts/%d", id, ordinal), map[string]any{})
	if del.status >= 300 {
		t.Fatalf("deleting answered %d:\n%s", del.status, del)
	}
	if got, body = list(); len(got) != 0 {
		t.Errorf("a deleted post's notification is still listed: %s", body)
	}
	if n := reader.me(t).Notifications; n != 0 {
		t.Errorf("mona's badge is %d after the delete", n)
	}
	restore := writer.do(t, http.MethodPost, fmt.Sprintf("/api/v1/threads/%s/posts/%d/restore", id, ordinal), map[string]any{})
	if restore.status >= 300 {
		t.Fatalf("restoring answered %d:\n%s", restore.status, restore)
	}
	marked := reader.do(t, http.MethodPost, "/api/v1/notifications/read", map[string]any{"up_to": got0(t, list)})
	if marked.status != http.StatusOK || !strings.Contains(marked.String(), `"marked":1`) {
		t.Fatalf("marking read answered %d:\n%s", marked.status, marked)
	}
	if n := reader.me(t).Notifications; n != 0 {
		t.Errorf("mona's badge is %d after marking it read", n)
	}
}

// got0 is the id of the first notification list returns.
func got0[T interface{ ~[]E }, E any](t *testing.T, list func() (T, string)) int64 {
	t.Helper()
	got, body := list()
	if len(got) == 0 {
		t.Fatalf("the list is empty: %s", body)
	}
	b, err := json.Marshal(got[0])
	if err != nil {
		t.Fatal(err)
	}
	var first struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(b, &first); err != nil {
		t.Fatal(err)
	}
	return first.ID
}

// TestReindexWritesNoNotifications is AC5's last part (TKT-01M29CRJ2T):
// notifications and the generator's progress are authoritative, so ketju
// reindex leaves both exactly as they were, read rows and offsets included.
// Rebuilding the projection is not a reason to alert anyone again, or to
// move where the generator resumes.
func TestReindexWritesNoNotifications(t *testing.T) {
	data, ids := populate(t, 4)
	d := openDB(t, data)
	execDB(t, d, `INSERT INTO notifications (user_id, kind, thread_id, ordinal, at, read_at) VALUES
		(3, 'mention', ?, 1, 5, NULL), (3, 'thread_you_watch', ?, 2, 6, 7)`, int64(ids[0]), int64(ids[1]))
	execDB(t, d, `INSERT INTO notification_progress (thread_id, log_length, updated_at) VALUES (?, 64, 1), (?, 99, 2)`,
		int64(ids[2]), int64(ids[3]))
	before := notificationRows(t, d)
	d.Close()
	if strings.Count(before, "\n") != 4 {
		t.Fatalf("the setup wrote:\n%s", before)
	}
	if _, errOut, err := reindexRun(t, reindexOpts{}, "--root", data); err != nil {
		t.Fatalf("reindex: %v\n%s", err, errOut)
	}
	d = openDB(t, data)
	defer d.Close()
	if after := notificationRows(t, d); after != before {
		t.Fatalf("reindex changed the notifications:\n%s\nwant:\n%s", after, before)
	}
}
