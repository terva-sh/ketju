package problem

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/terva-sh/ketju/internal/events"
	"github.com/terva-sh/ketju/internal/forms"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// mapping is the one place a domain error becomes an API code.
//
// This is AC2 of TKT-01M29C8018, and the reason it is a table rather than a
// switch in each handler: when threadlog grows an error, there is exactly one
// file that has to learn about it, and the compiler points at it. A handler
// that maps its own errors is a handler whose 404 drifts from everybody
// else's.
//
// It is an ordered slice rather than a map because errors wrap: ErrCriticalRecordType
// wraps ErrUnknownRecordType, so a match has to be able to prefer the specific
// one. First match wins, so the specific entries come first.
//
// The direction of the dependency is worth noting: this package imports the
// domain packages, and none of them import this one. Storage does not know
// what an HTTP status is, and should not.
var mapping = []struct {
	err  error
	code Code
}{
	// Client asked for something that is not there or is not theirs to have.
	{threadlog.ErrNoSuchOrdinal, CodeNotFound},
	{threadid.ErrMalformed, CodeBadRequest},

	// Client asked for a reply to a post that is not in the thread. The
	// request parsed; what it names does not exist there.
	{render.ErrInvalidReplyTo, CodeValidationFailed},

	// A form's answers or a form schema that breaks the rules. The body
	// lists each field and its code (FieldErrorsOf). An answer naming no
	// field is a client bug: the client drew its inputs from the schema.
	{forms.ErrInvalidAnswers, CodeValidationFailed},
	{forms.ErrInvalidSchema, CodeValidationFailed},
	{forms.ErrUnknownAnswer, CodeBadRequest},

	// Client asked for something the object's state refuses.
	{threadlog.ErrActorClosed, CodeConflict},
	// An edit of a deleted post, a delete of one already gone, a restore of
	// one that is not deleted: the actor refused the record (TKT-01M29CA9BP).
	{threadlog.ErrPostState, CodeConflict},
	// Any record after a ThreadArchived: archived is read-only to everyone,
	// and a write that raced the archive answers as one that came after it
	// (TKT-01M29CRJ64).
	{threadlog.ErrArchived, CodeConflict},

	// The admin users, groups and permissions screens (TKT-01M29CXK1X).
	// A user or group named by id that is not there; a change that is not
	// one; and a change the rows as they stand refuse: the last active
	// admin, a membership the identity provider owns, and a batch staged
	// against a grid that has moved.
	{perm.ErrNoUser, CodeNotFound},
	{perm.ErrNoGroup, CodeNotFound},
	{perm.ErrInvalidRole, CodeValidationFailed},
	{perm.ErrInvalidChange, CodeValidationFailed},
	{perm.ErrImplicitGroup, CodeValidationFailed},
	{perm.ErrLastAdmin, CodeConflict},
	{perm.ErrIdPMembership, CodeConflict},
	{perm.ErrGrantState, CodeConflict},
	// The account lifecycle (TKT-01M29CKRFS): a status change the account's
	// status does not allow, and any change to an anonymized account.
	{perm.ErrBadTransition, CodeConflict},
	{perm.ErrAnonymized, CodeConflict},
	// The admin forum screen (TKT-01M29CXK19): a forum named by id that is
	// not there, and a visibility that is not one of the three.
	{perm.ErrNoForum, CodeNotFound},
	{perm.ErrInvalidVisibility, CodeValidationFailed},

	// Client sent too much. render.ErrTooLarge is the post source over
	// render.MaxSource, which docs/07 puts at 64 KB.
	{render.ErrTooLarge, CodePayloadTooLarge},

	// The server is draining or a dependency is down. Retryable, which is what
	// separates these from CodeInternal.
	{threadcache.ErrClosed, CodeUnavailable},
	{events.ErrClosed, CodeUnavailable},
	{events.ErrDrainTimeout, CodeUnavailable},
	{context.DeadlineExceeded, CodeUnavailable},
	// The database lock outlasted busy_timeout: another process holds it,
	// or a long read blocked a checkpoint. Retryable.
	{db.ErrBusy, CodeUnavailable},
	// A newer binary migrated this database. The server should refuse to
	// start, so this reaching a handler means it did not; say so honestly
	// rather than serve queries against a schema they were not written for.
	{db.ErrSchemaNewer, CodeUnavailable},

	// Corruption and programmer error. These are all CodeInternal on purpose.
	// A checksum mismatch is a server fault, not something the reader did, and
	// its text ("threadlog: checksum mismatch", "not a thread log") describes
	// the on-disk format to whoever provoked it. They are listed rather than
	// left to fall through so that the intent is recorded: they were
	// considered and deliberately given the bug bucket.
	{threadlog.ErrChecksum, CodeInternal},
	{threadlog.ErrBadMagic, CodeInternal},
	{threadlog.ErrMalformed, CodeInternal},
	{threadlog.ErrUnsupportedVersion, CodeInternal},
	{threadlog.ErrUnsupportedEncoding, CodeInternal},
	{threadlog.ErrCriticalRecordType, CodeInternal},
	{threadlog.ErrUnknownRecordType, CodeInternal},
	{threadlog.ErrLogTooShort, CodeInternal},
	{threadlog.ErrIndexStale, CodeInternal},
	{threadlog.ErrShortBuffer, CodeInternal},
	{threadid.ErrTimestampRange, CodeInternal},
	{events.ErrStarted, CodeInternal},
	{events.ErrDuplicateName, CodeInternal},
	{db.ErrMigrations, CodeInternal},
}

