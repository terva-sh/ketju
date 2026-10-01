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

import "encoding/json"

func ptr[T any](v T) *T { return &v }

const (
	tAnon      = "anon"
	tMember    = "member"
	tModerator = "moderator"
	// tPreUser is an approval-mode applicant; only /me has a shape for one.
	tPreUser = "pre_user"
	// tAdmin is an admin; the admin routes have a shape for no one else.
	tAdmin = "admin"
	// tAgent is an agent authenticated by its credential, not a session
	// (TKT-01M3QH3X58). It reads every route as the member it is, so only
	// /me, where it checks who it is signed in as, has a golden for it.
	tAgent = "agent"
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
				Ordinal:   61,
				Revision:  2,
				Author:    alice,
				CreatedAt: 1757500100000,
				EditedAt:  ptr(Millis(1757500200000)),
				// Its second revision was a moderator's (TKT-01M29CRJ5J).
				EditedByModerator: true,
				ReplyTo:           ptr(57),
				ReplyToSuppressed: false,
				ReplyCount:        3,
				HTML:              "<p>The bill was the smaller half of it.</p>",
				// An image, with its dimensions, and a file that is not one,
				// whose dimensions are null (TKT-01M29CXK0K).
				Attachments: []Attachment{{
					Hash:         "a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90",
					Size:         18422,
					MIME:         "image/png",
					OriginalName: "retention-settings.png",
					Width:        ptr(1280),
					Height:       ptr(720),
				}, {
					Hash:         "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c4b5a69788796a5b4c3d2e1f0",
					Size:         90112,
					MIME:         "application/pdf",
					OriginalName: "invoice-march.pdf",
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
				Removal:           ptr(RemovalModerator),
				MovedTo:           nil,
				MachineAuthored:   false,
			},
			{
				// A moved post: a pointer to the thread it went to, with no
				// removal and no body for anyone.
				Ordinal:           64,
				Revision:          1,
				Author:            alice,
				CreatedAt:         1757500500000,
				EditedAt:          nil,
				ReplyTo:           nil,
				ReplyToSuppressed: false,
				ReplyCount:        0,
				HTML:              "",
				Attachments:       []Attachment{},
				Deleted:           true,
				Removal:           nil,
				MovedTo:           ptr("3yymw47g00099"),
				MachineAuthored:   false,
			},
		},
	}

	if class == tModerator {
		// A hard-redacted post (TKT-01M3R1WPNS): a moderator's removal that
		// can never come back. Every class reads it exactly so, with no body
		// (internal/web/api redact_test.go holds that). It is on the
		// moderator's page alone, beside #63, whose body a moderator does
		// read, and so that the member page the client tests count stays
		// as it was.
		p.Posts = append(p.Posts, Post{
			Ordinal:           65,
			Revision:          2,
			Author:            alice,
			CreatedAt:         1757500600000,
			EditedAt:          nil,
			ReplyTo:           nil,
			ReplyToSuppressed: false,
			ReplyCount:        0,
			HTML:              "",
			Attachments:       []Attachment{},
			Deleted:           true,
			Removal:           ptr(RemovalModerator),
			Redacted:          true,
			MovedTo:           nil,
			MachineAuthored:   false,
		})
	}

	switch class {
	case tAnon:
		p.Viewer = ThreadViewer{LastReadOrdinal: 0, Watch: "", CanReply: false, CanModerate: false,
			SlowModeSeconds: 30, NextPostAt: nil}
	case tMember:
		// In slow mode, and waiting: the member's last post was at
		// 1757500500000, and the thread asks 30 s between posts. The member
		// started the thread, so their rename window runs to a day after
		// its start. They moderate nothing, so the author windows bind them,
		// at their defaults.
		p.Viewer = ThreadViewer{LastReadOrdinal: 60, Watch: "watch", CanReply: true, CanUpload: true, CanModerate: false,
			SlowModeSeconds: 30, NextPostAt: ptr(Millis(1757500530000)), RenameUntil: ptr(Millis(1757586400000)),
			AuthorWindows: &AuthorWindows{EditSeconds: 86400, DeleteSeconds: 900}}
	case tModerator:
		// Exempt: the interval is shown, and there is nothing to wait for.
		p.Viewer = ThreadViewer{LastReadOrdinal: 42, Watch: "mute", CanReply: true, CanModerate: true,
			SlowModeSeconds: 30, NextPostAt: nil}
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
		NextCursor: "YXwxfDE3NTc0MDAwMDAwMDB8NDU3NDE0MDA0MDg1NDExMDIxMHw0NTc0MTQwMDQwODU0MTEwMjA5",
	}
}

