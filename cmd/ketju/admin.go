package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

const adminUsage = `usage: ketju admin <subcommand> [flags]

subcommands:
  promote  make a member an admin
`

const promoteUsage = `usage: ketju admin promote [--root DIR] <handle>

Makes the member with this handle an admin, and records it in the audit log.
It needs only the data directory: no config file, no running server, and no
identity provider. It is how a forum gets its first admin when
bootstrap.admin_subjects and bootstrap.admin_emails name nobody (docs/05).

Safe while ketju serve runs on the same directory: the database serialises
the two writers. A member already an admin is left as they are.

exit codes:
  0  promoted, or already an admin
  1  no member has that handle
  2  usage
  4  the data directory has no database, or the write failed

`

func runAdmin(args []string) error {
	return admin(context.Background(), os.Stdout, os.Stderr, args)
}

func admin(ctx context.Context, out, errOut io.Writer, args []string) error {
	if len(args) == 0 {
		fmt.Fprint(errOut, adminUsage)
		return errUsage{errors.New("admin needs a subcommand")}
	}
	switch args[0] {
	case "promote":
		return adminPromote(ctx, out, errOut, args[1:])
	case "-h", "--help", "help":
		fmt.Fprint(out, adminUsage)
		return nil
	default:
		fmt.Fprint(errOut, adminUsage)
		return errUsage{fmt.Errorf("unknown admin subcommand %q", args[0])}
	}
}

// errNotFound is a name the command was given that names nothing. It exits
// 1, which is otherwise corruption's code; admin has no corruption to report.
type errNotFound struct{ err error }

func (e errNotFound) Error() string { return e.err.Error() }
func (e errNotFound) Unwrap() error { return e.err }

func adminPromote(ctx context.Context, out, errOut io.Writer, args []string) error {
	fs_ := flag.NewFlagSet("admin promote", flag.ContinueOnError)
	fs_.SetOutput(errOut)
	root := fs_.String("root", ".", "data directory")
	fs_.Usage = func() {
		fmt.Fprint(fs_.Output(), promoteUsage)
		fs_.PrintDefaults()
	}
	if err := fs_.Parse(args); err != nil {
		return errUsage{err}
	}
	if fs_.NArg() != 1 {
		fs_.Usage()
		return errUsage{errors.New("admin promote takes exactly one handle")}
	}
	handle := fs_.Arg(0)

	// Opening a database creates it, and a mistyped --root would then leave
	// an empty forum behind and report a handle nobody has. The file has to
	// be there already.
	dbPath := filepath.Join(*root, "db", "ketju.db")
	if _, err := os.Stat(dbPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("admin promote: %s has no database at %s; is --root the data directory?", *root, dbPath)
		}
		return fmt.Errorf("admin promote: %w", err)
	}
	d, err := db.Open(ctx, db.Options{Path: dbPath})
	if err != nil {
		return fmt.Errorf("admin promote: opening %s: %w", dbPath, err)
	}
	defer d.Close()
	// Migrating is what serve does first, and is a no-op on a database it
	// has already brought up to date.
	if err := d.Migrate(ctx); err != nil {
		return fmt.Errorf("admin promote: migrating %s: %w", dbPath, err)
	}

	var u dbq.User
	err = d.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		// handle is COLLATE NOCASE, so this finds Alice as alice.
		u, err = q.GetUserByHandle(ctx, handle)
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return errNotFound{fmt.Errorf("admin promote: no member has the handle %q", handle)}
	}
	if err != nil {
		return fmt.Errorf("admin promote: looking up %q: %w", handle, err)
	}

	// This evaluator's cache is not a running server's, and clearing it
	// cannot reach one. It does not have to: the server builds each request's
	// Viewer from the users row, and an admin passes before its cache is
	// read (perm.Evaluator.Promote, TestPromoteFromAnotherProcess).
	promoted, err := perm.New(d).Promote(ctx, u.ID, perm.Promotion{Details: map[string]string{"via": "cli"}})
	if err != nil {
		return fmt.Errorf("admin promote: %w", err)
	}
	if !promoted {
		fmt.Fprintf(out, "%s is already an admin\n", u.Handle)
		return nil
	}
	fmt.Fprintf(out, "%s is now an admin\n", u.Handle)
	return nil
}
