//go:build e2e

// The e2e build of ketju serves the Playwright smoke suite (TKT-01M29CGBY1,
// web/client/e2e): `ketju e2e -dir DIR -port N` makes a fresh data
// directory, seeds it, starts the fake OpenID provider on loopback, and runs
// the real serve against both, signing in through the provider's own login
// page. It is built only with -tags e2e and is never released: the fake
// provider approves anyone who asks (internal/auth/oidc/oidctest), and the
// seed is fixture data.

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/auth/oidc/oidctest"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

func init() { buildCommands["e2e"] = runE2E }

// The seed, which the suite's assertions name. The thread has seedPosts
// posts; carol has read them all and alice none, so a post alice adds is
// carol's first unread.
const (
	seedForum   = "general"
	seedTitle   = "Naming the monthly threads"
	seedSlug    = "naming-the-monthly-threads"
	seedPosts   = 60
	e2eProvider = "fake"
)

var seedUsers = []struct{ handle, name string }{{"alice", "Alice Nakamura"}, {"carol", "Carol Ruiz"}}

// seedApplicants are subjects the fake provider signs in who have no
// account, so registration's default approval mode gives each a pre-user
// session (TKT-01M29CP2NG). There is one per Playwright project, because
// each project submits an application and an identity may submit one a day
// (e2e/apply.spec.ts).
var seedApplicants = []string{"dana", "erin"}

// seedStrangers are Phase 2's exit applicants (TKT-01M3NTSCMN,
// e2e/phase2-exit.spec.ts), one per Playwright project: each applies, a
// moderator approves them from the queue, and they sign in as the member
// the approval made. Like seedApplicants they have no account and no
// identity until they sign in.
var seedStrangers = []string{"stranger-phone", "stranger-desktop"}

// seedModerator reviews applications (e2e/admin-applications.spec.ts), and
// wrote #56 of the main thread, so the thread page draws a role badge
// (e2e/thread.spec.ts). The fake provider signs them in like the members.
const seedModerator = "mona"

// seedAdmin edits the registration form (e2e/admin-forms.spec.ts,
// TKT-01M29CP2P1): the form routes are for admins only.
const seedAdmin = "ada"

// seedMarker is a member who marks the whole site read
// (e2e/unread-lists.spec.ts, TKT-01M3V6Y4JP). The site-level mark only
// moves forward, so nothing can put it back, and no other spec may read
// this member's unread state (Terva review 1743). No stored session is kept
// for them: the spec signs them in itself.
const seedMarker, seedMarkerName = "nell", "Nell Okoro"

// seedPending are pending applications on the active registration form,
// from identities at the fake provider with no account, one per Playwright
// project, since each project approves one. Nobody signs in as them. Their
// ids are in seed.json as applications, by project.
var seedPending = []struct{ project, subject, name string }{
	{"phone", "pat", "Pat Lee"},
	{"desktop", "quinn", "Quinn Okafor"},
}

// seedSettings are the settings screens' members (TKT-01M29CXK4E,
// e2e/settings.spec.ts), one per Playwright project, because the
// identities test removes the identity its member signs in with. No spec
// keeps a stored session for them, so signing them out everywhere costs no
// other spec its sign-in. link is a second subject at the fake provider,
// with no account until the test links it to the member.
var seedSettings = []struct{ handle, name, link string }{
	{"sid-phone", "Sid Phone", "sid-phone-link"},
	{"sid-desktop", "Sid Desktop", "sid-desktop-link"},
}

// seedAccount are the account screen's members (TKT-01M3V6XTYG,
// e2e/account.spec.ts), one per Playwright project, because the test
// renames its member and then deactivates them. No other spec signs in as
// them, and no stored session is kept for them.
var seedAccount = []struct{ handle, name string }{
	{"acct-phone", "Ash Phone"},
	{"acct-desktop", "Ash Desktop"},
}

