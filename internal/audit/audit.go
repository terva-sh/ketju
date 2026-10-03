// Package audit is the one way rows reach audit_log (docs/04 "audit_log").
//
// Every moderation and administration action writes one row, inside the
// transaction that makes the change, so the change and its record commit
// together or not at all. The caller holds the transaction: a service's
// db.Write, or the projection writer's range transaction for an action the
// thread log records. Append never opens one of its own.
//
// Going through here fixes three things a direct dbq.AppendAudit call could
// get wrong: an actor with no account is written as SQL NULL and never as id
// 0, which names nobody; the work of no account says what did it, in
// details_json as "via"; and each target_type has one target_id format, so a
// reader can find every row about a thing. TestOneWayIntoAuditLog keeps
// AppendAudit out of every other package.
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// By is who did an action, for the actor_id and ip columns.
type By struct {
	// Actor is the member who acted. 0 means no account did: the command
	// line, the configuration, a background job. It is written as NULL.
	Actor int64

	// IP is the address the request came from, as web.ClientIP reads it.
	// Empty when there is no request: the command line, a job, or the
	// projection writer, since the thread log carries no address.
	IP string

	// Via says what did the action when no account did, such as "cli",
	// "expiry" or "auto-hide". It is written into details_json as "via".
	Via string
}

// System is the actor for work nobody's account did. via names what did it,
// and is recorded in the row's details.
func System(via string) By { return By{Via: via} }

// Target is what an action was done to: the row's target_type and
// target_id. Build one with the constructors below, which fix each type's
// id format.
type Target struct {
	Type string
	ID   string
}

// The target types. docs/04 "audit_log" lists them with their id formats,
// and TestTargetTypesDocumented holds the two together.
const (
	TypeUser        = "user"
	TypeForum       = "forum"
	TypeGroup       = "group"
	TypeFlag        = "flag"
	TypeApplication = "application"
	TypeThread      = "thread"
	TypePost        = "post"
	TypeSetting     = "setting"
	TypePermission  = "permission"
	TypeForm        = "form"
	TypeIdentity    = "identity"
	TypeBlob        = "blob"
)

// Types lists every target type, for the documentation test.
var Types = []string{TypeUser, TypeForum, TypeGroup, TypeFlag, TypeApplication, TypeThread, TypePost, TypeSetting,
	TypePermission, TypeForm, TypeIdentity, TypeBlob}

// Actions lists every action a row is written with today, sorted: the
// closed list the admin audit view offers as its action filter
// (TKT-01M29CP2R4). docs/04 "audit_log" lists the same actions, and
// TestActionsDocumented holds the two together; TestActionsCoverTheWriters
// fails on an action constant or an Append literal that is missing here.
//
// Append does not refuse an action that is not listed. A missed action then
// costs the filter one entry rather than failing a moderator's action.
var Actions = []string{
	"agent.credential_issued",
	"agent.credential_revoked",
	"agent.limits_changed",
	"application.approved",
	"application.denied",
	"blob.deleted",
	"flag.dismissed",
	"flag.resolved",
	"form.version_created",
	"forum.archived",
	"forum.created",
	"forum.moved",
	"forum.unarchived",
	"forum.updated",
	"forum.visibility_changed",
	"group.created",
	"group.deleted",
	"group.member_added",
	"group.member_removed",
	"group.updated",
	"identity.linked",
	"identity.unblocked",
	"identity.unlinked",
	"permission.granted",
	"permission.revoked",
	"post.auto_hidden",
	"post.deleted",
	"post.edited",
	"post.hidden",
	"post.redacted",
	"post.restored",
	"posts.moved",
	"setting.changed",
	"thread.archived",
	"thread.locked",
	"thread.moved",
	"thread.pinned",
	"thread.renamed",
	"thread.retagged",
	"thread.slow_mode",
	"thread.title_redacted",
	"thread.unlocked",
	"thread.unpinned",
	"user.anonymized",
	"user.avatar_cleared",
	"user.created",
	"user.deactivated",
	"user.demoted",
	"user.posts_hidden",
	"user.posts_restored",
	"user.promoted",
	"user.reactivated",
	"user.renamed",
	"user.role_changed",
	"user.suspended",
	"user.unsuspended",
}

func decimal(typ string, id int64) Target { return Target{Type: typ, ID: strconv.FormatInt(id, 10)} }

