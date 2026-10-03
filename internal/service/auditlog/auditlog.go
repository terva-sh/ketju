// Package auditlog reads audit_log for the admin audit view
// (TKT-01M29CP2R4, docs/07 "The audit log"). internal/audit writes the rows;
// this package only reads them, and adds no way to change one.
//
// The log is an admin's alone: it says who did what, and from which
// address, and moderators see none of it (docs/08 "Decided 2026-09-29").
// The API's admin gate refuses everyone else before a handler runs, and
// Query asks again, so a caller that forgot the gate is still refused.
package auditlog

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/member"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/db/views"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// PageSize is how many rows one page holds.
const PageSize = 50

// Service answers the audit view.
type Service struct {
	DB *db.DB
}

// Query is the view's filters, as the query string gives them. An empty
// field does not filter.
type Query struct {
	// Actor is a member's handle. An unknown handle is an empty page.
	Actor string
	// System, "1", selects the rows no account did. It is a parameter of
	// its own rather than a reserved value of Actor, because any word a
	// handle can spell might be a member's handle.
	System string
	// Action is one action, or a prefix ending in '.', such as "user.".
	Action string
	// TargetType is one of audit.Types, and TargetID, which needs it, one
	// thing of that type, in the type's id format (docs/04 "audit_log").
	TargetType, TargetID string
	// From and To are Unix ms: From the first included, To the first not.
	From, To string
	// Cursor is the previous page's next_cursor.
	Cursor string
}

// Query is GET /admin/audit: one page of the log, newest first, narrowed by
// in. Admin only. A malformed parameter is bad-request.
func (s *Service) Query(ctx context.Context, v domain.Viewer, in Query) (dto.AuditPage, error) {
	out := dto.AuditPage{Entries: []dto.AuditEntry{}}
	if err := member.RequireAdmin(v, "read the audit log"); err != nil {
		return out, err
	}
	out.Actions = slices.Clone(audit.Actions)
	f, err := filter(in)
	if err != nil {
		return out, err
	}
	err = s.DB.Read(ctx, func(q *dbq.Queries, tx *sql.Tx) error {
		if in.Actor != "" {
			u, err := q.GetUserByHandle(ctx, in.Actor)
			if errors.Is(err, sql.ErrNoRows) {
				return errNobody
			}
			if err != nil {
				return err
			}
			f.Actor = u.ID
		}
		rows, err := views.AuditPage(ctx, tx, f)
		if err != nil {
			return err
		}
		if len(rows) > PageSize {
			rows = rows[:PageSize]
			out.NextCursor = encodeCursor(rows[len(rows)-1].ID)
		}
		out.Entries, err = resolve(ctx, q, rows)
		return err
	})
	if errors.Is(err, errNobody) {
		// The caller is an admin and learns nothing they could not read
		// from the users screen: nobody has the handle, so nobody did
		// anything under it.
		return out, nil
	}
	return out, err
}

// errNobody ends the read early for an actor handle nobody has.
var errNobody = errors.New("auditlog: no account has the handle")

func badRequest(format string, args ...any) error {
	return problem.Coded(problem.CodeBadRequest, fmt.Errorf("auditlog: "+format, args...))
}

// filter reads in into a view filter, all but the actor, which needs the
// database.
func filter(in Query) (views.AuditFilter, error) {
	f := views.AuditFilter{Limit: PageSize + 1}
	switch in.System {
	case "":
	case "1":
		if in.Actor != "" {
			return f, badRequest("an actor and system together select nothing")
		}
		f.System = true
	default:
		return f, badRequest("system is %q; it is 1 or absent", in.System)
	}
	if in.Action != "" {
		f.Action = in.Action
		f.ActionPrefix = strings.HasSuffix(in.Action, ".")
	}
	switch {
	case in.TargetType != "":
		if !slices.Contains(audit.Types, in.TargetType) {
			return f, badRequest("%q is not a target type", in.TargetType)
		}
		f.TargetType, f.TargetID = in.TargetType, in.TargetID
	case in.TargetID != "":
		return f, badRequest("a target_id needs a target_type")
	}
	for _, b := range []struct {
		name, v string
		to      **int64
	}{{"from", in.From, &f.From}, {"to", in.To, &f.To}} {
		if b.v == "" {
			continue
		}
		n, err := strconv.ParseInt(b.v, 10, 64)
		if err != nil || n < 0 {
			return f, badRequest("%s is %q; it is Unix ms", b.name, b.v)
		}
		*b.to = &n
	}
	if in.Cursor != "" {
		n, err := decodeCursor(in.Cursor)
		if err != nil {
			return f, err
		}
		f.Before = n
	}
	return f, nil
}