func runE2E(args []string) error {
	fs := flag.NewFlagSet("e2e", flag.ContinueOnError)
	dir := fs.String("dir", "", "directory to create the data directory and config in; must not exist")
	port := fs.Int("port", 8787, "loopback port to serve on")
	if err := fs.Parse(args); err != nil {
		return errUsage{err}
	}
	if *dir == "" {
		return errUsage{errors.New("e2e needs -dir")}
	}
	if err := os.Mkdir(*dir, 0o750); err != nil {
		return fmt.Errorf("e2e: %w", err)
	}

	idp, err := oidctest.Start()
	if err != nil {
		return fmt.Errorf("e2e: starting the fake provider: %w", err)
	}
	defer idp.Close()
	for _, u := range seedUsers {
		idp.Users = append(idp.Users, u.handle)
	}
	idp.Users = append(idp.Users, seedModerator, seedAdmin, seedMarker)
	idp.Users = append(idp.Users, seedApplicants...)
	idp.Users = append(idp.Users, seedStrangers...)
	for _, u := range seedSettings {
		idp.Users = append(idp.Users, u.handle, u.link)
	}
	for _, u := range seedAccount {
		idp.Users = append(idp.Users, u.handle)
	}

	site := "http://localhost:" + strconv.Itoa(*port)
	data := filepath.Join(*dir, "data")
	path := filepath.Join(*dir, "config.toml")
	cfg := fmt.Sprintf("listen = %q\ndata_dir = %q\nsite_url = %q\n[storage]\nfsync = \"never\"\n"+
		"[[oidc.providers]]\nkey = %q\nname = \"Fake\"\nissuer = %q\nclient_id = %q\nclient_secret = %q\n",
		"127.0.0.1:"+strconv.Itoa(*port), data, site, e2eProvider, idp.URL, oidctest.ClientID, oidctest.ClientSecret)
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		return fmt.Errorf("e2e: %w", err)
	}

	ctx, stop := signalContext()
	defer stop()
	thread, err := seed(ctx, data, idp.URL)
	if err != nil {
		return fmt.Errorf("e2e: seeding: %w", err)
	}
	applications, err := seedReview(ctx, data, idp.URL)
	if err != nil {
		return fmt.Errorf("e2e: seeding the review: %w", err)
	}
	// The suite reads what was seeded from here rather than repeating it.
	fixture, _ := json.Marshal(map[string]any{"thread": "/t/" + thread.String() + "/" + seedSlug,
		"title": seedTitle, "posts": seedPosts, "users": idp.Users, "applications": applications,
		"restricted_word": seedRestrictedWord})
	if err := os.WriteFile(filepath.Join(*dir, "seed.json"), fixture, 0o600); err != nil {
		return fmt.Errorf("e2e: %w", err)
	}
	// The request log goes to a file beside the data, where a failed run can
	// be read, rather than into the test runner's output.
	logPath := filepath.Join(*dir, "serve.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("e2e: %w", err)
	}
	defer logFile.Close()
	fmt.Fprintf(os.Stderr, "e2e: logging to %s\n", logPath)
	return serve(ctx, path, serveOpts{
		idpClient: idp.Client(),
		logOut:    logFile,
		ready:     func(addr string) { fmt.Fprintf(os.Stderr, "e2e: serving %s\n", site) },
	})
}

// seedReplies is which post each of the main thread's replies answers, for
// the reply expander (TKT-01M29CGBW6), at 25 posts a page: #3's only reply
// is #4, directly below, so it offers none; #5 has two on its own page; #25's
// only reply opens page 2; and #1's two are on pages 2 and 3.
var seedReplies = map[int]uint32{4: 3, 6: 5, 8: 5, 26: 25, 30: 1, 55: 1}

// The main thread's page 3 carries the parts of the mockups' thread page
// that members' plain posts cannot (TKT-01M3S3ZZQD, e2e/thread.spec.ts),
// on ordinals no other spec names: seedAuthors writes #56 as mona, so it
// carries her role badge; seedEdited is edited by its author, alice, past
// the grace window, so it carries the edited mark; and seedRemoved is
// deleted by its author, carol, so everyone but a moderator sees its
// tombstone. Every other post keeps the alternation, alice's even and
// carol's odd.
var seedAuthors = map[int]string{56: seedModerator}

const (
	seedEdited  = 52
	seedRemoved = 57
	// seedQuotesFirst quotes #1 from page 2, as #2 does from page 1, so #1
	// shows "Quoted by #2 #44" and one backlink opens the popover
	// (e2e/quoted-by.spec.ts, TKT-01M3YTHAZB).
	seedQuotesFirst = 44
)

