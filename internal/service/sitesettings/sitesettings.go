// Package sitesettings answers the admin site settings screen
// (TKT-01M29CXK2H, docs/07 "Site settings"): every runtime setting in the
// registry, who last set each one, and the parts of config.toml the screen
// names but does not show.
//
// Settings change what the site does, registration mode and the agent rate
// limits among them, so both methods are an admin's alone. The API's admin
// gate refuses everyone else before a handler runs, and each method asks
// again, so a caller that forgot the gate is still refused. Holding
// moderate somewhere opens nothing here (08 "Decided 2026-09-30").
//
// A change goes through settings.SetIn, which refuses an unknown key or a
// value the key does not take and writes setting.changed with the admin,
// the address and the old and new values in the same transaction, which is
// also the one the answer is read in. Nothing
// here caches a value: every reader asks the table, so a change applies from
// the next request.
package sitesettings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/terva-sh/ketju/internal/config"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/member"
	"github.com/terva-sh/ketju/internal/settings"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// Service answers the settings screen.
type Service struct {
	DB *db.DB
	// Now is the clock a change is stamped with; nil is time.Now.
	Now func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

// Get is GET /admin/settings: every setting, sorted by key, and every
// config.toml section. Admin only.
func (s *Service) Get(ctx context.Context, v domain.Viewer) (dto.SiteSettings, error) {
	out := dto.SiteSettings{Settings: []dto.SiteSetting{}, Config: []dto.ConfigSection{}}
	if err := member.RequireAdmin(v, "read the site settings"); err != nil {
		return out, err
	}
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		values, err := settings.CurrentIn(ctx, q)
		if err != nil {
			return err
		}
		authors, err := member.AuthorsByID(ctx, q, updaters(values...))
		if err != nil {
			return err
		}
		for _, val := range values {
			out.Settings = append(out.Settings, siteSetting(val, authors))
		}
		return nil
	})
	if err != nil {
		return dto.SiteSettings{}, err
	}
	for _, sec := range config.Sections() {
		out.Config = append(out.Config, dto.ConfigSection{Name: sec.Name, Reason: sec.Reason,
			Keys: append([]string{}, sec.Keys...)})
	}
	return out, nil
}

// Set is PUT /admin/settings/{key}: stores value, JSON of the key's kind, as
// the admin v from ip, and answers with the key as it now stands. Admin
// only. A key the registry lacks is not-found; a value the key does not take
// is validation-failed, and the listing's allowed text says what it takes.
func (s *Service) Set(ctx context.Context, v domain.Viewer, ip, key string, value json.RawMessage) (dto.SiteSetting, error) {
	if err := member.RequireAdmin(v, "change a site setting"); err != nil {
		return dto.SiteSetting{}, err
	}
	// The answer is read in the write's transaction, so a failure building
	// it rolls the change back rather than reporting failure for a change
	// that committed, which a caller would retry (Terva review 1587).
	var out dto.SiteSetting
	err := s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		if err := settings.SetIn(ctx, q, key, value, settings.Change{Actor: v.UserID, IP: ip, At: s.now()}); err != nil {
			return err
		}
		val, err := settings.LookupIn(ctx, q, key)
		if err != nil {
			return err
		}
		authors, err := member.AuthorsByID(ctx, q, updaters(val))
		if err != nil {
			return err
		}
		out = siteSetting(val, authors)
		return nil
	})
	switch {
	case errors.Is(err, settings.ErrUnknownKey):
		return dto.SiteSetting{}, problem.Coded(problem.CodeNotFound, err)
	case errors.Is(err, settings.ErrInvalidValue):
		return dto.SiteSetting{}, problem.Coded(problem.CodeValidationFailed, err)
	case err != nil:
		return dto.SiteSetting{}, err
	}
	return out, nil
}

// updaters is the account behind each value that one set, once each.
func updaters(values ...settings.Value) []int64 {
	seen := map[int64]bool{}
	var ids []int64
	for _, val := range values {
		if id := val.UpdatedBy; id != 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

// siteSetting is val as the screen draws it. Who last set it follows the
// row: a key with no row has neither a time nor an account, and a key the
// command line set has a time and no account.
func siteSetting(val settings.Value, authors map[int64]dto.Author) dto.SiteSetting {
	out := dto.SiteSetting{Key: val.Name, Doc: val.Doc, Kind: val.Kind, Allowed: val.Allowed,
		Choices: append([]string{}, val.Choices...), Default: json.RawMessage(val.Default),
		Value: json.RawMessage(val.JSON), IsDefault: val.IsDefault}
	if val.UpdatedAt != 0 {
		at := dto.Millis(val.UpdatedAt)
		out.UpdatedAt = &at
	}
	if val.UpdatedBy != 0 {
		a := authors[val.UpdatedBy]
		out.UpdatedBy = &a
	}
	return out
}