// encodeCursor is the opaque cursor after the row id: its decimal id,
// base64url-encoded.
func encodeCursor(id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(id, 10)))
}

// decodeCursor reads encodeCursor's form back, and refuses anything else.
func decodeCursor(c string) (int64, error) {
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err == nil {
		var n int64
		if n, err = strconv.ParseInt(string(b), 10, 64); err == nil && n > 0 && encodeCursor(n) == c {
			return n, nil
		}
	}
	return 0, badRequest("cursor %q is malformed", c)
}

// resolve turns a page of rows into entries, naming each actor and target
// as the screen draws it. Each kind of thing is read in one statement for
// the whole page.
func resolve(ctx context.Context, q *dbq.Queries, rows []dbq.AuditLog) ([]dto.AuditEntry, error) {
	var r resolver
	r.collect(rows)
	if err := r.read(ctx, q); err != nil {
		return nil, err
	}
	out := make([]dto.AuditEntry, 0, len(rows))
	for _, row := range rows {
		e := dto.AuditEntry{ID: row.ID, At: dto.Millis(row.At), Action: row.Action, Target: r.target(row.TargetType, row.TargetID),
			Related: r.related(row), Details: details(row.DetailsJson), IP: row.Ip}
		if row.ActorID.Valid {
			a, ok := r.users[row.ActorID.Int64]
			if !ok {
				a = member.Missing(row.ActorID.Int64)
			}
			e.Actor = &a
		}
		out = append(out, e)
	}
	return out, nil
}

// details is a row's details_json as the answer carries it. The column's
// CHECK keeps it valid JSON; anything the encoder would still refuse is
// carried as a string rather than failing the page.
func details(s string) json.RawMessage {
	if json.Valid([]byte(s)) {
		return json.RawMessage(s)
	}
	b, _ := json.Marshal(s)
	return b
}

// actionThreadMoved is the action whose details name two forums, which the
// entry carries as related (internal/events/projection).
const actionThreadMoved = "thread.moved"

// moved is what a thread.moved row's details say about the forums.
type moved struct {
	From *int64 `json:"from_forum"`
	To   *int64 `json:"to_forum"`
}

// actionPostsMoved is the action whose details name two threads, the one
// the posts left and the one they were copied into, which the entry carries
// as related (internal/events/projection, TKT-01M29CXJZZ).
const actionPostsMoved = "posts.moved"

// postsMoved is what a posts.moved row's details say about the threads, by
// their base32 ids.
type postsMoved struct {
	From string `json:"from_thread"`
	To   string `json:"to_thread"`
}

// threads is the ids a posts.moved row's details name, in order, each one
// that parses as a thread id.
func (m postsMoved) threads() []threadid.ID {
	var out []threadid.ID
	for _, s := range []string{m.From, m.To} {
		if id, err := threadid.Parse(s); err == nil {
			out = append(out, id)
		}
	}
	return out
}

// resolver holds what one page's rows name, by kind, and what the database
// says about each.
type resolver struct {
	userIDs, threadIDs, forumIDs, groupIDs, flagIDs []int64

	users   map[int64]dto.Author
	threads map[int64]dbq.AuditThreadsRow
	forums  map[int64]dbq.AuditForumsRow
	groups  map[int64]string
	flags   map[int64]dbq.AuditFlagsRow
}

