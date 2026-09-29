// Package formsvc stores and loads the versions of declarative forms
// (docs/05 "Forms"), and answers the admin form routes (docs/07
// "Moderation and admin").
//
// It is named formsvc rather than forms so that it and internal/forms, the
// pure schema package it calls, can be imported side by side.
//
// Every version is parsed when it is loaded. A stored schema that does not
// parse is ErrCorruptSchema, which nothing maps to an API code: it is a 500
// and an error-level log naming the form and version, never a form served
// without the field it could not read.
package formsvc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/forms"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/service/member"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// Registration is the key of the registration form migration 0011 seeds.
const Registration = "registration"

// ActionVersionCreated is the audit action for a saved version.
const ActionVersionCreated = "form.version_created"

var (
	// ErrNoForm is a key or version that names no form. It is not-found.
	ErrNoForm = errors.New("formsvc: no such form or version")

	// ErrStaleVersion is a save whose base_version is not the active
	// version: another admin saved since the editor loaded the form. It is
	// conflict, so neither save silently overwrites the other.
	ErrStaleVersion = errors.New("formsvc: the form changed since the editor loaded it")

	// ErrCorruptSchema is a stored schema that does not parse. It does not
	// wrap forms.ErrInvalidSchema, which would map it to validation-failed:
	// the data is broken, not the request.
	ErrCorruptSchema = errors.New("formsvc: a stored form schema does not parse")
)

// Service is the forms service.
type Service struct {
	DB *db.DB
	// Now is the clock for created_at and the audit row. Nil is time.Now.
	Now func() time.Time
}

// Version is one stored version of a form, parsed.
type Version struct {
	FormID int64
	Key    string
	Number int
	Schema forms.Schema
	// Raw is schema_json as stored.
	Raw       json.RawMessage
	CreatedAt int64
	// CreatedBy is the admin who saved it, 0 for the seeded version.
	CreatedBy int64
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Active is the version of the form named key that an applicant is shown.
func (s *Service) Active(ctx context.Context, key string) (Version, error) {
	var v Version
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		v, err = ActiveIn(ctx, q, key)
		return err
	})
	return v, err
}

// ActiveIn is Active inside a transaction the caller holds, so a caller that
// writes against the active version reads it in the same transaction as the
// write: an application is checked against the version that is active when
// it is stored, not one read a moment before (internal/service/applications).
func ActiveIn(ctx context.Context, q *dbq.Queries, key string) (Version, error) {
	f, err := formByKey(ctx, q, key)
	if err != nil {
		return Version{}, err
	}
	return load(ctx, q, f, int(f.ActiveVersion))
}

// Version is version n of the form with id formID, whichever version is
// active: an application is read against the version it answered.
func (s *Service) Version(ctx context.Context, formID int64, n int) (Version, error) {
	var v Version
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		v, err = VersionIn(ctx, q, formID, n)
		return err
	})
	return v, err
}

// VersionIn is Version inside a transaction the caller holds, so a review
// reads an application and the version it answered together
// (internal/service/applications).
func VersionIn(ctx context.Context, q *dbq.Queries, formID int64, n int) (Version, error) {
	f, err := q.GetFormByID(ctx, formID)
	if errors.Is(err, sql.ErrNoRows) {
		return Version{}, problem.Coded(problem.CodeNotFound, fmt.Errorf("%w: form id %d", ErrNoForm, formID))
	}
	if err != nil {
		return Version{}, err
	}
	return load(ctx, q, f, n)
}

