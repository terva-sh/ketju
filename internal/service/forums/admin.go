package forums

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/member"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// The admin forum screen (TKT-01M29CXK19, docs/07 "Forums"): creating,
// editing, moving, archiving and setting the visibility of forums. Every
// method is an admin's alone, and asks again whatever gate the caller
// passed.
//
// Visibility and archiving change who may read and post, so they go through
// internal/perm, which audits them and clears its decision cache. Creating,
// editing and moving change no decision: a new forum has no permission rows
// and nothing cached about it, and a forum's name, description, parent and
// position are not read by permission evaluation. They are written here,
// each with its audit row in the same transaction.
//
// Every write answers with the whole list, read in the write's transaction,
// so the answer is what committed: a move may have renumbered the siblings.

// The audit_log actions this package writes, against the forum.
// perm.ActionForumVisibilityChanged, perm.ActionForumArchived and
// perm.ActionForumUnarchived are the other three.
const (
	ActionCreated = "forum.created"
	ActionUpdated = "forum.updated"
	ActionMoved   = "forum.moved"
)

// Limits with no product meaning beyond keeping a forum's words bounded.
const (
	SlugMaxLen        = 60
	NameMaxLen        = 100
	DescriptionMaxLen = 1000
	// Gap is the space a new forum leaves after its last sibling, and the
	// step a renumbering spaces siblings by. A move into the middle takes
	// the integer midway between its neighbours, so ten moves into the
	// same place fit before one renumbers.
	Gap = 1024
)

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func invalid(format string, args ...any) error {
	return problem.Coded(problem.CodeValidationFailed, fmt.Errorf("forums: "+format, args...))
}

// NewForum is POST /admin/forums's body.
type NewForum struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// ParentID is the root to create it under, or null for a root.
	ParentID *int64 `json:"parent_id"`
	// Visibility is public, members or restricted; empty is members, the
	// schema's default.
	Visibility string `json:"visibility"`
}

// ForumEdit is PATCH /admin/forums/{id}'s body. A nil field is left as it
// is. The slug is not here: it is fixed at creation.
type ForumEdit struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

// ForumMove is a move: the parent to place the forum under, nil for the top
// level, and at most one sibling there to go before or after. Neither puts
// it last. The route decodes it itself, because its body must name
// parent_id, and a pointer cannot tell an omitted field from null.
type ForumMove struct {
	ParentID *int64
	Before   int64
	After    int64
}

// ValidateSlug is a new forum's slug rule: 1 to SlugMaxLen characters of
// a-z, 0-9 and '-', with no '-' first or last. The slug is the forum's
// address, /f/{slug}, so it is kept to what a URL carries unquoted.
func ValidateSlug(slug string) error {
	if n := len(slug); n < 1 || n > SlugMaxLen {
		return invalid("a slug has 1 to %d characters, not %d", SlugMaxLen, n)
	}
	for i := 0; i < len(slug); i++ {
		c := slug[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '-' && i > 0 && i < len(slug)-1:
		case c == '-':
			return invalid("a slug does not start or end with '-'")
		default:
			return invalid("%q is not allowed in a slug; use a-z, 0-9 or '-'", rune(c))
		}
	}
	return nil
}

// checkName trims and checks a forum's name: 1 to NameMaxLen characters,
// no control characters.
func checkName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) {
		return "", invalid("a name is UTF-8")
	}
	if n := utf8.RuneCountInString(name); n < 1 || n > NameMaxLen {
		return "", invalid("a name has 1 to %d characters, not %d", NameMaxLen, n)
	}
	if strings.ContainsFunc(name, unicode.IsControl) {
		return "", invalid("a name has no control characters")
	}
	return name, nil
}

// checkDescription trims and checks a description: at most
// DescriptionMaxLen characters, and no control characters but line breaks
// and tabs.
func checkDescription(d string) (string, error) {
	d = strings.TrimSpace(d)
	if !utf8.ValidString(d) {
		return "", invalid("a description is UTF-8")
	}
	if n := utf8.RuneCountInString(d); n > DescriptionMaxLen {
		return "", invalid("a description has at most %d characters, not %d", DescriptionMaxLen, n)
	}
	if strings.ContainsFunc(d, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) {
		return "", invalid("a description has no control characters but line breaks and tabs")
	}
	return d, nil
}

