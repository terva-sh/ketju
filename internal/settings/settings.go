// Package settings is the runtime half of the configuration split (docs/02
// "Configuration"): what an admin changes without a restart, stored as JSON
// in the settings table.
//
// The key list is closed and lives here, one typed [Key] per setting. Set
// refuses a key that is not in it and a value of the wrong type, before
// anything reaches the table, and writes an audit row in the same
// transaction. A key with no row reads as its default, so a fresh database
// needs no seeding and a new key needs no migration.
//
// docs/04 "settings" carries the same list for readers; TestKeysDocumented
// holds the two together.
package settings

import (
	"bytes"
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// Key is one setting, typed. Its value is JSON of type T.
type Key[T any] struct {
	Name    string
	Default T
	Doc     string
	valid   func(T) error
}

// spec is a key with its type erased, for Set and the listing.
type spec struct {
	name    string
	doc     string
	decode  func([]byte) (any, error)
	encoded string // the default as JSON
}

var registry = map[string]spec{}

func define[T any](name string, def T, doc string, valid func(T) error) Key[T] {
	if _, dup := registry[name]; dup {
		panic("settings: " + name + " defined twice")
	}
	if valid != nil {
		if err := valid(def); err != nil {
			panic(fmt.Sprintf("settings: the default of %s is invalid: %v", name, err))
		}
	}
	k := Key[T]{Name: name, Default: def, Doc: doc, valid: valid}
	b, err := json.Marshal(def)
	if err != nil {
		panic(err)
	}
	registry[name] = spec{name: name, doc: doc, encoded: string(b),
		decode: func(b []byte) (any, error) { return k.decode(b) }}
	return k
}

// The settings. Defaults come from the documents named beside each one; where
// a document gives none, the choice is recorded in TKT-01M29BYYJ0.
var (
	// docs/04. The name shown in the header and in page titles.
	SiteName = define("site_name", "Ketju",
		"The site's name, shown in the header and page titles.", text(1, 100))

	// docs/05 "Registration path". Invite is a later phase and is refused
	// until it exists, rather than accepted and ignored.
	RegistrationMode = define("registration.mode", "approval",
		`How a new visitor becomes a member: "open" or "approval".`, oneOf("open", "approval"))

	// docs/05 "Local passwords": an optional feature, off by default.
	AllowLocalPasswords = define("registration.allow_local_passwords", false,
		"Whether accounts may sign in with a local password as well as a provider.", nil)

	// docs/08 "Primitives". Off by default (TKT-01M29P9RS0); per-thread slow
	// mode is Phase 2 (TKT-01M29CRJ50).
	SlowModeDefaultSeconds = define("slow_mode.default_seconds", 0,
		"Minimum seconds between one member's posts in a thread; 0 is off.", atLeast(0))

	// docs/08: "default 24 h for edits, 15 min for delete".
	EditWindowSeconds = define("posts.edit_window_seconds", 24*60*60,
		"How long after posting an author may edit a post.", atLeast(0))
	DeleteWindowSeconds = define("posts.delete_window_seconds", 15*60,
		"How long after posting an author may delete a post.", atLeast(0))

	// docs/08: "the first N minutes (default 5) of edits are collapsed".
	GraceEditSeconds = define("posts.grace_edit_seconds", 5*60,
		"Edits within this many seconds of posting are shown as the original revision.", atLeast(0))

	// docs/08 "Post history": "default on".
	PublicEditHistory = define("public_edit_history", true,
		"Whether members, not only moderators, may read a post's revisions.", nil)

	// docs/08 "Flags (reports)": "default 3".
	FlagAutoHideThreshold = define("flags.auto_hide_threshold", 3,
		"Open flags on a post that hide it pending review.", atLeast(1))
)

// ErrUnknownKey is a key the registry does not have.
var ErrUnknownKey = errors.New("settings: unknown key")

// ErrInvalidValue is a value that is not the key's type or is out of range.
var ErrInvalidValue = errors.New("settings: invalid value")

// Get reads the setting, or its default when no row exists. A stored value
// that no longer decodes is an error rather than the default: it means the
// table was written around Set, and quietly substituting the default would
// hide that.
func (k Key[T]) Get(ctx context.Context, d *db.DB) (T, error) {
	var v T
	err := d.Read(ctx, func(q *dbq.Queries, tx *sql.Tx) error {
		var err error
		v, err = k.In(ctx, q)
		return err
	})
	return v, err
}

// In is Get inside a transaction the caller holds, for a write that must
// see the setting as it is when the write commits: registration reads
// registration.mode in the transaction that creates the account, so an
// admin closing it cannot be missed in between (TKT-01M29C8005).
func (k Key[T]) In(ctx context.Context, q *dbq.Queries) (T, error) {
	row, err := q.GetSetting(ctx, k.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return k.Default, nil
	}
	if err != nil {
		return k.Default, err
	}
	v, err := k.decode([]byte(row.ValueJson))
	if err != nil {
		return k.Default, fmt.Errorf("settings: the stored %s: %w", k.Name, err)
	}
	return v, nil
}

// decode reads exactly one JSON value of type T and validates it.
func (k Key[T]) decode(b []byte) (T, error) {
	var v T
	b = bytes.TrimSpace(b)
	// null would decode into any T as its zero value, which is a value
	// nobody typed.
	if bytes.Equal(b, []byte("null")) {
		return v, fmt.Errorf("%w: %s may not be null", ErrInvalidValue, k.Name)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(&v); err != nil {
		return v, fmt.Errorf("%w: %s: %v", ErrInvalidValue, k.Name, err)
	}
	// Exactly one value: anything after it, a second value or a stray
	// bracket, is refused rather than dropped (dec.More would pass "5}").
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return v, fmt.Errorf("%w: %s: input continues after the value", ErrInvalidValue, k.Name)
	}
	if k.valid != nil {
		if err := k.valid(v); err != nil {
			return v, fmt.Errorf("%w: %s: %v", ErrInvalidValue, k.Name, err)
		}
	}
	return v, nil
}

// Set stores a setting by name, as an admin screen or the CLI sends it, and
// appends an audit row naming the actor and the old and new values, in one
// transaction. actor is 0 for the system.
func Set(ctx context.Context, d *db.DB, name string, value json.RawMessage, actor int64, now time.Time) error {
	s, ok := registry[name]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownKey, name)
	}
	v, err := s.decode(value)
	if err != nil {
		return err
	}
	// Stored in canonical form, so the table and the audit row say the
	// same thing however the caller spaced it.
	canonical, err := json.Marshal(v)
	if err != nil {
		return err
	}
	by := sql.NullInt64{Int64: actor, Valid: actor != 0}
	return d.Write(ctx, func(q *dbq.Queries, tx *sql.Tx) error {
		from := s.encoded
		switch old, err := q.GetSetting(ctx, name); {
		case err == nil:
			from = old.ValueJson
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		if err := q.PutSetting(ctx, dbq.PutSettingParams{Key: name, ValueJson: string(canonical),
			UpdatedAt: now.UnixMilli(), UpdatedBy: by}); err != nil {
			return err
		}
		details, err := json.Marshal(struct {
			From json.RawMessage `json:"from"`
			To   json.RawMessage `json:"to"`
		}{json.RawMessage(from), canonical})
		if err != nil {
			return err
		}
		_, err = q.AppendAudit(ctx, dbq.AppendAuditParams{At: now.UnixMilli(), ActorID: by,
			Action: "setting.changed", TargetType: "setting", TargetID: name, DetailsJson: string(details)})
		return err
	})
}

// Setting describes one key for a listing: `ketju config check`, the admin
// screen, the docs test.
type Setting struct {
	Name    string
	Doc     string
	Default string // JSON
}

// All lists every key, sorted by name.
func All() []Setting {
	out := make([]Setting, 0, len(registry))
	for _, s := range registry {
		out = append(out, Setting{Name: s.name, Doc: s.doc, Default: s.encoded})
	}
	slices.SortFunc(out, func(a, b Setting) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

func text(minLen, maxLen int) func(string) error {
	return func(s string) error {
		if n := len([]rune(s)); n < minLen || n > maxLen {
			return fmt.Errorf("%d characters, want %d to %d", n, minLen, maxLen)
		}
		return nil
	}
}

func oneOf(allowed ...string) func(string) error {
	return func(s string) error {
		if !slices.Contains(allowed, s) {
			return fmt.Errorf("%q is not one of %q", s, allowed)
		}
		return nil
	}
}

func atLeast(least int) func(int) error {
	return func(n int) error {
		if n < least {
			return fmt.Errorf("%d is below %d", n, least)
		}
		return nil
	}
}