// coded is an error that names its own API code.
//
// Handlers raise most client errors directly rather than through a sentinel --
// "this title is empty" has no reusable error value and does not want one. Such
// an error wraps a code with Coded and the mapping finds it without an entry in
// the table above.
type coded struct {
	code Code
	err  error
}

func (c coded) Error() string { return c.err.Error() }
func (c coded) Unwrap() error { return c.err }

// Coded tags an error with the API code it should produce.
//
// The error's own text still never reaches the client -- only the code travels,
// and the title comes from the catalogue. This exists so a handler can say what
// kind of failure it hit without inventing a package-level sentinel for every
// validation rule.
func Coded(c Code, err error) error {
	if err == nil {
		return nil
	}
	return coded{code: c, err: err}
}

// retryAfter is a coded error that also says when trying again can succeed.
type retryAfter struct {
	coded
	after time.Duration
}

func (r retryAfter) Unwrap() error { return r.coded }

// RetryAfter tags an error with its code and with how long the client should
// wait before trying again, which WriteError sends as Retry-After. Slow mode
// uses it: the wait is the rule's, not a guess.
func RetryAfter(c Code, after time.Duration, err error) error {
	if err == nil {
		return nil
	}
	return retryAfter{coded: coded{code: c, err: err}, after: after}
}

// RetryAfterOf returns the wait an error carries, and whether it carries one.
func RetryAfterOf(err error) (time.Duration, bool) {
	var r retryAfter
	if errors.As(err, &r) {
		return r.after, true
	}
	return 0, false
}

// availableAt is a coded error that also says when the refused thing
// becomes possible.
type availableAt struct {
	coded
	at int64
}

func (a availableAt) Unwrap() error { return a.coded }

// AvailableAt tags an error with its code and with the time, in Unix ms,
// when what it refused becomes possible, which WriteError sends as the
// body's available_at. A member's handle change inside the 30-day cooldown
// uses it (TKT-01M3NRP3W9): the wait is weeks, which Retry-After would tell
// a client to spend retrying.
func AvailableAt(c Code, at int64, err error) error {
	if err == nil {
		return nil
	}
	return availableAt{coded: coded{code: c, err: err}, at: at}
}

// AvailableAtOf returns the time an error carries, and whether it carries
// one.
func AvailableAtOf(err error) (int64, bool) {
	var a availableAt
	if errors.As(err, &a) {
		return a.at, true
	}
	return 0, false
}

// FieldErrorsOf returns the field errors err carries, or nil. A form's
// refusal (*forms.Invalid) carries them; WriteError puts them in a
// validation-failed body and nowhere else.
func FieldErrorsOf(err error) []FieldError {
	var inv *forms.Invalid
	if !errors.As(err, &inv) || len(inv.Fields) == 0 {
		return nil
	}
	out := make([]FieldError, len(inv.Fields))
	for i, f := range inv.Fields {
		out[i] = FieldError{Field: f.Field, Code: f.Code}
	}
	return out
}

// FromError returns the code for an error.
//
// A nil error is CodeInternal rather than a success: reaching an error writer
// with nothing wrong is itself a bug, and answering 200 here would hide it.
//
// An unmapped error is CodeInternal too. That is the safe default in both
// directions -- the client learns nothing about an error nobody has classified,
// and WriteError logs it at error level precisely because an unmapped error
// means the table above has a gap.
func FromError(err error) Code {
	if err == nil {
		return CodeInternal
	}

	// An explicit tag wins over the table: a handler that said what it meant
	// is more specific than a sentinel it happens to wrap.
	// A body that blew past MaxBytesReader. It is a type rather than a
	// sentinel, so it cannot live in the table above. Checked before the
	// table because the read error it surfaces through is often wrapped in
	// something more generic by the decoder that hit it.
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return CodePayloadTooLarge
	}

	var c coded
	if errors.As(err, &c) {
		if _, ok := catalogue[c.code]; ok {
			return c.code
		}
		return CodeInternal
	}

	for _, m := range mapping {
		if errors.Is(err, m.err) {
			return m.code
		}
	}

	// A cancelled request usually means the client hung up, so whatever we
	// write goes nowhere. It is mapped last, after DeadlineExceeded, so a
	// timeout is not reported as a cancellation.
	if errors.Is(err, context.Canceled) {
		return CodeUnavailable
	}
	return CodeInternal
}