// Admin is GET /admin/forms/{key}, and with n > 0 GET
// /admin/forms/{key}/versions/{n}: the form, its versions newest first,
// and version n (or the active one) rendered beside its stored schema.
// Admins only.
func (s *Service) Admin(ctx context.Context, viewer domain.Viewer, key string, n int) (dto.FormAdmin, error) {
	var out dto.FormAdmin
	if err := requireAdmin(viewer, "read a form's versions"); err != nil {
		return out, err
	}
	var v Version
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		f, err := formByKey(ctx, q, key)
		if err != nil {
			return err
		}
		if n == 0 {
			n = int(f.ActiveVersion)
		}
		if v, err = load(ctx, q, f, n); err != nil {
			return err
		}
		rows, err := q.ListFormVersions(ctx, f.ID)
		if err != nil {
			return err
		}
		authors, err := creators(ctx, q, rows)
		if err != nil {
			return err
		}
		out = dto.FormAdmin{Key: f.Key, Title: f.Title, ActiveVersion: int(f.ActiveVersion),
			Versions: make([]dto.FormVersionInfo, 0, len(rows))}
		for _, r := range rows {
			info := dto.FormVersionInfo{Version: int(r.Version), CreatedAt: dto.Millis(r.CreatedAt)}
			if r.CreatedBy.Valid {
				a := authors[r.CreatedBy.Int64]
				info.CreatedBy = &a
			}
			out.Versions = append(out.Versions, info)
		}
		return nil
	})
	if err != nil {
		return dto.FormAdmin{}, err
	}
	if out.Form, err = ToDTO(v); err != nil {
		return dto.FormAdmin{}, err
	}
	out.Schema = v.Raw
	return out, nil
}

// Create saves schema as the next version of the form named key and makes
// it active, for POST /admin/forms/{key}/versions. Admins only.
//
// base is the version the editor loaded. It must be the active version, or
// the save is ErrStaleVersion: without the check, two admins editing at once
// would each overwrite the other's change as a newer version.
//
// The schema is parsed first, so one that breaks the rules writes nothing,
// and stored in its canonical form. The version, the move of active_version
// and the form.version_created audit row commit together or not at all. ip
// is the request's address, for the audit row.
func (s *Service) Create(ctx context.Context, viewer domain.Viewer, key string, base int, schema []byte, ip string) (int, error) {
	if err := requireAdmin(viewer, "save a form version"); err != nil {
		return 0, err
	}
	parsed, err := forms.Parse(schema)
	if err != nil {
		return 0, err
	}
	canonical, err := forms.Canonical(parsed)
	if err != nil {
		return 0, err
	}
	now := s.now().UnixMilli()
	var n int
	err = s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		f, err := formByKey(ctx, q, key)
		if err != nil {
			return err
		}
		if int(f.ActiveVersion) != base {
			return problem.Coded(problem.CodeConflict, fmt.Errorf("%w: form %q is at version %d, the save was based on %d",
				ErrStaleVersion, key, f.ActiveVersion, base))
		}
		last, err := q.MaxFormVersion(ctx, f.ID)
		if err != nil {
			return err
		}
		n = int(last) + 1
		if err := q.InsertFormVersion(ctx, dbq.InsertFormVersionParams{FormID: f.ID, Version: int64(n),
			SchemaJson: string(canonical), CreatedBy: sql.NullInt64{Int64: viewer.UserID, Valid: true}, CreatedAt: now}); err != nil {
			return err
		}
		if err := q.SetActiveFormVersion(ctx, dbq.SetActiveFormVersionParams{ActiveVersion: int64(n), UpdatedAt: now, ID: f.ID}); err != nil {
			return err
		}
		_, err = audit.Append(ctx, q, time.UnixMilli(now), audit.By{Actor: viewer.UserID, IP: ip},
			ActionVersionCreated, audit.Form(f.Key), map[string]any{"key": f.Key, "version": n})
		return err
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}

// Preview renders schema as the form named key would show it, without
// writing anything, for POST /admin/forms/{key}/preview. Admins only. The
// draft is version 0. A schema that breaks the rules is refused as a save
// would refuse it.
func (s *Service) Preview(ctx context.Context, viewer domain.Viewer, key string, schema []byte) (dto.Form, error) {
	if err := requireAdmin(viewer, "preview a form"); err != nil {
		return dto.Form{}, err
	}
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		_, err := formByKey(ctx, q, key)
		return err
	})
	if err != nil {
		return dto.Form{}, err
	}
	parsed, err := forms.Parse(schema)
	if err != nil {
		return dto.Form{}, err
	}
	return ToDTO(Version{Key: key, Schema: parsed})
}