// seedForums is the forum tree the fixture adds around seedForum, shaped
// like the mockup's front page (docs/mockups Main.dc.html) so the forum
// index's screenshots have something to compare: roots with children, a
// members-only forum, a restricted one alice cannot see, and an empty one.
var seedForums = []struct {
	slug, name, description, visibility, parent string
	position                                    int64
}{
	{"introductions", "Introductions", "Say hello when you join; one thread per person", domain.VisibilityPublic, seedForum, 0},
	{"events", "Meetups and events", "Planning, RSVPs, and the photos afterwards", domain.VisibilityPublic, seedForum, 1},
	{"projects", "Projects", "Things we are building together", domain.VisibilityPublic, "", 1},
	{"garden", "The garden build", "Raised beds, irrigation, and who is watering this week", domain.VisibilityMembers, "projects", 0},
	{"reading", "Reading group", "One book a month, spoilers behind the cut", domain.VisibilityPublic, "projects", 1},
	{"site-feedback", "Site feedback", "Bugs, requests, and how the forum itself should work", domain.VisibilityPublic, "", 2},
	{"moderators", "Moderators", "Visible to the moderators group", domain.VisibilityRestricted, "", 3},
	{seedChatter, "Chatter", "Off-topic, one line at a time", domain.VisibilityPublic, "", 4},
}

// seedSideThreads are the short threads beside the main one, so the index has
// last posts to show; reading and site-feedback stay empty.
var seedSideThreads = []struct{ forum, title, slug string }{
	{"introductions", "Hi from the far side of the river", "hi-from-the-far-side-of-the-river"},
	{"events", "October bonfire: date poll closes Friday", "october-bonfire-date-poll-closes-friday"},
	{"garden", "Drip line pressure is dropping at the far bed", "drip-line-pressure-is-dropping-at-the-far-bed"},
}

// The thread list's seed (TKT-01M29CEE6D), in seedForum beside the main
// thread: a pinned thread; a thread alice has read to seedBadgeRead, whose
// badge is a permalink, and which nothing else in the suite reads; and a
// thread with a 150-character title and six tags, for the 390 px rows.
// seedChatter holds seedChatterThreads one-post threads, so its list pages.
const (
	seedPinnedTitle    = "House rules: read these before your first post"
	seedBadgeTitle     = "Minutes from the September meeting"
	seedBadgeRead      = 30
	seedChatter        = "chatter"
	seedChatterThreads = 60
)

// seedRestrictedWord is held by one post, mona's in the restricted
// moderators forum, which alice cannot read, and by nothing else: Phase 3's
// exit search test (TKT-01M3V3CFRE, e2e/phase3-exit.spec.ts) has alice search
// for it and find nothing. The thread is older than everything else, so no
// list or index a member reads changes.
const seedRestrictedWord = "quartzite"

var (
	seedLongTitle = "Comparing every rain barrel, diverter, first-flush filter and overflow fitting we " +
		"tried on the shed roof over two summers, with photos, notes, budgets"
	seedLongTags = []string{"garden", "water", "hardware", "long-read", "photos", "budgets"}
)

