package forums

import (
	"database/sql"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/db/dbtest"
)

// The admin forum screen's service (TKT-01M29CXK19).

type adminFixture struct {
	t     *testing.T
	db    *db.DB
	s     *Service
	admin domain.Viewer
	clock int64
}

func newAdminFixture(t *testing.T) *adminFixture {
	t.Helper()
	d := dbtest.Open(t, db.Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	f := &adminFixture{t: t, db: d, clock: 1_757_500_000_000}
	f.s = &Service{DB: d, Perm: perm.New(d), Now: func() time.Time {
		f.clock += 1000
		return time.UnixMilli(f.clock)
	}}
	var id int64
	if err := d.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		id, err = q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: "dora", DisplayName: "Dora", Status: "active",
			Role: domain.RoleAdmin, Kind: "human", CreatedAt: 1, UpdatedAt: 1})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	f.admin = domain.Viewer{UserID: id, Role: domain.RoleAdmin, Active: true, SessionKey: "s-dora"}
	return f
}

// create makes a forum under parent (0 for a root) and returns its id.
func (f *adminFixture) create(slug string, parent int64) int64 {
	f.t.Helper()
	in := NewForum{Slug: slug, Name: slug}
	if parent != 0 {
		in.ParentID = &parent
	}
	_, id, err := f.s.Create(f.t.Context(), f.admin, "192.0.2.1", in)
	if err != nil {
		f.t.Fatalf("creating %s: %v", slug, err)
	}
	return id
}

func (f *adminFixture) move(id int64, m ForumMove) error {
	f.t.Helper()
	_, err := f.s.Move(f.t.Context(), f.admin, "192.0.2.1", id, m)
	return err
}

func (f *adminFixture) exec(q string, args ...any) error {
	return f.db.Write(f.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(f.t.Context(), q, args...)
		return err
	})
}

// rows is every forum row, by id, as a string of the columns a move may
// change.
func (f *adminFixture) rows() map[int64]string {
	f.t.Helper()
	out := map[int64]string{}
	if err := f.db.Read(f.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		all, err := q.ListForums(f.t.Context())
		for _, r := range all {
			out[r.ID] = fmt.Sprintf("parent=%v position=%d updated=%d", r.ParentID, r.Position, r.UpdatedAt)
		}
		return err
	}); err != nil {
		f.t.Fatal(err)
	}
	return out
}

