package notifier

import (
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// TestMoveNotifiesNobody (TKT-01M29CXJZZ): a move's copies notify nobody,
// a watcher of the target included, and neither does a moderator's Edit
// carried onto a copy, though it names a member, whether the source's split
// is projected before it or after. A later edit of the finished copy
// notifies as any edit does.
func TestMoveNotifiesNobody(t *testing.T) {
	f := newFixture(t)
	src, srcCreated := f.thread(f.forum)
	dst, dstCreated := f.thread(f.forum)
	f.watch("carol", dst, 0, domain.WatchWatch)
	f.deliver(srcCreated, dstCreated,
		f.post(src, "alice", "hello", 0, "bob"),
		f.edit(src, "mod", 1, []string{"bob", "dave"}))
	f.want("bob mention #1 by alice", "dave mention #1 by mod")

	mod := uint64(f.users["mod"])
	split := f.append(src, threadlog.Append{Type: threadlog.TypePostsSplitOut, Author: mod,
		Body: &threadlog.PostsSplitOutBody{Ordinals: []uint32{1}, TargetThreadID: uint64(dst)}})
	merged := f.append(dst, threadlog.Append{Type: threadlog.TypePostsMergedIn, Author: mod,
		Body: &threadlog.PostsMergedInBody{SourceThreadID: uint64(src), Ordinals: []uint32{1}}})
	copied := f.append(dst, threadlog.Append{Type: threadlog.TypePost, Author: uint64(f.users["alice"]),
		Body: &threadlog.PostBody{Source: "hello", Mentions: []uint64{uint64(f.users["bob"])},
			Origin: &threadlog.PostOrigin{ThreadID: uint64(src), Ordinal: 1}}})
	ids := []uint64{uint64(f.users["bob"]), uint64(f.users["dave"]), uint64(f.users["carol"])}
	carried := f.append(dst, threadlog.Append{Type: threadlog.TypeEdit, Author: mod, Ordinal: 1,
		Body: &threadlog.EditBody{Source: "edited", Mentions: &ids}})
	// The target before the source: the copy's source revision is unknown,
	// and the Edit is carried all the same.
	f.deliver(merged, copied, carried)
	f.deliver(split)
	f.want("bob mention #1 by alice", "dave mention #1 by mod")

	// Revision 3 is past the source's 2: a new edit, which notifies.
	f.deliver(f.edit(dst, "mod", 1, []string{"carol"}))
	f.want("bob mention #1 by alice", "dave mention #1 by mod", "carol mention #1 by mod")
}