// decimalID is a target id in decimal, or false for anything else, which
// then resolves as nothing.
func decimalID(s string) (int64, bool) {
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil && n > 0 && strconv.FormatInt(n, 10) == s
}

// collect records every id rows name, for read.
func (r *resolver) collect(rows []dbq.AuditLog) {
	for _, row := range rows {
		if row.ActorID.Valid {
			r.userIDs = append(r.userIDs, row.ActorID.Int64)
		}
		switch row.TargetType {
		case audit.TypeUser, audit.TypeForum, audit.TypeGroup, audit.TypeFlag:
			n, ok := decimalID(row.TargetID)
			if !ok {
				continue
			}
			switch row.TargetType {
			case audit.TypeUser:
				r.userIDs = append(r.userIDs, n)
			case audit.TypeForum:
				r.forumIDs = append(r.forumIDs, n)
			case audit.TypeGroup:
				r.groupIDs = append(r.groupIDs, n)
			case audit.TypeFlag:
				r.flagIDs = append(r.flagIDs, n)
			}
		case audit.TypeThread:
			if id, err := threadid.Parse(row.TargetID); err == nil {
				r.threadIDs = append(r.threadIDs, int64(id))
			}
		case audit.TypePost:
			if id, _, err := audit.ParsePost(row.TargetID); err == nil {
				r.threadIDs = append(r.threadIDs, int64(id))
			}
		}
		switch row.Action {
		case actionThreadMoved:
			var m moved
			if json.Unmarshal([]byte(row.DetailsJson), &m) == nil {
				for _, f := range []*int64{m.From, m.To} {
					if f != nil {
						r.forumIDs = append(r.forumIDs, *f)
					}
				}
			}
		case actionPostsMoved:
			var m postsMoved
			if json.Unmarshal([]byte(row.DetailsJson), &m) == nil {
				for _, id := range m.threads() {
					r.threadIDs = append(r.threadIDs, int64(id))
				}
			}
		}
	}
}

// uniq is ids sorted with duplicates removed, so a statement names each once.
func uniq(ids []int64) []int64 {
	slices.Sort(ids)
	return slices.Compact(ids)
}

// read looks up everything collect recorded, one statement per kind. Flags
// go first, since the post a flag is on adds a thread to read.
func (r *resolver) read(ctx context.Context, q *dbq.Queries) error {
	r.users, r.threads, r.forums = map[int64]dto.Author{}, map[int64]dbq.AuditThreadsRow{}, map[int64]dbq.AuditForumsRow{}
	r.groups, r.flags = map[int64]string{}, map[int64]dbq.AuditFlagsRow{}
	if ids := uniq(r.flagIDs); len(ids) > 0 {
		fs, err := q.AuditFlags(ctx, ids)
		if err != nil {
			return err
		}
		for _, f := range fs {
			r.flags[f.ID] = f
			r.threadIDs = append(r.threadIDs, f.ThreadID)
		}
	}
	if ids := uniq(r.userIDs); len(ids) > 0 {
		us, err := q.UsersByIDs(ctx, ids)
		if err != nil {
			return err
		}
		for _, u := range us {
			r.users[u.ID] = member.Author(u)
		}
	}
	if ids := uniq(r.threadIDs); len(ids) > 0 {
		ts, err := q.AuditThreads(ctx, ids)
		if err != nil {
			return err
		}
		for _, t := range ts {
			r.threads[t.ID] = t
		}
	}
	if ids := uniq(r.forumIDs); len(ids) > 0 {
		fs, err := q.AuditForums(ctx, ids)
		if err != nil {
			return err
		}
		for _, f := range fs {
			r.forums[f.ID] = f
		}
	}
	if ids := uniq(r.groupIDs); len(ids) > 0 {
		gs, err := q.AuditGroups(ctx, ids)
		if err != nil {
			return err
		}
		for _, g := range gs {
			r.groups[g.ID] = g.Name
		}
	}
	return nil
}