// changed is the ids whose row differs between before and after.
func changed(before, after map[int64]string) []int64 {
	var out []int64
	for id, a := range after {
		if before[id] != a {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

func isCode(err error, c problem.Code) bool {
	return err != nil && problem.FromError(err) == c
}

// TestForumNestsOneLevel is AC1's nesting rule: a forum under a child, and a
// parent for a forum that has children, are validation-failed and change
// nothing, and the depth trigger still refuses the write made directly.
func TestForumNestsOneLevel(t *testing.T) {
	f := newAdminFixture(t)
	root, other := f.create("root", 0), f.create("other", 0)
	child := f.create("child", root)
	before := f.rows()

	_, _, err := f.s.Create(t.Context(), f.admin, "", NewForum{Slug: "grandchild", Name: "G", ParentID: &child})
	if !isCode(err, problem.CodeValidationFailed) {
		t.Errorf("creating under a child: %v, want validation-failed", err)
	}
	missing := other + 100
	if _, _, err := f.s.Create(t.Context(), f.admin, "", NewForum{Slug: "orphan", Name: "O", ParentID: &missing}); !isCode(err, problem.CodeValidationFailed) {
		t.Errorf("creating under no forum: %v, want validation-failed", err)
	}
	for name, m := range map[string]struct {
		id int64
		m  ForumMove
	}{
		"a root under a child":         {other, ForumMove{ParentID: &child}},
		"a forum with children placed": {root, ForumMove{ParentID: &other}},
		"a forum under itself":         {other, ForumMove{ParentID: &other}},
	} {
		if err := f.move(m.id, m.m); !isCode(err, problem.CodeValidationFailed) {
			t.Errorf("%s: %v, want validation-failed", name, err)
		}
	}
	if got := changed(before, f.rows()); len(got) != 0 {
		t.Errorf("refused changes wrote rows %v", got)
	}
	if err := f.exec(`UPDATE forums SET parent_id = ? WHERE id = ?`, child, other); err == nil ||
		!strings.Contains(err.Error(), "nest one level") {
		t.Errorf("the direct write under a child: %v, want the trigger's refusal", err)
	}
}

// TestMoveWritesOnlyTheMovedRow is AC2. Into a gap, a move changes the moved
// row's position and updated_at and nothing else. With no gap, only the
// destination parent's children are renumbered, in their order with the
// forum in its new place, and another parent's rows are untouched.
func TestMoveWritesOnlyTheMovedRow(t *testing.T) {
	f := newAdminFixture(t)
	a, b := f.create("a", 0), f.create("b", 0)
	a1, a2, a3 := f.create("a1", a), f.create("a2", a), f.create("a3", a)
	b1, b2 := f.create("b1", b), f.create("b2", b)

	before := f.rows()
	if err := f.move(a3, ForumMove{ParentID: &a, Before: a2}); err != nil {
		t.Fatal(err)
	}
	if got := changed(before, f.rows()); !slices.Equal(got, []int64{a3}) {
		t.Errorf("a move into a gap changed %v, want only %d", got, a3)
	}
	f.order(t, a, a1, a3, a2)

	// Across parents, into a gap: the moved row alone.
	before = f.rows()
	if err := f.move(b2, ForumMove{ParentID: &a, After: a1}); err != nil {
		t.Fatal(err)
	}
	if got := changed(before, f.rows()); !slices.Equal(got, []int64{b2}) {
		t.Errorf("a move to another parent changed %v, want only %d", got, b2)
	}
	f.order(t, a, a1, b2, a3, a2)
	f.order(t, b, b1)

	// No gap: a's children sit at 1, 2, 3, 4.
	for i, id := range []int64{a1, b2, a3, a2} {
		if err := f.exec(`UPDATE forums SET position = ? WHERE id = ?`, i+1, id); err != nil {
			t.Fatal(err)
		}
	}
	before = f.rows()
	if err := f.move(a2, ForumMove{ParentID: &a, Before: b2}); err != nil {
		t.Fatal(err)
	}
	after := f.rows()
	for _, id := range changed(before, after) {
		if id != a1 && id != b2 && id != a3 && id != a2 {
			t.Errorf("a renumbering move changed %d, which is not under a", id)
		}
	}
	for _, id := range []int64{a, b, b1} {
		if before[id] != after[id] {
			t.Errorf("forum %d changed: %s then %s", id, before[id], after[id])
		}
	}
	f.order(t, a, a1, a2, b2, a3)

	// A move to where the forum already is writes nothing.
	before = f.rows()
	if err := f.move(a2, ForumMove{ParentID: &a, After: a1}); err != nil {
		t.Fatal(err)
	}
	if got := changed(before, f.rows()); len(got) != 0 {
		t.Errorf("a move to where it was changed %v", got)
	}
	if err := f.move(a2, ForumMove{ParentID: &a, After: b1}); !isCode(err, problem.CodeValidationFailed) {
		t.Errorf("after a forum under another parent: %v, want validation-failed", err)
	}
	if err := f.move(a2, ForumMove{ParentID: &a, After: a1, Before: a3}); !isCode(err, problem.CodeValidationFailed) {
		t.Errorf("both before and after: %v, want validation-failed", err)
	}
}

// order checks the children of parent (0 for the roots) come in want's order
// in the list the screen reads.
func (f *adminFixture) order(t *testing.T, parent int64, want ...int64) {
	t.Helper()
	list, err := f.s.List(t.Context(), f.admin)
	if err != nil {
		t.Fatal(err)
	}
	var got []int64
	for _, m := range list.Forums {
		if (parent == 0 && m.ParentID == nil) || (m.ParentID != nil && *m.ParentID == parent) {
			got = append(got, m.ID)
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("under %d: %v, want %v", parent, got, want)
	}
}

// TestMoveKeepsOrderUnderRandomMoves is AC2's order: 500 random moves, half
// of them to the front of a parent so the gaps run out and renumber, each
// checked against a reference model of every parent's children. The list
// always has each root followed by its children.
func TestMoveKeepsOrderUnderRandomMoves(t *testing.T) {
	f := newAdminFixture(t)
	rng := rand.New(rand.NewPCG(1, 2))
	model := map[int64][]int64{} // parent (0 for the roots) -> children in order
	parentOf := map[int64]int64{}
	for i := range 4 {
		r := f.create(fmt.Sprintf("r%d", i), 0)
		model[0] = append(model[0], r)
		for j := range 3 {
			c := f.create(fmt.Sprintf("r%d-%d", i, j), r)
			model[r] = append(model[r], c)
			parentOf[c] = r
		}
	}
	all := func() []int64 {
		var out []int64
		for _, kids := range model {
			out = append(out, kids...)
		}
		slices.Sort(out)
		return out
	}
	without := func(s []int64, id int64) []int64 {
		return slices.DeleteFunc(slices.Clone(s), func(x int64) bool { return x == id })
	}

	for step := range 500 {
		ids := all()
		id := ids[rng.IntN(len(ids))]
		// The destination: the top level, or a root other than id; a forum
		// with children may only go to the top level.
		dest := int64(0)
		if len(model[id]) == 0 && rng.IntN(3) > 0 {
			var roots []int64
			for _, r := range model[0] {
				if r != id {
					roots = append(roots, r)
				}
			}
			dest = roots[rng.IntN(len(roots))]
		}
		sibs := without(model[dest], id)
		m := ForumMove{}
		if dest != 0 {
			m.ParentID = &dest
		}
		to := len(sibs)
		switch k := rng.IntN(4); {
		case len(sibs) == 0 || k == 3:
		case k < 2:
			m.Before, to = sibs[0], 0
		default:
			i := rng.IntN(len(sibs))
			m.After, to = sibs[i], i+1
		}
		if err := f.move(id, m); err != nil {
			t.Fatalf("step %d: moving %d with %+v: %v", step, id, m, err)
		}
		from := parentOf[id]
		model[from] = without(model[from], id)
		model[dest] = slices.Insert(without(model[dest], id), to, id)
		parentOf[id] = dest
		if len(model[from]) == 0 && from != 0 {
			delete(model, from)
		}

		list, err := f.s.List(t.Context(), f.admin)
		if err != nil {
			t.Fatal(err)
		}
		var want []int64
		for _, r := range model[0] {
			want = append(want, r)
			want = append(want, model[r]...)
		}
		if got := ids2(list); !slices.Equal(got, want) {
			t.Fatalf("step %d, after moving %d with %+v: the list is %v, want %v", step, id, m, got, want)
		}
	}
	var renumbered int
	if err := f.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT count(*) FROM audit_log WHERE action = ?
			AND json_extract(details_json, '$.renumbered')`, ActionMoved).Scan(&renumbered)
	}); err != nil {
		t.Fatal(err)
	}
	if renumbered == 0 {
		t.Error("no move renumbered, so the no-gap path was never walked")
	}
}

func ids2(l dto.ManagedForumList) []int64 {
	out := make([]int64, len(l.Forums))
	for i, m := range l.Forums {
		out[i] = m.ID
	}
	return out
}
