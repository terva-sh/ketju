//go:build !race

package notifier

import (
	"fmt"
	"testing"

	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// TestLargeBacklogCatchesUp (Terva review 1784): a backlog that names more
// posts than SQLite binds in one statement still generates. Alice answers
// her own first post more times than SQLite's default variable limit, which
// notifies nobody but asks for post #1's author once per reply, and Bob's
// mention after them must still arrive in the same delivery.
//
// The backlog takes seconds to append and minutes under the race detector,
// which says nothing about a parameter count, so this file is built without
// -race and CI runs it in a step of its own, as the projection's catch-up
// bound is.
func TestLargeBacklogCatchesUp(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread(f.forum)
	f.deliver(created, f.post(id, "alice", "first", 0))
	const replies = 32_800 // past SQLite's default SQLITE_MAX_VARIABLE_NUMBER, 32766
	for range replies {
		f.append(id, threadlog.Append{Type: threadlog.TypePost, Author: uint64(f.users["alice"]),
			Body: &threadlog.PostBody{Source: "again", ReplyTo: 1}})
	}
	f.deliver(f.post(id, "bob", "@carol", 0, "carol"))
	f.want(fmt.Sprintf("carol mention #%d by bob", replies+2))
}