// seed writes the fixture: two members and seedModerator with identities at
// the fake provider, the forum tree, one thread of seedPosts posts in
// seedForum, all read by carol, a few short threads elsewhere, and the thread list's
// threads (seedPinnedTitle and the rest). serve projects the
// threads when it starts, as it would any log it finds. It answers the main
// thread's id.
func seed(ctx context.Context, data, issuer string) (threadid.ID, error) {
	for _, sub := range []string{threadid.ThreadsDir, "db"} {
		if err := os.MkdirAll(filepath.Join(data, sub), 0o750); err != nil {
			return 0, err
		}
	}
	d, err := db.Open(ctx, db.Options{Path: filepath.Join(data, "db", "ketju.db")})
	if err != nil {
		return 0, err
	}
	defer d.Close()
	if err := d.Migrate(ctx); err != nil {
		return 0, err
	}

	now := time.Now().UnixMilli()
	users := map[string]int64{}
	forums := map[string]int64{}
	if err := d.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		for _, u := range seedUsers {
			id, err := q.CreateUser(ctx, dbq.CreateUserParams{Handle: u.handle, DisplayName: u.name,
				Status: "active", Role: domain.RoleMember, Kind: domain.KindHuman, CreatedAt: now, UpdatedAt: now})
			if err != nil {
				return err
			}
			users[u.handle] = id
			if _, err := q.CreateIdentity(ctx, dbq.CreateIdentityParams{UserID: sql.NullInt64{Int64: id, Valid: true},
				ProviderKey: e2eProvider, Subject: u.handle, Issuer: issuer, CreatedAt: now, UpdatedAt: now}); err != nil {
				return err
			}
		}
		// mona after alice and carol, so the ids stay as they were when
		// seedReview made her: alice 1, carol 2, mona 3, and ada 4.
		mona, err := q.CreateUser(ctx, dbq.CreateUserParams{Handle: seedModerator, DisplayName: "Mona Varga",
			Status: domain.StatusActive, Role: domain.RoleModerator, Kind: domain.KindHuman, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			return err
		}
		users[seedModerator] = mona
		if _, err := q.CreateIdentity(ctx, dbq.CreateIdentityParams{UserID: sql.NullInt64{Int64: mona, Valid: true},
			ProviderKey: e2eProvider, Subject: seedModerator, Issuer: issuer, CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		id, err := q.CreateForum(ctx, dbq.CreateForumParams{Slug: seedForum, Name: "General",
			Description: "Anything that does not fit elsewhere", Visibility: domain.VisibilityPublic,
			CreatedAt: now, UpdatedAt: now})
		if err != nil {
			return err
		}
		forums[seedForum] = id
		for _, f := range seedForums {
			var parent sql.NullInt64
			if f.parent != "" {
				parent = sql.NullInt64{Int64: forums[f.parent], Valid: true}
			}
			if forums[f.slug], err = q.CreateForum(ctx, dbq.CreateForumParams{ParentID: parent, Slug: f.slug,
				Name: f.name, Description: f.description, Position: f.position, Visibility: f.visibility,
				CreatedAt: now, UpdatedAt: now}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return 0, err
	}
	// Members may upload everywhere, so the composer offers Attach
	// (e2e/attachments.spec.ts, TKT-01M3V73J8K). No migration grants it
	// (docs/07 "Attachments"); an admin would, through perm as this does.
	var members dbq.Group
	if err := d.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) (err error) {
		members, err = q.GetGroupBySlug(ctx, domain.GroupMembers)
		return err
	}); err != nil {
		return 0, err
	}
	if err := perm.New(d).Grant(ctx, audit.System("e2e seed"), 0, members.ID, domain.PermUpload); err != nil {
		return 0, err
	}

	cache, err := threadcache.New(data, threadcache.Options{Sync: threadlog.SyncNever})
	if err != nil {
		return 0, err
	}
	ids := threadid.NewGenerator()
	// thread appends a thread of n posts to forum, a minute apart and ending
	// at end, alternating between the two users, with the tags given, and
	// pinned in its forum if pinned is true. body, if not nil, writes post
	// i instead of the default, and names its author unless it answers "".
	// then is appended after the posts.
	thread := func(forum int64, title, slug string, n int, end int64, tags []string, pinned bool,
		body func(i int) (string, threadlog.PostBody), then ...threadlog.Append) (threadid.ID, error) {
		id, err := ids.New()
		if err != nil {
			return 0, err
		}
		start := end - int64(n)*60_000
		th, err := cache.Create(id, uint64(forum), start)
		if err != nil {
			return 0, err
		}
		defer cache.Release(th)
		if _, err := th.Actor.Append(threadlog.Append{Type: threadlog.TypeThreadCreated, Author: uint64(users["carol"]),
			TimeMS: start, Body: &threadlog.ThreadCreatedBody{Title: title, Slug: slug,
				ForumID: uint64(forum), Tags: tags}}); err != nil {
			return 0, err
		}
		if pinned {
			if _, err := th.Actor.Append(threadlog.Append{Type: threadlog.TypeThreadPinned, Author: uint64(users["carol"]),
				TimeMS: start, Body: &threadlog.ThreadPinnedBody{Scope: "forum"}}); err != nil {
				return 0, err
			}
		}
		for i := 1; i <= n; i++ {
			author := seedUsers[i%2].handle
			post := threadlog.PostBody{Source: fmt.Sprintf("Seeded post %d. Month name, four-digit year, then the topic.", i)}
			if body != nil {
				var who string
				if who, post = body(i); who != "" {
					author = who
				}
			}
			if _, err := th.Actor.Append(threadlog.Append{Type: threadlog.TypePost, Author: uint64(users[author]),
				TimeMS: start + int64(i)*60_000, Body: &post}); err != nil {
				return 0, err
			}
		}
		for _, rec := range then {
			if _, err := th.Actor.Append(rec); err != nil {
				return 0, err
			}
		}
		return id, nil
	}
	// The main thread's second post quotes the first, so its first page has
	// a quote's header, a link and a toggle, for the accessibility checks.
	// Some of its posts answer others (seedReplies), for the reply expander.
	// After the posts, at now, alice edits seedEdited, written 8 minutes
	// earlier and so past the default 300 s grace window
	// (posts.grace_edit_seconds), and carol deletes seedRemoved. Both bodies
	// are the ones threads.Service writes for an author's own edit and
	// delete: the edit is not a grace edit, so the post is marked edited,
	// and the delete is not a moderator's, so the post is deleted rather
	// than hidden.
	// seedQuotesFirst, on page 2, quotes the first post as well, so #1's
	// backlinks name a post on its page and one off it (TKT-01M3YTHAZB).
	mainSource := func(i int) string {
		src := fmt.Sprintf("Seeded post %d. Month name, four-digit year, then the topic.", i)
		if i == 2 || i == seedQuotesFirst {
			src = "> [quote=#1]\n> Seeded post 1.\n\n" + src
		}
		return src
	}
	main, err := thread(forums[seedForum], seedTitle, seedSlug, seedPosts, now, []string{}, false,
		func(i int) (string, threadlog.PostBody) {
			return seedAuthors[i], threadlog.PostBody{Source: mainSource(i), ReplyTo: seedReplies[i]}
		},
		threadlog.Append{Type: threadlog.TypeEdit, Author: uint64(users[seedUsers[seedEdited%2].handle]),
			Ordinal: seedEdited, TimeMS: now, Body: &threadlog.EditBody{
				Source: mainSource(seedEdited) + " (edited in the seed)", ReplyTo: seedReplies[seedEdited],
				Mentions: &[]uint64{}}},
		threadlog.Append{Type: threadlog.TypeDelete, Author: uint64(users[seedUsers[seedRemoved%2].handle]),
			Ordinal: seedRemoved, TimeMS: now, Body: &threadlog.DeleteBody{}},
	)
	for i, t := range seedSideThreads {
		if err != nil {
			break
		}
		// Older than the main thread, a few hours apart.
		_, err = thread(forums[t.forum], t.title, t.slug, 2+i, now-int64(i+1)*3*3600_000, []string{}, false, nil)
	}
	// The thread list's, older than everything above, so the index's last
	// posts are unchanged.
	day := int64(24 * 3600_000)
	var badge threadid.ID
	if err == nil {
		_, err = thread(forums[seedForum], seedPinnedTitle, "house-rules", 1, now-30*day, []string{"meta"}, true, nil)
	}
	if err == nil {
		badge, err = thread(forums[seedForum], seedBadgeTitle, "minutes-from-the-september-meeting",
			seedPosts, now-2*day, []string{"meeting"}, false, nil)
	}
	if err == nil {
		_, err = thread(forums[seedForum], seedLongTitle, "comparing-rain-barrels", 3, now-3*day, seedLongTags, false, nil)
	}
	if err == nil {
		_, err = thread(forums["moderators"], "Keys to the store room", "keys-to-the-store-room", 1, now-60*day,
			[]string{}, false, func(int) (string, threadlog.PostBody) {
				return seedModerator, threadlog.PostBody{Source: "The " + seedRestrictedWord + " samples stay locked in the store room."}
			})
	}
	for i := 0; i < seedChatterThreads && err == nil; i++ {
		_, err = thread(forums[seedChatter], fmt.Sprintf("Chatter thread %02d", i+1), fmt.Sprintf("chatter-%02d", i+1),
			1, now-int64(i+1)*3600_000, []string{}, false, nil)
	}
	if err := errors.Join(err, cache.Close()); err != nil {
		return 0, err
	}

	return main, d.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		if err := q.UpsertReadPosition(ctx, dbq.UpsertReadPositionParams{UserID: users["carol"], ThreadID: int64(main),
			LastReadOrdinal: seedPosts, LastReadAt: now}); err != nil {
			return err
		}
		return q.UpsertReadPosition(ctx, dbq.UpsertReadPositionParams{UserID: users["alice"], ThreadID: int64(badge),
			LastReadOrdinal: seedBadgeRead, LastReadAt: now})
	})
}