// forumTree exercises the one level of nesting docs/04 allows, a leaf with no
// children, an archived forum, and a forum with no threads, whose last post is
// null. An anonymous viewer is shown public forums only (TKT-01M29CA9A5), so
// the members-only child is the member's alone.
func forumTree(class string) ForumTree {
	general := Forum{
		Slug:        "general",
		Name:        "General",
		Description: "Anything that does not fit elsewhere",
		Visibility:  "public",
		Archived:    false,
		ThreadCount: 418,
		PostCount:   9637,
		LastPost: &ForumLastPost{
			Thread: ThreadLink{ID: "3yymw47g00001", Slug: "naming-the-monthly-threads",
				Title: "Naming the monthly threads"},
			Author: alice,
			At:     1757500400000,
		},
		Children: []Forum{{
			Slug:        "general-events",
			Name:        "Events",
			Description: "Meetups and calls",
			Visibility:  "public",
			Archived:    false,
			Children:    []Forum{},
		}},
	}
	archive := Forum{
		Slug:        "archive-2024",
		Name:        "Archive 2024",
		Description: "Read only",
		Visibility:  "public",
		Archived:    true,
		ThreadCount: 1204,
		PostCount:   31980,
		LastPost: &ForumLastPost{
			Thread: ThreadLink{ID: "3yymw47g00007", Slug: "year-end-wrap-up", Title: "Year-end wrap-up"},
			Author: bob,
			At:     1735600000000,
		},
		Children: []Forum{},
	}
	if class != tAnon {
		// upload is granted per forum, and to nobody anonymous.
		general.CanUpload = true
		general.Unread = 7
		general.Children = append(general.Children, Forum{
			Slug:        "general-offtopic",
			Name:        "Off topic",
			Description: "",
			Visibility:  "members",
			Archived:    false,
			ThreadCount: 96,
			PostCount:   1130,
			Unread:      2,
			LastPost: &ForumLastPost{
				Thread: ThreadLink{ID: "3yymw47g00009", Slug: "what-are-you-reading", Title: "What are you reading?"},
				Author: bob,
				At:     1757490000000,
			},
			Children: []Forum{},
		})
	}
	return ForumTree{Forums: []Forum{general, archive}}
}

// me is the viewer. The anonymous case is the one that matters most: a null
// user is the whole of how a client learns it is signed out. The site name
// is the same for every class, one an admin set, since every viewer is
// shown it.
func me(class string) Me {
	switch class {
	case tAnon:
		return Me{
			User:          nil,
			Unread:        UnreadCounts{},
			Notifications: 0,
			Settings:      Settings{Timezone: "UTC", Locale: "en", PageSize: 25},
			SiteName:      "The Supper Club",
		}
	case tPreUser:
		return Me{
			User:          nil,
			PreUser:       &PreUser{Provider: "github", Tag: "5b0e2c9d71a4f38e6c1d0b27"},
			Unread:        UnreadCounts{},
			Notifications: 0,
			Settings:      Settings{Timezone: "UTC", Locale: "en", PageSize: 25},
			SiteName:      "The Supper Club",
		}
	case tModerator:
		return Me{
			User:          &bob,
			Unread:        UnreadCounts{Threads: 3, Participated: 1, Watching: 2},
			Notifications: 9,
			Queue:         ptr(4),
			Settings:      Settings{Timezone: "Asia/Tokyo", Locale: "ja", PageSize: 50},
			SiteName:      "The Supper Club",
		}
	case tAgent:
		return Me{
			User:          &scribe,
			Unread:        UnreadCounts{Threads: 7, Participated: 3, Watching: 1},
			Notifications: 4,
			Settings:      Settings{Timezone: "UTC", Locale: "en", PageSize: 40},
			SiteName:      "The Supper Club",
		}
	default:
		return Me{
			User:          &alice,
			Unread:        UnreadCounts{Threads: 12, Participated: 5, Watching: 4},
			Notifications: 2,
			Settings:      Settings{Timezone: "Europe/Helsinki", Locale: "fi", PageSize: 25},
			SiteName:      "The Supper Club",
		}
	}
}