// User is a user, by id.
func User(id int64) Target { return decimal(TypeUser, id) }

// Forum is a forum, by id.
func Forum(id int64) Target { return decimal(TypeForum, id) }

// Group is a group, by id.
func Group(id int64) Target { return decimal(TypeGroup, id) }

// Flag is one flag (report), by id.
func Flag(id int64) Target { return decimal(TypeFlag, id) }

// Application is a registration application, by id.
func Application(id int64) Target { return decimal(TypeApplication, id) }

// Identity is one identity a provider vouches for, by id: an applicant's,
// whose block staff lift (TKT-01M29CP2MG).
func Identity(id int64) Target { return decimal(TypeIdentity, id) }

// Thread is a thread, by its base32 id as URLs spell it.
func Thread(id threadid.ID) Target { return Target{Type: TypeThread, ID: id.String()} }

// Post is one post, as <base32 thread id>/<ordinal>.
func Post(thread threadid.ID, ordinal uint32) Target {
	return Target{Type: TypePost, ID: thread.String() + "/" + strconv.FormatUint(uint64(ordinal), 10)}
}

// Blob is an attachment blob, by its SHA-256 in lower-case hex: the one a
// hard redaction deleted (TKT-01M29CXJZ9).
func Blob(hash string) Target { return Target{Type: TypeBlob, ID: hash} }

// Setting is a runtime setting, by its key.
func Setting(key string) Target { return Target{Type: TypeSetting, ID: key} }

// Permission is one permission row: <forum id>:<group id>:<permission>, or
// global:<group id>:<permission> for a site-wide grant, whose forum_id is
// NULL. forumID 0 means global, as it does in internal/perm.
func Permission(forumID, groupID int64, permission string) Target {
	forum := "global"
	if forumID != 0 {
		forum = strconv.FormatInt(forumID, 10)
	}
	return Target{Type: TypePermission, ID: forum + ":" + strconv.FormatInt(groupID, 10) + ":" + permission}
}

// Form is a declarative form, by its key, such as registration. The key
// names the form across its versions; the version an action wrote goes in
// the row's details.
func Form(key string) Target { return Target{Type: TypeForm, ID: key} }

// ParsePost reads a post target's id back into its thread and ordinal.
func ParsePost(id string) (threadid.ID, uint32, error) {
	t, o, ok := strings.Cut(id, "/")
	if !ok {
		return 0, 0, fmt.Errorf("audit: post target %q has no ordinal", id)
	}
	thread, err := threadid.Parse(t)
	if err != nil {
		return 0, 0, fmt.Errorf("audit: post target %q: %w", id, err)
	}
	n, err := strconv.ParseUint(o, 10, 32)
	if err != nil || n < 1 {
		return 0, 0, fmt.Errorf("audit: post target %q: the ordinal is not a positive integer", id)
	}
	return thread, uint32(n), nil
}

// Append writes one audit row through q, inside the caller's transaction,
// and returns its id.
//
// at is the caller's, not the clock's: a service passes the time it read for
// the change, and the projection writer passes the frame's timestamp, so the
// row says when the action happened however late it was projected.
//
// details become details_json; nil is {}. A non-empty by.Via is written as
// "via" and takes that key over from details, since what did the action is
// the actor's to say.
func Append(ctx context.Context, q *dbq.Queries, at time.Time, by By, action string, target Target, details map[string]any) (int64, error) {
	if action == "" {
		return 0, errors.New("audit: an action is required")
	}
	if by.Actor < 0 {
		return 0, fmt.Errorf("audit: actor id %d names nobody", by.Actor)
	}
	fields := make(map[string]any, len(details)+1)
	for k, v := range details {
		fields[k] = v
	}
	if by.Via != "" {
		fields["via"] = by.Via
	}
	dj, err := json.Marshal(fields)
	if err != nil {
		return 0, fmt.Errorf("audit: the details of %s: %w", action, err)
	}
	return q.AppendAudit(ctx, dbq.AppendAuditParams{
		At:          at.UnixMilli(),
		ActorID:     sql.NullInt64{Int64: by.Actor, Valid: by.Actor != 0},
		Action:      action,
		TargetType:  target.Type,
		TargetID:    target.ID,
		DetailsJson: string(dj),
		Ip:          by.IP,
	})
}