// seedReview writes what the admin screens need: seedAdmin for the form
// editor (TKT-01M29CP2P1), then seedMarker, who has nothing to do with
// review and is here only because this runs after seed() made the first
// four accounts; and for the applications review (TKT-01M3NRX2NN)
// seedPending, each a pending application from an identity with no account,
// answered on the registration form's active version, a minute apart. The
// review's moderator, seedModerator, is seed()'s, since she also posts in
// the main thread. It answers the applications' ids by project.
func seedReview(ctx context.Context, data, issuer string) (map[string]int64, error) {
	d, err := db.Open(ctx, db.Options{Path: filepath.Join(data, "db", "ketju.db")})
	if err != nil {
		return nil, err
	}
	defer d.Close()
	now := time.Now().UnixMilli()
	ids := map[string]int64{}
	err = d.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		ada, err := q.CreateUser(ctx, dbq.CreateUserParams{Handle: seedAdmin, DisplayName: "Ada Brandt",
			Status: domain.StatusActive, Role: domain.RoleAdmin, Kind: domain.KindHuman, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			return err
		}
		if _, err := q.CreateIdentity(ctx, dbq.CreateIdentityParams{UserID: sql.NullInt64{Int64: ada, Valid: true},
			ProviderKey: e2eProvider, Subject: seedAdmin, Issuer: issuer, CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		// seedMarker after ada, so the earlier ids stay as they were.
		marker, err := q.CreateUser(ctx, dbq.CreateUserParams{Handle: seedMarker, DisplayName: seedMarkerName,
			Status: domain.StatusActive, Role: domain.RoleMember, Kind: domain.KindHuman, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			return err
		}
		if _, err := q.CreateIdentity(ctx, dbq.CreateIdentityParams{UserID: sql.NullInt64{Int64: marker, Valid: true},
			ProviderKey: e2eProvider, Subject: seedMarker, Issuer: issuer, CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		// The settings members after seedMarker, so the ids above stay as they were.
		for _, u := range seedSettings {
			id, err := q.CreateUser(ctx, dbq.CreateUserParams{Handle: u.handle, DisplayName: u.name,
				Status: domain.StatusActive, Role: domain.RoleMember, Kind: domain.KindHuman, CreatedAt: now, UpdatedAt: now})
			if err != nil {
				return err
			}
			if _, err := q.CreateIdentity(ctx, dbq.CreateIdentityParams{UserID: sql.NullInt64{Int64: id, Valid: true},
				ProviderKey: e2eProvider, Subject: u.handle, Issuer: issuer, CreatedAt: now, UpdatedAt: now}); err != nil {
				return err
			}
		}
		// The account members after the settings members, so the ids above
		// stay as they were.
		for _, u := range seedAccount {
			id, err := q.CreateUser(ctx, dbq.CreateUserParams{Handle: u.handle, DisplayName: u.name,
				Status: domain.StatusActive, Role: domain.RoleMember, Kind: domain.KindHuman, CreatedAt: now, UpdatedAt: now})
			if err != nil {
				return err
			}
			if _, err := q.CreateIdentity(ctx, dbq.CreateIdentityParams{UserID: sql.NullInt64{Int64: id, Valid: true},
				ProviderKey: e2eProvider, Subject: u.handle, Issuer: issuer, CreatedAt: now, UpdatedAt: now}); err != nil {
				return err
			}
		}
		form, err := q.GetFormByKey(ctx, "registration")
		if err != nil {
			return err
		}
		for i, p := range seedPending {
			at := now - int64(len(seedPending)-i)*60_000
			email := p.subject + "@example.test"
			identity, err := q.CreateIdentity(ctx, dbq.CreateIdentityParams{ProviderKey: e2eProvider, Subject: p.subject,
				Email: sql.NullString{String: email, Valid: true}, EmailVerified: 1, Issuer: issuer, CreatedAt: at, UpdatedAt: at})
			if err != nil {
				return err
			}
			answers, err := json.Marshal(map[string]any{"name": p.name, "email": email,
				"who": "Alice invited me to the reading group.", "rules": true})
			if err != nil {
				return err
			}
			if ids[p.project], err = q.InsertApplication(ctx, dbq.InsertApplicationParams{
				IdentityID: sql.NullInt64{Int64: identity, Valid: true}, FormID: form.ID, FormVersion: form.ActiveVersion,
				AnswersJson: string(answers), CreatedAt: at}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}