// lockedThread is the other shape a thread response takes: locked, unpinned,
// and with no lens applied, so every pointer the main case fills is nil here,
// apart from the author windows.
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
		Page: Pagination{Number: 1, Size: 25, TotalPages: 1},
		Lens: nil,
		// The lock takes reply away and leaves the author windows: a member
		// may still delete and restore their own post in a locked thread.
		Viewer: ThreadViewer{LastReadOrdinal: 2, Watch: "", CanReply: false, CanModerate: false, SlowModeSeconds: 0, NextPostAt: nil,
			AuthorWindows: &AuthorWindows{EditSeconds: 86400, DeleteSeconds: 900}},
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

// archivedForumThread is the same thread with its forum archived and the lock
// lifted: reply is gone, and forum_read_only says the member's own posts
// take no edit, delete or restore either, though the author windows are
// still sent (TKT-01M3QHKD21).
func archivedForumThread() ThreadPage {
	p := lockedThread()
	p.Thread.Locked = false
	p.Viewer.ForumReadOnly = true
	return p
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

// postHistory is post 61's history. The author's grace edit, made a minute
// after posting, and a moderator's edit with a reason sit beside the
// original, so actor, reason and grace are all seen; the deleted variant is
// what a member gets for a tombstoned post.
func postHistory(class string) PostHistory {
	h := PostHistory{Ordinal: 61, Revisions: []PostRevision{
		{Revision: 1, Actor: alice, At: 1757500100000, Reason: "",
			Source: "The bill was the smaller half of it."},
		{Revision: 2, Actor: alice, At: 1757500160000, Reason: "",
			Source: "The bill was the *smaller* half of it.", Grace: true},
		{Revision: 3, Actor: bob, At: 1757500200000, Reason: "fixed the emphasis",
			Source: "The bill was the **smaller** half of it."},
	}}
	if class == tMember {
		// The member case is the tombstone, which is the shape difference.
		// It is the redacted post's (TKT-01M3R1WPNS), a tombstone to a
		// member as any removed post's is, which also says it cannot come
		// back.
		h = PostHistory{Ordinal: 65, Deleted: true, Redacted: true, Revisions: []PostRevision{}}
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
	"me." + tPreUser:   me(tPreUser),
	"me." + tAgent:     me(tAgent),

	"post." + tMember:    singlePost(tMember),
	"post." + tModerator: singlePost(tModerator),

	"replies." + tMember:    postReplies(tMember),
	"replies." + tModerator: postReplies(tModerator),

	"history." + tMember:    postHistory(tMember),
	"history." + tModerator: postHistory(tModerator),

	"post-source." + tMember: PostSource{Ordinal: 61, Revision: 2,
		Source: "The bill was the **smaller** half of it."},

	"thread-locked." + tMember:   lockedThread(),
	"thread-archived." + tMember: archivedForumThread(),

	"slow-mode." + tModerator: SlowMode{Seconds: 30},

	"render." + tMember:         renderReply(),
	"render-plain." + tMember:   renderPlain(),
	"users." + tMember:          userSuggestions(),
	"created-post." + tMember:   createdPost(),
	"created-thread." + tMember: createdThread(),
	"unread." + tMember:         crossForumList(),
	"read." + tMember:           ReadPosition{LastReadOrdinal: 62},
	"flag-receipt." + tMember:   FlagReceipt{Received: true},
	"draft." + tMember:          draftReply(),
	"draft-plain." + tMember:    draftPlain(),

	"forum-draft." + tMember:          forumDraft(),
	"forum-draft-untitled." + tMember: forumDraftUntitled(),

	"profile." + tMember:            profile(tMember),
	"profile." + tModerator:         profile(tModerator),
	"profile-anonymized." + tMember: anonymizedProfile(),

	"user-posts." + tMember:     userPosts(tMember),
	"user-posts." + tModerator:  userPosts(tModerator),
	"user-posts-own." + tMember: userPostsOwn(),

	"form-admin." + tAdmin:   formAdmin(),
	"form-preview." + tAdmin: formPreview(),

	"apply-form." + tPreUser:             applicationForm(),
	"apply-status-none." + tPreUser:      ApplicationStatus{Status: "none", Provider: "github", CanApply: true},
	"apply-status-pending." + tPreUser:   applicationPending(),
	"apply-status-withdrawn." + tPreUser: applicationWithdrawn(),
	"apply-status-approved." + tPreUser:  applicationApproved(),
}

// applicationForm is GET /apply/form for an applicant whose IdP gave a
// name and an email: the registration form, and both claims to prefill.
func applicationForm() ApplicationForm {
	return ApplicationForm{Form: registrationForm(),
		Prefill: map[string]string{"name": "Carol Applicant", "email": "carol@example.com"}}
}

// applicationPending is GET /apply/status with an application under review:
// it may be withdrawn, and nothing may be submitted.
func applicationPending() ApplicationStatus {
	return ApplicationStatus{Status: "pending", Provider: "github", SubmittedAt: ptr(Millis(1757600000000)),
		CanWithdraw: true}
}

// applicationWithdrawn is GET /apply/status an hour after a withdrawal: a new
// application may be submitted 24 hours after the last one was.
func applicationWithdrawn() ApplicationStatus {
	return ApplicationStatus{Status: "withdrawn", Provider: "github", SubmittedAt: ptr(Millis(1757600000000)),
		DecidedAt: ptr(Millis(1757603600000)), CanApplyAt: ptr(Millis(1757686400000))}
}

// applicationApproved is GET /apply/status once the identity has a user: the
// applicant signs in again, as a member.
func applicationApproved() ApplicationStatus {
	return ApplicationStatus{Status: "approved", Provider: "github", SubmittedAt: ptr(Millis(1757600000000)),
		DecidedAt: ptr(Millis(1757610000000)), SignIn: ptr("/auth/github/start?return_to=/")}
}

// registrationForm is the seeded registration form, version 1, as a client
// renders it: a prefilled name, a locked email, and no notice.
func registrationForm() Form {
	return Form{
		Key: "registration", Version: 1, Title: "Apply to join", IntroHTML: "<p>Tell us a bit about yourself…</p>\n",
		Fields: []FormField{
			{Key: "name", Type: "text", Label: "Name", Required: true, Max: 200, Options: []string{}, FromClaim: "name"},
			{Key: "email", Type: "email", Label: "Email", Max: 254, Options: []string{}, Locked: true, FromClaim: "email"},
			{Key: "who", Type: "textarea", Label: "Who invited you or how did you find us?", Required: true, Max: 1000, Options: []string{}},
			{Key: "rules", Type: "checkbox", Label: "I have read the guidelines", Required: true, Options: []string{}},
		},
	}
}

// formAdmin is GET /admin/forms/registration after an admin saved version
// 2: the history newest first, the seeded version with no author, and the
// active version rendered beside its stored schema.
func formAdmin() FormAdmin {
	f := formPreview()
	f.Version = 2
	return FormAdmin{
		Key: "registration", Title: "Registration", ActiveVersion: 2,
		Versions: []FormVersionInfo{
			{Version: 2, CreatedAt: 1757600000000, CreatedBy: &Author{Handle: "root", DisplayName: "Site Admin", Role: "admin", Kind: "human"}},
			{Version: 1, CreatedAt: 0, CreatedBy: nil},
		},
		Form: f,
		Schema: json.RawMessage(`{"title":"Apply to join","intro_markdown":"Tell us a bit about yourself…","fields":[` +
			`{"key":"name","type":"text","label":"Name","required":true,"from_claim":"name"},` +
			`{"key":"email","type":"email","label":"Email","from_claim":"email","locked":true},` +
			`{"key":"who","type":"textarea","label":"Who invited you or how did you find us?","required":true,"max":1000},` +
			`{"key":"rules","type":"checkbox","label":"I have read the guidelines","required":true},` +
			`{"key":"interests","type":"select","label":"Interests","options":["Games","Books","Hiking"],"multiple":true},` +
			`{"key":"welcome","type":"markdown_notice","markdown":"Be **kind**."}]}`),
	}
}

// formPreview is POST /admin/forms/registration/preview: an unsaved draft,
// version 0, with a select and a notice beside the seeded fields.
func formPreview() Form {
	f := registrationForm()
	f.Version = 0
	f.Fields = append(f.Fields,
		FormField{Key: "interests", Type: "select", Label: "Interests", Options: []string{"Games", "Books", "Hiking"}, Multiple: true},
		FormField{Key: "welcome", Type: "markdown_notice", Options: []string{}, HTML: "<p>Be <strong>kind</strong>.</p>\n"},
	)
	return f
}

// profile is GET /users/alice. The two classes differ where docs/07 says:
// a moderator is shown the account's status, and counts the post in the
// staff forum a member cannot read.
func profile(class string) Profile {
	p := Profile{User: alice, BioHTML: "<p>Runs the <em>book club</em>.</p>\n",
		JoinedAt: 1757000000000, PostCount: 41}
	if class == tModerator {
		p.PostCount = 42
		p.Status = ptr("active")
	}
	return p
}

// anonymizedProfile is GET /users/deleted-user-7: the placeholder handle and
// nothing that named the person, with no bio.
func anonymizedProfile() Profile {
	return Profile{User: Author{Handle: "deleted-user-7", Role: "member", Kind: "human"},
		JoinedAt: 1757000000000, PostCount: 12}
}

// userPosts is GET /users/alice/posts: two posts in two forums, newest
// first, and a deleted one between them that a member does not see and a
// moderator sees with its body.
func userPosts(class string) UserPosts {
	newer := threadPage(tMember).Posts[1]
	newer.Author, newer.MachineAuthored = alice, false
	older := newer
	older.Ordinal, older.ReplyTo, older.ReplyToSuppressed, older.CreatedAt = 4, nil, false, 1757400000000
	older.HTML = "<p>Welcome, everyone.</p>\n"
	out := UserPosts{Posts: []UserPost{
		{Thread: ThreadLink{ID: "3yymw47g00001", Slug: "why-we-left-slack", Title: "Why we left Slack"},
			Forum: ForumRef{Slug: "general", Name: "General"}, Post: newer},
	}, NextCursor: "MTc1NzQwMDAwMDAwMHwxMjM0NXw0"}
	if class == tModerator {
		gone := older
		gone.Ordinal, gone.CreatedAt, gone.Deleted, gone.Removal = 9, 1757450000000, true, ptr(RemovalAuthor)
		gone.HTML = "<p>the text a moderator still needs to read</p>"
		out.Posts = append(out.Posts, UserPost{
			Thread: ThreadLink{ID: "3yymw47g00002", Slug: "book-club", Title: "Book club"},
			Forum:  ForumRef{Slug: "social", Name: "Social"}, Post: gone})
	}
	out.Posts = append(out.Posts, UserPost{
		Thread: ThreadLink{ID: "3yymw47g00003", Slug: "introductions", Title: "Introductions"},
		Forum:  ForumRef{Slug: "social", Name: "Social"}, Post: older})
	return out
}

// userPostsOwn is alice's own history: the post she deleted is there, marked
// deleted by its author, with no body, as her thread page shows it to her; and it is the
// last page, so there is no cursor.
func userPostsOwn() UserPosts {
	out := userPosts(tModerator)
	out.Posts[1].Post.HTML = ""
	out.NextCursor = ""
	return out
}

// draftReply is GET /drafts/{thread_id} for a reply to post 61, half
// written; draftPlain is one that replies to the thread as a whole.
func draftReply() Draft {
	reply := uint32(61)
	return Draft{ThreadID: "3yymw47g00001", Source: "The bill was the smaller half, but",
		ReplyTo: &reply, UpdatedAt: 1757500300000, Revision: 3}
}

func draftPlain() Draft {
	return Draft{ThreadID: "3yymw47g00001", Source: "Agreed, and",
		ReplyTo: nil, UpdatedAt: 1757500310000, Revision: 1}
}

// forumDraft is GET /forums/{slug}/draft for a new thread whose title is
// half typed, its trailing space kept; forumDraftUntitled is one saved
// before any title was typed, whose title is omitted.
func forumDraft() ForumDraft {
	title := "Moving the book club to "
	return ForumDraft{Forum: "social", Title: &title, Source: "We keep losing the thread in the group chat.",
		UpdatedAt: 1757500320000, Revision: 5}
}

func forumDraftUntitled() ForumDraft {
	return ForumDraft{Forum: "social", Source: "Something about the", UpdatedAt: 1757500330000, Revision: 1}
}

// renderReply is POST /render for a reply that quotes post 61, whose quoted
// words are there, and post 62, which was deleted, and mentions alice.
func renderReply() RenderResult {
	reply := uint32(61)
	return RenderResult{
		// What internal/render produces for
		// "> [quote=#61]\n> the quoted words\n\n> [quote=#62]\n> gone\n\nAgreed, @alice."
		HTML: "<details class=\"quote\" open=\"\">\n<summary>Quote from #61</summary>\n" +
			"<blockquote>\n<p class=\"quote-attribution\"><a href=\"#post-61\" class=\"quote-source\" rel=\"nofollow\">Post #61</a></p>\n<p>the quoted words</p>\n</blockquote>\n</details>\n" +
			"<details class=\"quote\" open=\"\">\n<summary>Quote from #62</summary>\n" +
			"<blockquote>\n<p class=\"quote-attribution\"><a href=\"#post-62\" class=\"quote-source\" rel=\"nofollow\">Post #62</a></p>\n<p class=\"quote-deleted\">This post was deleted.</p>\n</blockquote>\n</details>\n" +
			"<p>Agreed, <a href=\"/u/alice\" class=\"mention\" rel=\"nofollow\">@alice</a>.</p>\n",
		Mentions: []MentionRef{{Handle: "alice"}},
		Quotes:   []QuoteRef{{Ordinal: 61}, {Ordinal: 62, Deleted: true}},
		ReplyTo:  &reply,
	}
}

// renderPlain is POST /render for a post that answers, quotes and mentions
// nothing: the lists are empty rather than null, and reply_to is null.
func renderPlain() RenderResult {
	return RenderResult{HTML: "<p>Hello.</p>\n", Mentions: []MentionRef{}, Quotes: []QuoteRef{}}
}

// userSuggestions is GET /users?prefix=al: two members, one an agent with no
// avatar, in handle order: '-' sorts before every letter.
func userSuggestions() UserSuggestions {
	return UserSuggestions{Users: []Author{
		{Handle: "al-scribe", DisplayName: "Scribe", Avatar: "", Role: "member", Kind: "agent"},
		{Handle: "alice", DisplayName: "Alice Nakamura", Avatar: "b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90a1", Role: "member", Kind: "human"},
	}}
}

// createdPost is POST /threads/{id}/posts for a member's reply to post 61:
// the post as a later read serves it, with where the append left the log.
func createdPost() CreatedPost {
	p := threadPage(tMember).Posts[1]
	p.Author, p.MachineAuthored = alice, false
	return CreatedPost{Post: p, Ordinal: 62, Revision: 1, LogLength: 483920}
}

// createdThread is POST /forums/general/threads: a new thread with no tags,
// unpinned, whose first post is the only one.
func createdThread() CreatedThread {
	p := threadPage(tMember).Posts[1]
	p.Ordinal, p.ReplyTo, p.ReplyToSuppressed = 1, nil, false
	p.Author, p.MachineAuthored, p.CreatedAt = alice, false, 1757500000000
	return CreatedThread{
		Thread: ThreadHeader{
			ID: "3yymw47g00001", Slug: "why-we-left-slack", Title: "Why we left Slack",
			Forum: ForumRef{Slug: "general", Name: "General"}, PostCount: 1, LogLength: 312,
			Tags: []string{}, CreatedAt: 1757500000000,
		},
		Post: CreatedPost{Post: p, Ordinal: 1, Revision: 1, LogLength: 312},
	}
}
