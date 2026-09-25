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

	"github.com/terva-sh/ketju/internal/auth/oidc/oidctest"
	"github.com/terva-sh/ketju/internal/domain"
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
	// The suite reads what was seeded from here rather than repeating it.
	fixture, _ := json.Marshal(map[string]any{"thread": "/t/" + thread.String() + "/" + seedSlug,
		"title": seedTitle, "posts": seedPosts, "users": idp.Users})
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

// seed writes the fixture: two members with identities at the fake
// provider, a public forum, and one thread of seedPosts posts, all read by
// carol. serve projects the thread when it starts, as it would any log it
// finds. It answers the thread's id.
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
	var forum int64
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
		var err error
		forum, err = q.CreateForum(ctx, dbq.CreateForumParams{Slug: seedForum, Name: "General",
			Visibility: domain.VisibilityPublic, CreatedAt: now, UpdatedAt: now})
		return err
	}); err != nil {
		return 0, err
	}

	id, err := threadid.NewGenerator().New()
	if err != nil {
		return 0, err
	}
	cache, err := threadcache.New(data, threadcache.Options{Sync: threadlog.SyncNever})
	if err != nil {
		return 0, err
	}
	start := now - seedPosts*60_000
	th, err := cache.Create(id, uint64(forum), start)
	if err != nil {
		cache.Close()
		return 0, err
	}
	appendAll := func() error {
		defer cache.Release(th)
		if _, err := th.Actor.Append(threadlog.Append{Type: threadlog.TypeThreadCreated, Author: uint64(users["carol"]),
			TimeMS: start, Body: &threadlog.ThreadCreatedBody{Title: seedTitle, Slug: seedSlug,
				ForumID: uint64(forum), Tags: []string{}}}); err != nil {
			return err
		}
		for i := 1; i <= seedPosts; i++ {
			author := seedUsers[i%2].handle
			if _, err := th.Actor.Append(threadlog.Append{Type: threadlog.TypePost, Author: uint64(users[author]),
				TimeMS: start + int64(i)*60_000, Body: &threadlog.PostBody{
					Source: fmt.Sprintf("Seeded post %d. Month name, four-digit year, then the topic.", i)}}); err != nil {
				return err
			}
		}
		return nil
	}
	if err := errors.Join(appendAll(), cache.Close()); err != nil {
		return 0, err
	}

	return id, d.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		return q.UpsertReadPosition(ctx, dbq.UpsertReadPositionParams{UserID: users["carol"], ThreadID: int64(id),
			LastReadOrdinal: seedPosts, LastReadAt: now})
	})
}
