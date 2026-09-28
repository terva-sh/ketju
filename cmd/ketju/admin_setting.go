package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/terva-sh/ketju/internal/lock"
	"github.com/terva-sh/ketju/internal/settings"
)

const settingUsage = `usage: ketju admin setting set [--root DIR] KEY VALUE
       ketju admin setting get [--root DIR] KEY
       ketju admin setting list [--root DIR]

Reads and changes the runtime settings, which live in the database rather
than in config.toml (docs/02 "Configuration", docs/04 "settings"). It is how
an operator changes one before there is an admin screen, and how a fresh
forum opens registration, which is closed (approval) until someone does:

  ketju admin setting set --root /var/lib/ketju registration.mode open

VALUE is read by the key's kind, which list shows:
  text     the value as typed, with no JSON quotes: open, or 'My forum'
           quoted for the shell
  integer  a whole number: 30
  boolean  true or false
Every value is checked against the key's rules before anything is written,
and an unknown key or a value the key does not take is refused, naming the
keys there are or what the key takes.

set is for a stopped instance: it refuses while ketju serve holds the data
directory. Stop serve, set the value, and start serve again. Each change
writes a setting.changed audit row in the same transaction, with no actor
and "via": "cli", as admin promote records a promotion.

get prints the value as set takes it. list prints every key as
"key = value", the value as JSON, then whether it is the default (or what
the default is), the key's kind and rules, and what it does. get and list
only read, and may run while serve does.

exit codes:
  0  done
  1  no setting has that key, or the key does not take that value
  2  usage
  4  the data directory is in use (set only), has no database, or the
     database could not be read or written

`

// errInvalidValue is a value the setting does not take. It exits 1, as an
// unknown key does.
type errInvalidValue struct{ err error }

func (e errInvalidValue) Error() string { return e.err.Error() }
func (e errInvalidValue) Unwrap() error { return e.err }

func adminSetting(ctx context.Context, out, errOut io.Writer, args []string) error {
	if len(args) == 0 {
		fmt.Fprint(errOut, settingUsage)
		return errUsage{errors.New("admin setting needs set, get or list")}
	}
	var positional int
	switch args[0] {
	case "set":
		positional = 2
	case "get":
		positional = 1
	case "list":
		positional = 0
	case "-h", "--help", "help":
		fmt.Fprint(out, settingUsage)
		return nil
	default:
		fmt.Fprint(errOut, settingUsage)
		return errUsage{fmt.Errorf("unknown admin setting subcommand %q", args[0])}
	}
	sub := args[0]
	cmd := "admin setting " + sub
	fs_ := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs_.SetOutput(errOut)
	root := fs_.String("root", ".", "data directory")
	fs_.Usage = func() {
		fmt.Fprint(fs_.Output(), settingUsage)
		fs_.PrintDefaults()
	}
	if err := fs_.Parse(args[1:]); err != nil {
		return errUsage{err}
	}
	if fs_.NArg() != positional {
		fs_.Usage()
		want := map[int]string{0: "no arguments", 1: "exactly one key", 2: "a key and a value"}[positional]
		return errUsage{fmt.Errorf("%s takes %s", cmd, want)}
	}

	switch sub {
	case "set":
		return settingSet(ctx, out, cmd, *root, fs_.Arg(0), fs_.Arg(1))
	case "get":
		return settingGet(ctx, out, cmd, *root, fs_.Arg(0))
	default:
		return settingList(ctx, out, cmd, *root)
	}
}

// refusal gives a settings error its exit code: 1 for a key or value the
// registry refuses, 4 for anything else.
func refusal(cmd string, err error) error {
	err = fmt.Errorf("%s: %w", cmd, err)
	switch {
	case errors.Is(err, settings.ErrUnknownKey):
		return errNotFound{err}
	case errors.Is(err, settings.ErrInvalidValue):
		return errInvalidValue{err}
	}
	return err
}

func settingSet(ctx context.Context, out io.Writer, cmd, root, key, text string) error {
	raw, err := settings.FromText(key, text)
	if err != nil {
		return refusal(cmd, err)
	}
	if err := needDatabase(cmd, root); err != nil {
		return err
	}
	// Taken before the database is opened, as serve and reindex take it,
	// so serve cannot start between the check and the write.
	lk, err := lock.Acquire(root, "admin setting")
	if err != nil {
		var held *lock.HeldError
		if errors.As(err, &held) {
			return fmt.Errorf("%s: %w admin setting set is for a stopped instance: "+
				"stop ketju serve, set the value, then start serve again", cmd, err)
		}
		return fmt.Errorf("%s: %w", cmd, err)
	}
	defer lk.Release()

	d, err := openDatabase(ctx, cmd, root, true)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := settings.SetBy(ctx, d, key, raw, settings.Change{
		Details: map[string]string{"via": "cli"}, At: time.Now()}); err != nil {
		return refusal(cmd, err)
	}
	v, err := settings.Lookup(ctx, d, key)
	if err != nil {
		return refusal(cmd, err)
	}
	fmt.Fprintf(out, "%s = %s\n", v.Name, v.JSON)
	return nil
}

func settingGet(ctx context.Context, out io.Writer, cmd, root, key string) error {
	// An unknown key is refused before the database is looked for, so a
	// typo is named as one whatever --root says.
	if _, err := settings.Describe(key); err != nil {
		return refusal(cmd, err)
	}
	if err := needDatabase(cmd, root); err != nil {
		return err
	}
	d, err := openDatabase(ctx, cmd, root, false)
	if err != nil {
		return err
	}
	defer d.Close()
	v, err := settings.Lookup(ctx, d, key)
	if err != nil {
		return refusal(cmd, err)
	}
	fmt.Fprintln(out, v.Text())
	return nil
}

func settingList(ctx context.Context, out io.Writer, cmd, root string) error {
	if err := needDatabase(cmd, root); err != nil {
		return err
	}
	d, err := openDatabase(ctx, cmd, root, false)
	if err != nil {
		return err
	}
	defer d.Close()
	vs, err := settings.Current(ctx, d)
	if err != nil {
		return refusal(cmd, err)
	}
	var b strings.Builder
	for i, v := range vs {
		if i > 0 {
			b.WriteByte('\n')
		}
		state := "default"
		if !v.IsDefault {
			state = "set; the default is " + v.Default
		}
		fmt.Fprintf(&b, "%s = %s  # %s\n", v.Name, v.JSON, state)
		fmt.Fprintf(&b, "  %s: %s\n", v.Kind, v.Allowed)
		fmt.Fprintf(&b, "  %s\n", v.Doc)
	}
	_, err = io.WriteString(out, b.String())
	return err
}
