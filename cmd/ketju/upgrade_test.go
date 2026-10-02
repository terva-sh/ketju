package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/auth/oidc/oidctest"
	"github.com/terva-sh/ketju/internal/store/db"
)

// upgradeFixture is a data directory the build at ed82f15e wrote: main as
// Phase 4 began, at migration 0024. Its README says how it was made.
const upgradeFixture = "testdata/upgrade/ed82f15e"

// What the fixture's e2e seed wrote (its README).
const (
	upgradeThread = "0387y87c4m000"
	upgradePosts  = 60
)

// upgradeMembers sign in after the upgrade: members the seed made, through
// the identities it gave them at the fake provider.
var upgradeMembers = []string{"alice", "mona", "ada"}

// readmeUpgrade is the README's "Upgrading" section's sh blocks, in order,
// with the list item's indentation taken off.
func readmeUpgrade(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	const heading = "## Upgrading\n"
	_, section, ok := strings.Cut(string(b), heading)
	if !ok {
		t.Fatalf("the README has no %q section", strings.TrimSpace(heading))
	}
	if next := strings.Index(section, "\n## "); next >= 0 {
		section = section[:next]
	}
	var blocks []string
	var cur *strings.Builder
	for line := range strings.Lines(section) {
		trimmed := strings.TrimSpace(line)
		switch {
		case cur == nil && trimmed == "```sh":
			cur = &strings.Builder{}
		case cur != nil && trimmed == "```":
			blocks = append(blocks, cur.String())
			cur = nil
		case cur != nil:
			cur.WriteString(strings.TrimPrefix(line, "   "))
		}
	}
	if cur != nil {
		t.Fatal("an sh block in the upgrade section is not closed")
	}
	if len(blocks) != 3 {
		t.Fatalf("the upgrade section has %d sh blocks, want 3: back up, check, fsck", len(blocks))
	}
	for i, want := range []string{"ketju backup", "ketju config check", "ketju fsck"} {
		if !strings.Contains(blocks[i], want) {
			t.Errorf("sh block %d does not run %q:\n%s", i+1, want, blocks[i])
		}
	}
	return blocks
}

// TestREADMEUpgradeProcedure is the README's upgrade procedure, run as
// written with `ketju` being this test binary run as the real main, against
// a copy of a data directory the build at ed82f15e wrote. Serve is started
// and stopped where the README says to, in this process. Afterwards the
// backup is at the old schema, the database at this binary's, fsck passes,
// the seeded thread reads back whole, and the seeded members sign in.
func TestREADMEUpgradeProcedure(t *testing.T) {
	blocks := readmeUpgrade(t)

	data := restore(t, upgradeFixture)
	if err := os.MkdirAll(filepath.Join(data, "blobs"), 0o750); err != nil {
		t.Fatal(err)
	}
	old, err := db.OpenReadOnly(t.Context(), adminDBPath(data))
	if err != nil {
		t.Fatal(err)
	}
	oldVersion := migrationsRecorded(t, old)
	old.Close()

	idp := oidctest.New(t)
	idp.Users = upgradeMembers
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := fmt.Sprintf("listen = %q\ndata_dir = %q\nsite_url = \"https://forum.example\"\n[storage]\nfsync = \"never\"\n%s",
		freePort(t), data, providerBlock(idp.URL))
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	backupDir := filepath.Join(dir, "backup")

	bin := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(self, filepath.Join(bin, "ketju")); err != nil {
		t.Fatal(err)
	}
	run := func(step, script string) string {
		t.Helper()
		cmd := exec.Command("sh", "-eu", "-c", script)
		cmd.Env = append(os.Environ(),
			"KETJU_AS_MAIN=1", "KETJU_DATA="+data, "KETJU_BACKUP="+backupDir, "KETJU_CONFIG="+path,
			"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("the README's %s failed: %v\n%s", step, err, out)
		}
		return string(out)
	}

	// 2. The backup, as the README has it for an old binary without the
	// command: serve stopped, and the new binary's backup run. (The old
	// serve cannot run here.) The copy is at the old schema.
	out := run("backup", blocks[0])
	if !strings.Contains(out, "(offline)") {
		t.Errorf("the backup's output:\n%s", out)
	}
	bd, err := db.OpenReadOnly(t.Context(), adminDBPath(backupDir))
	if err != nil {
		t.Fatal(err)
	}
	if n := migrationsRecorded(t, bd); n != oldVersion {
		t.Errorf("the backup records %d migrations, want the old binary's %d: it was migrated", n, oldVersion)
	}
	bd.Close()

	// 4. The new binary, and the configuration checked against it.
	out = run("check", blocks[1])
	for _, want := range []string{"ketju ", "(commit ", "config"} {
		if !strings.Contains(out, want) {
			t.Errorf("the check's output lacks %q:\n%s", want, out)
		}
	}

	// 5. serve migrates and catches up.
	r := start(t, path, serveOpts{idpClient: idp.Client()})
	defer r.shutdown(t)
	d, err := db.OpenReadOnly(t.Context(), adminDBPath(data))
	if err != nil {
		t.Fatal(err)
	}
	if n, err := d.Pending(t.Context()); err != nil || n != 0 {
		t.Errorf("after serve started, %d migration(s) are pending (%v)", n, err)
	}
	if n := migrationsRecorded(t, d); n < oldVersion {
		t.Errorf("the database records %d migrations, fewer than the fixture's %d", n, oldVersion)
	} else {
		t.Logf("the fixture was at migration %d; serve brought it to %d", oldVersion, n)
	}
	d.Close()

	// 6. fsck, beside the running serve.
	out = run("fsck", blocks[2])
	if !strings.Contains(out, "no corruption found") {
		t.Errorf("fsck's output:\n%s", out)
	}

	// The seeded members sign in, and the seeded thread reads back whole.
	for _, handle := range upgradeMembers {
		a := newActor(r, handle)
		res := signIn(t, a, idp, handle)
		if res.status != http.StatusSeeOther || res.header.Get("Location") != "/" || a.session() == "" {
			t.Fatalf("%s: signing in answered %d to %q:\n%s", handle, res.status, res.header.Get("Location"), res)
		}
		if m := a.me(t); m.User == nil || m.User.Handle != handle {
			t.Fatalf("%s: /me after signing in is %+v", handle, m)
		}
		if handle != upgradeMembers[0] {
			continue
		}
		var page struct {
			Thread struct {
				PostCount int `json:"post_count"`
			} `json:"thread"`
			Page struct {
				TotalPages int `json:"total_pages"`
			} `json:"page"`
			Posts []threadPost `json:"posts"`
		}
		seen := 0
		for n := 1; ; n++ {
			res := a.do(t, http.MethodGet, fmt.Sprintf("/api/v1/threads/%s?page=%d", upgradeThread, n), nil)
			if res.status != http.StatusOK {
				t.Fatalf("page %d of the seeded thread answered %d:\n%s", n, res.status, res)
			}
			res.decode(t, &page)
			seen += len(page.Posts)
			if n >= page.Page.TotalPages {
				break
			}
		}
		if page.Thread.PostCount != upgradePosts || seen != upgradePosts {
			t.Errorf("the seeded thread has post_count %d and %d posts across its pages, want %d",
				page.Thread.PostCount, seen, upgradePosts)
		}
	}
}
