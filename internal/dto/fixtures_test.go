package dto

// The corpus.
//
// Every case is one endpoint rendered for one viewer class, and the committed
// JSON beside it is the contract. Values are chosen to be recognisable rather
// than realistic: a reader comparing two goldens should be able to see at a
// glance which field moved.
//
// Two rules the cases follow, both enforced by tests rather than by trust:
//
//  1. Every field of every type reachable from a response root is populated by
//     at least one case with a non-zero value. A field nothing sets is a field
//     whose rename no golden catches, which is the failure this whole harness
//     exists to prevent.
//
//  2. No two cases for the same endpoint serialize identically. Two identical
//     goldens mean either the viewer class distinction is not real or the
//     fixture does not exercise it, and there is no version of that worth
//     committing.

func ptr[T any](v T) *T { return &v }

const (
	tAnon      = "anon"
	tMember    = "member"
	tModerator = "moderator"
)

// alice is an ordinary human member.
var alice = Author{
	Handle:      "alice",
	DisplayName: "Alice Nakamura",
	Avatar:      "b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90a1",
	Role:        "member",
	Kind:        "human",
}

// bob is a moderator, so a golden shows the role badge differing from alice's.
var bob = Author{
	Handle:      "bob",
	DisplayName: "Bob Iyer",
	Avatar:      "",
	Role:        "moderator",
	Kind:        "human",
}

// scribe is an agent holding an ordinary member role, which is the combination
// docs/04 keeps possible by making kind orthogonal to role. If Author ever
// collapses the two, this fixture is what stops it quietly.
var scribe = Author{
	Handle:      "scribe",
	DisplayName: "Scribe",
	Avatar:      "",
	Role:        "member",
	Kind:        "agent",
}

// threadPage builds the thread response for one viewer class.
//
// The three differ in exactly the ways docs/07 says they should: an anonymous
// viewer can do nothing and has no read position, a member can reply, and a
// moderator can moderate and sees the body of a deleted post where the others
// see a tombstone with no HTML.
func threadPage(class string) ThreadPage {
	deletedHTML := ""
	if class == tModerator {
		deletedHTML = "<p>the text a moderator still needs to read</p>"
	}

	p := ThreadPage{
		Thread: ThreadHeader{
			ID:    "3yymw47g00001",
			Slug:  "why-we-left-slack",
			Title: "Why we left Slack",
			Forum: ForumRef{Slug: "general", Name: "General"},

			PostCount: 212,
			LogLength: 483920,
			Locked:    false,
			Pinned:    &Pinned{Scope: "forum", Until: ptr(Millis(1760000000000))},
			Tags:      []string{"meta", "tools"},
			CreatedAt: 1757500000000,
		},
		Page: Pagination{Number: 3, Size: 25, TotalPages: 9},
		Lens: &Lens{Kind: "author", Value: "drew", Shown: 7, Hidden: 205},
		Posts: []Post{
			{
				Ordinal:           61,
				Revision:          2,
				Author:            alice,
				CreatedAt:         1757500100000,
				EditedAt:          ptr(Millis(1757500200000)),
				ReplyTo:           ptr(57),
				ReplyToSuppressed: false,
				ReplyCount:        3,
				HTML:              "<p>The bill was the smaller half of it.</p>",
				Attachments: []Attachment{{
					Hash:         "a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90",
					Size:         18422,
					MIME:         "image/png",
					OriginalName: "retention-settings.png",
				}},
				Deleted:         false,
				MovedTo:         nil,
				MachineAuthored: false,
			},
			{
				// A reply to the post directly above, so the client suppresses
				// the backlink but still knows the parent.
				Ordinal:           62,
				Revision:          1,
				Author:            scribe,
				CreatedAt:         1757500300000,
				EditedAt:          nil,
				ReplyTo:           ptr(61),
				ReplyToSuppressed: true,
				ReplyCount:        0,
				HTML:              "<p>Summarising the thread so far.</p>",
				Attachments:       []Attachment{},
				Deleted:           false,
				MovedTo:           nil,
				MachineAuthored:   true,
			},
			{
				// A tombstone, and the one post whose body depends on who is
				// looking.
				Ordinal:           63,
				Revision:          1,
				Author:            bob,
				CreatedAt:         1757500400000,
				EditedAt:          nil,
				ReplyTo:           nil,
				ReplyToSuppressed: false,
				ReplyCount:        0,
				HTML:              deletedHTML,
				Attachments:       []Attachment{},
				Deleted:           true,
				MovedTo:           ptr("3yymw47g00099"),
				MachineAuthored:   false,
			},
		},
	}

	switch class {
	case tAnon:
		p.Viewer = ThreadViewer{LastReadOrdinal: 0, Watch: "", CanReply: false, CanModerate: false}
	case tMember:
		p.Viewer = ThreadViewer{LastReadOrdinal: 60, Watch: "watch", CanReply: true, CanModerate: false}
	case tModerator:
		p.Viewer = ThreadViewer{LastReadOrdinal: 42, Watch: "mute", CanReply: true, CanModerate: true}
	}
	return p
}

