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
	"strconv"
	"strings"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// Key is one setting, typed. Its value is JSON of type T.
type Key[T any] struct {
	Name    string
	Default T
	Doc     string
	valid   *rule[T]

	// allowed says in words what the key takes, for an error message.
	allowed string
}

// rule is a key's validation beyond its type, with the words that say what
// it allows.
type rule[T any] struct {
	check   func(T) error
	allowed string
}

// The kinds of value a key can hold. The kind decides how [FromText] reads
// a value typed on a command line.
const (
	KindText    = "text"
	KindInteger = "integer"
	KindBoolean = "boolean"
)

// spec is a key with its type erased, for Set and the listing.
type spec struct {
	name    string
	doc     string
	kind    string
	allowed string
	decode  func([]byte) (any, error)
	encoded string // the default as JSON
}

var registry = map[string]spec{}

func define[T any](name string, def T, doc string, valid *rule[T]) Key[T] {
	if _, dup := registry[name]; dup {
		panic("settings: " + name + " defined twice")
	}
	var kind, allowed string
	switch any(def).(type) {
	case string:
		kind, allowed = KindText, "text"
	case int:
		kind, allowed = KindInteger, "a whole number"
	case bool:
		kind, allowed = KindBoolean, "true or false"
	default:
		panic(fmt.Sprintf("settings: %s has a type (%T) FromText cannot read", name, def))
	}
	if valid != nil {
		if err := valid.check(def); err != nil {
			panic(fmt.Sprintf("settings: the default of %s is invalid: %v", name, err))
		}
		allowed = valid.allowed
	}
	k := Key[T]{Name: name, Default: def, Doc: doc, valid: valid, allowed: allowed}
	b, err := json.Marshal(def)
	if err != nil {
		panic(err)
	}
	registry[name] = spec{name: name, doc: doc, kind: kind, allowed: allowed, encoded: string(b),
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

// decode reads exactly one JSON value of type T and validates it. Its
// errors end by saying what the key takes, since whoever reads one is about
// to type the value again.
func (k Key[T]) decode(b []byte) (T, error) {
	var v T
	b = bytes.TrimSpace(b)
	invalid := func(why string) error {
		return fmt.Errorf("%w: %s: %s; it takes %s", ErrInvalidValue, k.Name, why, k.allowed)
	}
	// null would decode into any T as its zero value, which is a value
	// nobody typed.
	if bytes.Equal(b, []byte("null")) {
		return v, invalid("may not be null")
	}
	if len(b) == 0 {
		return v, invalid("no value")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(&v); err != nil {
		return v, invalid(err.Error())
	}
	// Exactly one value: anything after it, a second value or a stray
	// bracket, is refused rather than dropped (dec.More would pass "5}").
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return v, invalid("input continues after the value")
	}
	if k.valid != nil {
		if err := k.valid.check(v); err != nil {
			return v, invalid(err.Error())
		}
	}
	return v, nil
}

// Set stores a setting by name, as an admin screen or the CLI sends it, and
// appends an audit row naming the actor and the old and new values, in one
// transaction. actor is 0 for the system.
func Set(ctx context.Context, d *db.DB, name string, value json.RawMessage, actor int64, now time.Time) error {
	return SetBy(ctx, d, name, value, Change{Actor: actor, At: now})
}

// Change is who changed a setting and when, for the audit row.
type Change struct {
	// Actor is the member who made the change: 0 for the command line or
	// the system, which are nobody's account.
	Actor int64

	// Details go into the audit row's details_json beside "from" and "to",
	// which they cannot replace. `ketju admin setting set` records
	// "via": "cli", as `ketju admin promote` does.
	Details map[string]string

	// IP is the address the change came from, for the audit row: empty for
	// the command line.
	IP string

	At time.Time
}

// SetBy is Set with more to say in the audit row.
func SetBy(ctx context.Context, d *db.DB, name string, value json.RawMessage, c Change) error {
	s, ok := registry[name]
	if !ok {
		return unknown(name)
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
	by := sql.NullInt64{Int64: c.Actor, Valid: c.Actor != 0}
	at := c.At.UnixMilli()
	return d.Write(ctx, func(q *dbq.Queries, tx *sql.Tx) error {
		from := s.encoded
		switch old, err := q.GetSetting(ctx, name); {
		case err == nil:
			from = old.ValueJson
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		if err := q.PutSetting(ctx, dbq.PutSettingParams{Key: name, ValueJson: string(canonical),
			UpdatedAt: at, UpdatedBy: by}); err != nil {
			return err
		}
		fields := make(map[string]any, len(c.Details)+2)
		for k, v := range c.Details {
			fields[k] = v
		}
		fields["from"], fields["to"] = json.RawMessage(from), json.RawMessage(canonical)
		_, err := audit.Append(ctx, q, c.At, audit.By{Actor: c.Actor, IP: c.IP}, ActionChanged, audit.Setting(name), fields)
		return err
	})
}

// ActionChanged is the audit_log action SetBy writes.
const ActionChanged = "setting.changed"

// unknown is ErrUnknownKey for name, listing the keys there are.
func unknown(name string) error {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	slices.Sort(names)
	return fmt.Errorf("%w: %q; the keys are %s", ErrUnknownKey, name, strings.Join(names, ", "))
}

// FromText turns a value typed on a command line into the key's JSON. A
// text key takes the words as they are, with no quotes: open, or My forum.
// An integer or boolean key takes JSON, which for those is how the value is
// written anyway: 30, true. The result is not yet validated; Set does that.
func FromText(name, value string) (json.RawMessage, error) {
	s, ok := registry[name]
	if !ok {
		return nil, unknown(name)
	}
	if s.kind == KindText {
		return json.Marshal(value)
	}
	return json.RawMessage(value), nil
}

// Setting describes one key for a listing: `ketju config check`, `ketju
// admin setting list`, the admin screen, the docs test.
type Setting struct {
	Name    string
	Doc     string
	Default string // JSON

	// Kind is KindText, KindInteger or KindBoolean.
	Kind string

	// Allowed says in words what the key takes: one of "open", "approval";
	// a whole number, 0 or more.
	Allowed string
}

func (s spec) setting() Setting {
	return Setting{Name: s.name, Doc: s.doc, Default: s.encoded, Kind: s.kind, Allowed: s.allowed}
}

// Describe is the key named, or ErrUnknownKey listing the keys there are.
func Describe(name string) (Setting, error) {
	s, ok := registry[name]
	if !ok {
		return Setting{}, unknown(name)
	}
	return s.setting(), nil
}

// All lists every key, sorted by name.
func All() []Setting {
	out := make([]Setting, 0, len(registry))
	for _, s := range registry {
		out = append(out, s.setting())
	}
	slices.SortFunc(out, func(a, b Setting) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

// Value is a setting as the database has it.
type Value struct {
	Setting

	// JSON is the value in canonical form: the stored value, or the default
	// when there is no row.
	JSON string

	// IsDefault is whether the value is the default, with or without a row
	// that says so.
	IsDefault bool
}

// Text is the value as FromText reads it: a text key's string without its
// quotes, and any other key's JSON.
func (v Value) Text() string {
	if v.Kind == KindText {
		var s string
		if err := json.Unmarshal([]byte(v.JSON), &s); err == nil {
			return s
		}
	}
	return v.JSON
}

// Lookup reads one setting by name, for a caller that has a name rather
// than a Key. A stored value that no longer decodes is an error, as it is
// for Key.Get.
func Lookup(ctx context.Context, d *db.DB, name string) (Value, error) {
	s, ok := registry[name]
	if !ok {
		return Value{}, unknown(name)
	}
	var v Value
	err := d.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		v, err = s.current(ctx, q)
		return err
	})
	return v, err
}

// Current reads every setting, sorted by name, in one transaction.
func Current(ctx context.Context, d *db.DB) ([]Value, error) {
	all := All()
	var out []Value
	err := d.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		out = make([]Value, 0, len(all))
		for _, st := range all {
			v, err := registry[st.Name].current(ctx, q)
			if err != nil {
				return err
			}
			out = append(out, v)
		}
		return nil
	})
	return out, err
}

func (s spec) current(ctx context.Context, q *dbq.Queries) (Value, error) {
	v := Value{Setting: s.setting(), JSON: s.encoded, IsDefault: true}
	row, err := q.GetSetting(ctx, s.name)
	if errors.Is(err, sql.ErrNoRows) {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	decoded, err := s.decode([]byte(row.ValueJson))
	if err != nil {
		return v, fmt.Errorf("settings: the stored %s: %w", s.name, err)
	}
	canonical, err := json.Marshal(decoded)
	if err != nil {
		return v, err
	}
	v.JSON = string(canonical)
	v.IsDefault = v.JSON == s.encoded
	return v, nil
}

func text(minLen, maxLen int) *rule[string] {
	return &rule[string]{allowed: fmt.Sprintf("text of %d to %d characters", minLen, maxLen),
		check: func(s string) error {
			if n := len([]rune(s)); n < minLen || n > maxLen {
				return fmt.Errorf("%d characters", n)
			}
			return nil
		}}
}

func oneOf(allowed ...string) *rule[string] {
	quoted := make([]string, len(allowed))
	for i, a := range allowed {
		quoted[i] = strconv.Quote(a)
	}
	return &rule[string]{allowed: "one of " + strings.Join(quoted, ", "),
		check: func(s string) error {
			if !slices.Contains(allowed, s) {
				return fmt.Errorf("%q is not one of them", s)
			}
			return nil
		}}
}

func atLeast(least int) *rule[int] {
	return &rule[int]{allowed: fmt.Sprintf("a whole number, %d or more", least),
		check: func(n int) error {
			if n < least {
				return fmt.Errorf("%d is below %d", n, least)
			}
			return nil
		}}
}