// List is GET /admin/forums.
func (s *Service) List(ctx context.Context, v domain.Viewer) (dto.ManagedForumList, error) {
	if err := member.RequireAdmin(v, "list the forums to manage them"); err != nil {
		return dto.ManagedForumList{Forums: []dto.ManagedForum{}}, err
	}
	var out dto.ManagedForumList
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		out, err = managed(ctx, q)
		return err
	})
	return out, err
}

// managed is every forum in ListForums order, with its counts, in q's
// transaction.
func managed(ctx context.Context, q *dbq.Queries) (dto.ManagedForumList, error) {
	out := dto.ManagedForumList{Forums: []dto.ManagedForum{}}
	rows, err := q.ListForums(ctx)
	if err != nil {
		return out, err
	}
	counts, err := q.ForumThreadCounts(ctx)
	if err != nil {
		return out, err
	}
	threads, posts := map[int64]int{}, map[int64]int{}
	for _, c := range counts {
		threads[c.ForumID], posts[c.ForumID] = int(c.Threads), int(c.LivePosts)
	}
	for _, f := range rows {
		m := dto.ManagedForum{ID: f.ID, Slug: f.Slug, Name: f.Name, Description: f.Description,
			Position: f.Position, Visibility: f.Visibility, Archived: f.Archived != 0,
			Threads: threads[f.ID], Posts: posts[f.ID]}
		if f.ParentID.Valid {
			m.ParentID = &f.ParentID.Int64
		}
		out.Forums = append(out.Forums, m)
	}
	return out, nil
}

// getForum reads id in q's transaction, or not-found.
func getForum(ctx context.Context, q *dbq.Queries, id int64) (dbq.Forum, error) {
	f, err := q.GetForum(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return f, perm.ErrNoForum
	}
	return f, err
}

// checkParent is the rule for a parent: it exists and is a root, since
// forums nest one level (docs/04 "forums"). The depth triggers hold the same
// rule on every write path; this says it as validation-failed first.
func checkParent(ctx context.Context, q *dbq.Queries, parentID int64) error {
	p, err := q.GetForum(ctx, parentID)
	if errors.Is(err, sql.ErrNoRows) {
		return invalid("no forum %d to place it under", parentID)
	}
	if err != nil {
		return err
	}
	if p.ParentID.Valid {
		return invalid("%s is under another forum, and forums nest one level", p.Slug)
	}
	return nil
}

func nullable(id *int64) sql.NullInt64 {
	if id == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *id, Valid: true}
}

// idOrNil is a parent id for an audit row's details: null for a root.
func idOrNil(p sql.NullInt64) any {
	if !p.Valid {
		return nil
	}
	return p.Int64
}

// Create is POST /admin/forums: a new forum, last among its siblings.
func (s *Service) Create(ctx context.Context, v domain.Viewer, ip string, in NewForum) (dto.ManagedForumList, int64, error) {
	var out dto.ManagedForumList
	if err := member.RequireAdmin(v, "create a forum"); err != nil {
		return out, 0, err
	}
	if err := ValidateSlug(in.Slug); err != nil {
		return out, 0, err
	}
	name, err := checkName(in.Name)
	if err != nil {
		return out, 0, err
	}
	description, err := checkDescription(in.Description)
	if err != nil {
		return out, 0, err
	}
	visibility := in.Visibility
	switch visibility {
	case "":
		visibility = domain.VisibilityMembers
	case domain.VisibilityPublic, domain.VisibilityMembers, domain.VisibilityRestricted:
	default:
		return out, 0, invalid("%q is not a visibility: public, members or restricted", in.Visibility)
	}
	var id int64
	err = s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		now := s.now()
		if _, err := q.GetForumBySlug(ctx, in.Slug); err == nil {
			return problem.Coded(problem.CodeConflict, fmt.Errorf("forums: the slug %q is taken", in.Slug))
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if in.ParentID != nil {
			if err := checkParent(ctx, q, *in.ParentID); err != nil {
				return err
			}
		}
		sibs, err := q.ForumSiblings(ctx, nullable(in.ParentID))
		if err != nil {
			return err
		}
		position := int64(Gap)
		if len(sibs) > 0 {
			position = sibs[len(sibs)-1].Position + Gap
		}
		id, err = q.CreateForum(ctx, dbq.CreateForumParams{ParentID: nullable(in.ParentID), Slug: in.Slug, Name: name,
			Description: description, Position: position, Visibility: visibility,
			CreatedAt: now.UnixMilli(), UpdatedAt: now.UnixMilli()})
		if err != nil {
			return err
		}
		if _, err := audit.Append(ctx, q, now, audit.By{Actor: v.UserID, IP: ip}, ActionCreated, audit.Forum(id),
			map[string]any{"slug": in.Slug, "name": name, "parent": idOrNil(nullable(in.ParentID)),
				"visibility": visibility}); err != nil {
			return err
		}
		out, err = managed(ctx, q)
		return err
	})
	return out, id, err
}