// ToDTO is v as a client renders it. intro_markdown and each
// markdown_notice go through internal/render, so they get its sanitizer
// and its rule that a remote image is a link (docs/07).
func ToDTO(v Version) (dto.Form, error) {
	intro, err := markdown(v.Schema.IntroMarkdown)
	if err != nil {
		return dto.Form{}, fmt.Errorf("formsvc: form %q version %d: the intro: %w", v.Key, v.Number, err)
	}
	out := dto.Form{Key: v.Key, Version: v.Number, Title: v.Schema.Title, IntroHTML: intro,
		Fields: make([]dto.FormField, 0, len(v.Schema.Fields))}
	for _, f := range v.Schema.Fields {
		html, err := markdown(f.Markdown)
		if err != nil {
			return dto.Form{}, fmt.Errorf("formsvc: form %q version %d: a notice: %w", v.Key, v.Number, err)
		}
		options := f.Options
		if options == nil {
			options = []string{}
		}
		out.Fields = append(out.Fields, dto.FormField{Key: f.Key, Type: f.Type, Label: f.Label, Required: f.Required,
			Max: forms.Limit(f), Options: options, Multiple: f.Multiple, Locked: f.Locked, FromClaim: f.FromClaim, HTML: html})
	}
	return out, nil
}

func markdown(source string) (string, error) {
	if source == "" {
		return "", nil
	}
	r, err := render.Render(source)
	return r.HTML, err
}

// requireAdmin is the service's own check that viewer is an active admin.
// The admin gate checks the same before a handler runs; authority lives in
// the service too (docs/07 "Principles"), so a second caller of it does not
// have to remember.
func requireAdmin(v domain.Viewer, what string) error {
	if err := member.Require(v, what); err != nil {
		return err
	}
	if v.Role != domain.RoleAdmin {
		return problem.Coded(problem.CodeForbidden, fmt.Errorf("formsvc: only an admin may %s", what))
	}
	return nil
}

func formByKey(ctx context.Context, q *dbq.Queries, key string) (dbq.Form, error) {
	f, err := q.GetFormByKey(ctx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return f, problem.Coded(problem.CodeNotFound, fmt.Errorf("%w: %q", ErrNoForm, key))
	}
	return f, err
}

// load reads version n of f and parses it.
func load(ctx context.Context, q *dbq.Queries, f dbq.Form, n int) (Version, error) {
	row, err := q.GetFormVersion(ctx, dbq.GetFormVersionParams{FormID: f.ID, Version: int64(n)})
	if errors.Is(err, sql.ErrNoRows) {
		return Version{}, problem.Coded(problem.CodeNotFound, fmt.Errorf("%w: form %q has no version %d", ErrNoForm, f.Key, n))
	}
	if err != nil {
		return Version{}, err
	}
	parsed, err := forms.Parse([]byte(row.SchemaJson))
	if err != nil {
		// %v, not %w: see ErrCorruptSchema.
		return Version{}, fmt.Errorf("%w: form %q version %d: %v", ErrCorruptSchema, f.Key, n, err)
	}
	return Version{FormID: f.ID, Key: f.Key, Number: n, Schema: parsed, Raw: json.RawMessage(row.SchemaJson),
		CreatedAt: row.CreatedAt, CreatedBy: row.CreatedBy.Int64}, nil
}

// creators draws the admin behind each version that has one.
func creators(ctx context.Context, q *dbq.Queries, rows []dbq.ListFormVersionsRow) (map[int64]dto.Author, error) {
	var ids []int64
	for _, r := range rows {
		if r.CreatedBy.Valid && !slices.Contains(ids, r.CreatedBy.Int64) {
			ids = append(ids, r.CreatedBy.Int64)
		}
	}
	out := map[int64]dto.Author{}
	if len(ids) == 0 {
		return out, nil
	}
	users, err := q.UsersByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		out[u.ID] = member.Author(u)
	}
	member.FillMissing(out, ids...)
	return out, nil
}