// threadList is the forum thread list. Anonymous rows carry no unread state,
// which is the shape difference worth a separate golden.
func threadList(class string) ThreadList {
	rows := []ThreadRow{
		{
			ID:             "3yymw47g00001",
			Slug:           "why-we-left-slack",
			Title:          "Why we left Slack",
			Tags:           []string{"meta", "tools"},
			Author:         alice,
			ReplyCount:     211,
			LastPostAuthor: bob,
			LastPostAt:     1757500400000,
			Pinned:         &Pinned{Scope: "forum", Until: nil},
			Locked:         false,
		},
		{
			ID:             "3yymw47g00002",
			Slug:           "weekly-standup-notes",
			Title:          "Weekly standup notes",
			Tags:           []string{},
			Author:         scribe,
			ReplyCount:     4,
			LastPostAuthor: scribe,
			LastPostAt:     1757400000000,
			Pinned:         nil,
			Locked:         true,
		},
	}
	if class != tAnon {
		rows[0].Unread = 12
		rows[0].LastReadOrdinal = 200
		rows[1].Unread = 4
		rows[1].LastReadOrdinal = 0
	}
	return ThreadList{
		Forum:      &ForumRef{Slug: "general", Name: "General"},
		Threads:    rows,
		NextCursor: "MTc1NzQwMDAwMDAwMHwy",
	}
}

// forumTree exercises the one level of nesting docs/04 allows, a leaf with no
// children, and an archived forum.
func forumTree(class string) ForumTree {
	t := ForumTree{Forums: []Forum{
		{
			Slug:        "general",
			Name:        "General",
			Description: "Anything that does not fit elsewhere",
			Visibility:  "public",
			Archived:    false,
			ThreadCount: 418,
			Children: []Forum{{
				Slug:        "general-offtopic",
				Name:        "Off topic",
				Description: "",
				Visibility:  "members",
				Archived:    false,
				ThreadCount: 96,
				Children:    []Forum{},
			}},
		},
		{
			Slug:        "archive-2024",
			Name:        "Archive 2024",
			Description: "Read only",
			Visibility:  "public",
			Archived:    true,
			ThreadCount: 1204,
			Children:    []Forum{},
		},
	}}
	if class != tAnon {
		t.Forums[0].Unread = 7
		t.Forums[0].Children[0].Unread = 2
	}
	return t
}

// me is the viewer. The anonymous case is the one that matters most: a null
// user is the whole of how a client learns it is signed out.
func me(class string) Me {
	switch class {
	case tAnon:
		return Me{
			User:          nil,
			Unread:        UnreadCounts{},
			Notifications: 0,
			Settings:      Settings{Timezone: "UTC", Locale: "en", PageSize: 25},
		}
	case tModerator:
		return Me{
			User:          &bob,
			Unread:        UnreadCounts{Threads: 3, Participated: 1, Watching: 2},
			Notifications: 9,
			Settings:      Settings{Timezone: "Asia/Tokyo", Locale: "ja", PageSize: 50},
		}
	default:
		return Me{
			User:          &alice,
			Unread:        UnreadCounts{Threads: 12, Participated: 5, Watching: 4},
			Notifications: 2,
			Settings:      Settings{Timezone: "Europe/Helsinki", Locale: "fi", PageSize: 25},
		}
	}
}