// Update is PATCH /admin/forums/{id}: a new name, description, or both. A
// field sent as it already is changes nothing, and a change that changes
// nothing writes nothing.
func (s *Service) Update(ctx context.Context, v domain.Viewer, ip string, id int64, in ForumEdit) (dto.ManagedForumList, error) {
	var out dto.ManagedForumList
	if err := member.RequireAdmin(v, "change a forum"); err != nil {
		return out, err
	}
	if in.Name == nil && in.Description == nil {
		return out, invalid("the change names neither a name nor a description")
	}
	if in.Name != nil {
		n, err := checkName(*in.Name)
		if err != nil {
			return out, err
		}
		in.Name = &n
	}
	if in.Description != nil {
		d, err := checkDescription(*in.Description)
		if err != nil {
			return out, err
		}
		in.Description = &d
	}
	err := s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		now := s.now()
		f, err := getForum(ctx, q, id)
		if err != nil {
			return err
		}
		details := map[string]any{"slug": f.Slug}
		name, description := f.Name, f.Description
		if in.Name != nil && *in.Name != f.Name {
			details["name"] = map[string]any{"from": f.Name, "to": *in.Name}
			name = *in.Name
		}
		if in.Description != nil && *in.Description != f.Description {
			details["description"] = map[string]any{"from": f.Description, "to": *in.Description}
			description = *in.Description
		}
		if len(details) > 1 {
			if err := q.UpdateForum(ctx, dbq.UpdateForumParams{Name: name, Description: description,
				UpdatedAt: now.UnixMilli(), ID: id}); err != nil {
				return err
			}
			if _, err := audit.Append(ctx, q, now, audit.By{Actor: v.UserID, IP: ip}, ActionUpdated, audit.Forum(id),
				details); err != nil {
				return err
			}
		}
		out, err = managed(ctx, q)
		return err
	})
	return out, err
}

// Move is POST /admin/forums/{id}/move: the forum placed under m.ParentID,
// or at the top level, before or after a sibling there, or last.
//
// Positions are sparse. The forum takes the integer midway between its new
// neighbours, so a move writes its own row and no other. Only when the
// neighbours have no integer between them are that parent's children
// renumbered to multiples of Gap, in their order with the forum in its new
// place, all in this transaction. A move that leaves the forum where it was
// writes nothing.
func (s *Service) Move(ctx context.Context, v domain.Viewer, ip string, id int64, m ForumMove) (dto.ManagedForumList, error) {
	var out dto.ManagedForumList
	if err := member.RequireAdmin(v, "move a forum"); err != nil {
		return out, err
	}
	if m.Before != 0 && m.After != 0 {
		return out, invalid("a move names a sibling to go before or after, not both")
	}
	err := s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		now := s.now()
		f, err := getForum(ctx, q, id)
		if err != nil {
			return err
		}
		parent := nullable(m.ParentID)
		if parent.Valid {
			if parent.Int64 == id {
				return invalid("a forum cannot be placed under itself")
			}
			if err := checkParent(ctx, q, parent.Int64); err != nil {
				return err
			}
			n, err := q.ForumChildCount(ctx, sql.NullInt64{Int64: id, Valid: true})
			if err != nil {
				return err
			}
			if n > 0 {
				return invalid("%s has forums under it, and forums nest one level", f.Slug)
			}
		}
		all, err := q.ForumSiblings(ctx, parent)
		if err != nil {
			return err
		}
		// sibs is the destination's children without the forum; at is its
		// index there before the move, or -1 when it comes from another
		// parent, so at == to means it stays where it is.
		sibs := make([]dbq.ForumSiblingsRow, 0, len(all))
		at := -1
		for _, r := range all {
			if r.ID == id {
				at = len(sibs)
				continue
			}
			sibs = append(sibs, r)
		}
		to, err := placeAt(sibs, m)
		if err != nil {
			return err
		}
		if at == to {
			out, err = managed(ctx, q)
			return err
		}
		position, ok := between(sibs, to)
		renumbered := !ok
		if renumbered {
			position, err = renumber(ctx, q, sibs, to, now.UnixMilli())
			if err != nil {
				return err
			}
		}
		if err := q.PlaceForum(ctx, dbq.PlaceForumParams{ParentID: parent, Position: position,
			UpdatedAt: now.UnixMilli(), ID: id}); err != nil {
			return err
		}
		if _, err := audit.Append(ctx, q, now, audit.By{Actor: v.UserID, IP: ip}, ActionMoved, audit.Forum(id),
			map[string]any{"slug": f.Slug, "from_parent": idOrNil(f.ParentID), "to_parent": idOrNil(parent),
				"position": position, "renumbered": renumbered}); err != nil {
			return err
		}
		out, err = managed(ctx, q)
		return err
	})
	return out, err
}