// threadHref is the thread's page.
func threadHref(t dbq.AuditThreadsRow) string {
	return "/t/" + threadid.ID(t.ID).String() + "/" + url.PathEscape(t.Slug)
}

// postHref is a post's permalink.
func postHref(t dbq.AuditThreadsRow, ordinal int64) string {
	return threadHref(t) + "/" + strconv.FormatInt(ordinal, 10)
}

// target is the target typ and id name, labelled and linked as the screen
// draws it. A thing that no longer resolves has its raw id as its label and
// no link, except an absent user, who reads as a deleted member.
func (r *resolver) target(typ, id string) dto.AuditTarget {
	t := dto.AuditTarget{Type: typ, ID: id, Label: id}
	switch typ {
	case audit.TypeUser:
		if n, ok := decimalID(id); ok {
			if u, ok := r.users[n]; ok {
				t.Label, t.Href = u.Handle, "/u/"+url.PathEscape(u.Handle)
			} else {
				t.Label = domain.DeletedMemberHandle(n)
			}
		}
	case audit.TypeThread:
		if tid, err := threadid.Parse(id); err == nil {
			if th, ok := r.threads[int64(tid)]; ok {
				t.Label, t.Href = th.Title, threadHref(th)
			}
		}
	case audit.TypePost:
		if tid, ordinal, err := audit.ParsePost(id); err == nil {
			if th, ok := r.threads[int64(tid)]; ok {
				t.Label, t.Href = th.Title+" #"+strconv.FormatUint(uint64(ordinal), 10), postHref(th, int64(ordinal))
			}
		}
	case audit.TypeForum:
		if n, ok := decimalID(id); ok {
			if f, ok := r.forums[n]; ok {
				t.Label, t.Href = f.Name, "/f/"+url.PathEscape(f.Slug)
			}
		}
	case audit.TypeGroup:
		if n, ok := decimalID(id); ok {
			if name, ok := r.groups[n]; ok {
				t.Label, t.Href = name, "/admin/users?tab=groups"
			}
		}
	case audit.TypeFlag:
		if n, ok := decimalID(id); ok {
			if f, ok := r.flags[n]; ok {
				t.Label = "flag " + id
				if th, ok := r.threads[f.ThreadID]; ok {
					t.Label += " on " + th.Title + " #" + strconv.FormatInt(f.Ordinal, 10)
					t.Href = postHref(th, f.Ordinal)
				}
			}
		}
	case audit.TypeSetting:
		t.Href = "/admin/settings"
	case audit.TypeApplication:
		if _, ok := decimalID(id); ok {
			t.Label, t.Href = "application "+id, "/admin/applications?id="+id
		}
	case audit.TypePermission:
		t.Href = "/admin/users?tab=permissions"
	case audit.TypeForm:
		t.Href = "/admin/forms"
	case audit.TypeIdentity:
		t.Label = "identity " + id
	case audit.TypeBlob:
		// A blob.deleted blob is gone, so it has no page; the label is short
		// enough to read and the id carries the whole hash.
		if len(id) > 12 {
			t.Label = id[:12]
		}
	}
	return t
}

// related is the other things row names: a thread.moved row's forums, the
// one it left and the one it went to, and a posts.moved row's threads, the
// same way round. Empty for every other row.
func (r *resolver) related(row dbq.AuditLog) []dto.AuditTarget {
	out := []dto.AuditTarget{}
	if row.Action == actionPostsMoved {
		var m postsMoved
		if json.Unmarshal([]byte(row.DetailsJson), &m) != nil {
			return out
		}
		for _, id := range m.threads() {
			out = append(out, r.target(audit.TypeThread, id.String()))
		}
		return out
	}
	if row.Action != actionThreadMoved {
		return out
	}
	var m moved
	if json.Unmarshal([]byte(row.DetailsJson), &m) != nil {
		return out
	}
	for _, f := range []*int64{m.From, m.To} {
		if f != nil {
			out = append(out, r.target(audit.TypeForum, strconv.FormatInt(*f, 10)))
		}
	}
	return out
}