// lockedThread is the other shape a thread response takes: locked, unpinned,
// and with no lens applied, so every pointer the main case fills is nil here.
//
// It is a separate endpoint name rather than a fourth viewer class because it
// is not one -- it is the same viewer looking at a different thread, and
// grouping it with the classes would make the "classes must differ" test
// compare two things that have no reason to match.
func lockedThread() ThreadPage {
	return ThreadPage{
		Thread: ThreadHeader{
			ID:        "3yymw47g000a7",
			Slug:      "office-move-decided",
			Title:     "Office move: decided",
			Forum:     ForumRef{Slug: "general", Name: "General"},
			PostCount: 2,
			LogLength: 1841,
			Locked:    true,
			Pinned:    nil,
			Tags:      []string{},
			CreatedAt: 1757100000000,
		},
		Page:   Pagination{Number: 1, Size: 25, TotalPages: 1},
		Lens:   nil,
		Viewer: ThreadViewer{LastReadOrdinal: 2, Watch: "", CanReply: false, CanModerate: false},
		Posts: []Post{{
			Ordinal:     1,
			Revision:    1,
			Author:      bob,
			CreatedAt:   1757100000000,
			EditedAt:    nil,
			ReplyTo:     nil,
			ReplyCount:  1,
			HTML:        "<p>We are taking the Kallio unit.</p>",
			Attachments: []Attachment{},
		}},
	}
}

// crossForumList is /unread and its siblings: the same rows with no forum,
// which is the one field that separates "this forum" from "anywhere".
func crossForumList() ThreadList {
	l := threadList(tMember)
	l.Forum = nil
	l.NextCursor = ""
	return l
}

// singlePost is GET /threads/{id}/posts/{ordinal} for the tombstoned post,
// the one whose body depends on the viewer, as the popover would fetch it.
func singlePost(class string) Post {
	return threadPage(class).Posts[2]
}

// postReplies is the inline expander for post 61, whose replies include the
// tombstone, so the classes differ where docs/07 says they do.
func postReplies(class string) PostReplies {
	page := threadPage(class)
	return PostReplies{Ordinal: 61, Posts: []Post{page.Posts[1], page.Posts[2]}}
}

// postHistory is post 61's history. A moderator's edit with a reason sits
// beside the author's original, so actor and reason are both seen; the
// deleted variant is what a member gets for a tombstoned post.
func postHistory(class string) PostHistory {
	h := PostHistory{Ordinal: 61, Revisions: []PostRevision{
		{Revision: 1, Actor: alice, At: 1757500100000, Reason: "",
			Source: "The bill was the *smaller* half of it."},
		{Revision: 2, Actor: bob, At: 1757500200000, Reason: "fixed the emphasis",
			Source: "The bill was the **smaller** half of it."},
	}}
	if class == tMember {
		// The member case is the tombstone, which is the shape difference.
		h = PostHistory{Ordinal: 63, Deleted: true, Revisions: []PostRevision{}}
	}
	return h
}

// corpus maps a golden file's base name to the value it pins.
//
// The name is "<endpoint>.<viewer class>", and the endpoint half is what the
// "classes must differ" test groups on.
var corpus = map[string]any{
	"thread." + tAnon:      threadPage(tAnon),
	"thread." + tMember:    threadPage(tMember),
	"thread." + tModerator: threadPage(tModerator),

	"threads." + tAnon:   threadList(tAnon),
	"threads." + tMember: threadList(tMember),

	"forums." + tAnon:   forumTree(tAnon),
	"forums." + tMember: forumTree(tMember),

	"me." + tAnon:      me(tAnon),
	"me." + tMember:    me(tMember),
	"me." + tModerator: me(tModerator),

	"post." + tMember:    singlePost(tMember),
	"post." + tModerator: singlePost(tModerator),

	"replies." + tMember:    postReplies(tMember),
	"replies." + tModerator: postReplies(tModerator),

	"history." + tMember:    postHistory(tMember),
	"history." + tModerator: postHistory(tModerator),

	"thread-locked." + tMember: lockedThread(),
	"unread." + tMember:        crossForumList(),
}