// placeAt is the index among sibs the forum goes to: before m.Before, after
// m.After, or last. A sibling that is not in sibs is validation-failed.
func placeAt(sibs []dbq.ForumSiblingsRow, m ForumMove) (int, error) {
	ref := m.Before
	if ref == 0 {
		ref = m.After
	}
	if ref == 0 {
		return len(sibs), nil
	}
	for i, r := range sibs {
		if r.ID == ref {
			if m.After != 0 {
				return i + 1, nil
			}
			return i, nil
		}
	}
	return 0, invalid("forum %d is not another forum under that parent", ref)
}

// between is the position midway between the neighbours of index to in
// sibs, and whether there is one. First place is bounded below by 0, so
// positions stay positive; last place has no bound above and is Gap after
// the last sibling.
func between(sibs []dbq.ForumSiblingsRow, to int) (int64, bool) {
	if len(sibs) == 0 {
		return Gap, true
	}
	if to == len(sibs) {
		return sibs[len(sibs)-1].Position + Gap, true
	}
	var lo int64
	if to > 0 {
		lo = sibs[to-1].Position
	}
	hi := sibs[to].Position
	if hi-lo < 2 {
		return 0, false
	}
	return lo + (hi-lo)/2, true
}

// renumber spaces sibs by Gap, in their order with a slot left at index to,
// writing only the rows whose position changes, and returns the slot's
// position.
func renumber(ctx context.Context, q *dbq.Queries, sibs []dbq.ForumSiblingsRow, to int, now int64) (int64, error) {
	slot := int64(to+1) * Gap
	for i, r := range sibs {
		k := i
		if i >= to {
			k++
		}
		p := int64(k+1) * Gap
		if p == r.Position {
			continue
		}
		if err := q.SetForumPosition(ctx, dbq.SetForumPositionParams{Position: p, UpdatedAt: now, ID: r.ID}); err != nil {
			return 0, err
		}
	}
	return slot, nil
}

// SetVisibility is PUT /admin/forums/{id}/visibility, through perm, which
// audits it and clears the decision cache when it commits, so it applies
// from every viewer's next request.
func (s *Service) SetVisibility(ctx context.Context, v domain.Viewer, ip string, id int64, visibility string) (dto.ManagedForumList, error) {
	var out dto.ManagedForumList
	if err := member.RequireAdmin(v, "change a forum's visibility"); err != nil {
		return out, err
	}
	err := s.Perm.SetVisibilityThen(ctx, audit.By{Actor: v.UserID, IP: ip}, id, visibility, func(q *dbq.Queries) error {
		var err error
		out, err = managed(ctx, q)
		return err
	})
	return out, err
}

// SetArchived is PUT /admin/forums/{id}/archived, through perm. Archiving
// moves no thread and writes no thread record: read access is unchanged,
// and posting is refused to everyone but admins (docs/04 "permissions").
func (s *Service) SetArchived(ctx context.Context, v domain.Viewer, ip string, id int64, archived bool) (dto.ManagedForumList, error) {
	var out dto.ManagedForumList
	if err := member.RequireAdmin(v, "archive a forum"); err != nil {
		return out, err
	}
	err := s.Perm.SetForumArchivedThen(ctx, audit.By{Actor: v.UserID, IP: ip}, id, archived, func(q *dbq.Queries) error {
		var err error
		out, err = managed(ctx, q)
		return err
	})
	return out, err
}
