package users

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/forms"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/member"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// A member's own settings (TKT-01M29CCC0X, docs/07 "Resources (v1)"): GET
// and PATCH /settings. Members and agents alike may read and change them,
// because page_size changes the thread pages an agent reads. They take
// effect on the next request, which reads the users row again with its
// session or credential: nobody signs in again.

// SettingsPatch is PATCH /settings's body. Each key is kept raw so that a
// value of the wrong JSON type is a field error, not a bad request, and a
// key left out is nil and left as it is. locale is not here: nothing sets it
// yet, and the body decoder refuses a key this does not name.
type SettingsPatch struct {
	Timezone json.RawMessage `json:"timezone"`
	PageSize json.RawMessage `json:"page_size"`
	Theme    json.RawMessage `json:"theme"`
}

// Settings is GET /settings for v: their settings as the request's
// session or credential read them, as /me shows them.
func (s *Service) Settings(ctx context.Context, v domain.Viewer) (dto.Settings, error) {
	if err := member.Require(v, "read settings"); err != nil {
		return dto.Settings{}, err
	}
	u, ok := accountOf(ctx, v)
	if !ok {
		return dto.Settings{}, problem.Coded(problem.CodeUnauthenticated, errors.New("users: no account for the viewer"))
	}
	return settingsOf(u.Timezone, u.Locale, u.PrefsJSON), nil
}

// UpdateSettings is PATCH /settings: v changes the settings p names and
// keeps the rest. Every named value is checked first, and any refusal is
// validation-failed with a field error for each, from forms' codes:
// wrong_type for a value of the wrong JSON type, not_an_option for a
// timezone time.LoadLocation does not know or a theme outside Themes, and
// out_of_range for a page size outside 10 to 50. A refusal writes nothing.
//
// The write is one statement (UpdateUserSettings), which merges the named
// prefs keys into prefs_json and keeps every other key there, and answers
// the settings as stored.
func (s *Service) UpdateSettings(ctx context.Context, v domain.Viewer, p SettingsPatch) (dto.Settings, error) {
	if err := member.Require(v, "change settings"); err != nil {
		return dto.Settings{}, err
	}
	var bad []forms.FieldError
	refuse := func(field, code string) { bad = append(bad, forms.FieldError{Field: field, Code: code}) }
	var timezone sql.NullString
	prefs := map[string]any{}
	if p.Timezone != nil {
		var tz string
		switch {
		case isNull(p.Timezone) || json.Unmarshal(p.Timezone, &tz) != nil:
			refuse("timezone", forms.CodeWrongType)
		case !knownZone(tz):
			refuse("timezone", forms.CodeNotAnOption)
		default:
			timezone = sql.NullString{String: tz, Valid: true}
		}
	}
	if p.PageSize != nil {
		var n int64
		switch {
		case isNull(p.PageSize) || json.Unmarshal(p.PageSize, &n) != nil:
			refuse("page_size", forms.CodeWrongType)
		case n < minPageSize || n > maxPageSize:
			refuse("page_size", forms.CodeOutOfRange)
		default:
			prefs["page_size"] = n
		}
	}
	if p.Theme != nil {
		var t string
		switch {
		case isNull(p.Theme) || json.Unmarshal(p.Theme, &t) != nil:
			refuse("theme", forms.CodeWrongType)
		case !slices.Contains(Themes, t):
			refuse("theme", forms.CodeNotAnOption)
		default:
			prefs["theme"] = t
		}
	}
	if len(bad) > 0 {
		return dto.Settings{}, &forms.Invalid{Kind: forms.ErrInvalidAnswers, Fields: bad}
	}
	if !timezone.Valid && len(prefs) == 0 {
		return s.Settings(ctx, v)
	}
	patch, err := json.Marshal(prefs)
	if err != nil {
		return dto.Settings{}, err
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	var row dbq.UpdateUserSettingsRow
	err = s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		row, err = q.UpdateUserSettings(ctx, dbq.UpdateUserSettingsParams{Timezone: timezone, Prefs: string(patch),
			UpdatedAt: now.UnixMilli(), ID: v.UserID})
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return dto.Settings{}, problem.Coded(problem.CodeUnauthenticated, fmt.Errorf("users: no account %d", v.UserID))
	}
	if err != nil {
		return dto.Settings{}, fmt.Errorf("users: writing the settings of %d: %w", v.UserID, err)
	}
	return settingsOf(row.Timezone.String, row.Locale.String, row.PrefsJson), nil
}

// isNull is whether a key was sent as null, which is no value of any
// setting's type: json.Unmarshal would accept it and leave the zero value.
func isNull(raw json.RawMessage) bool {
	return string(bytes.TrimSpace(raw)) == "null"
}

// knownZone is whether tz names an IANA time zone. "" and "Local" are
// refused although time.LoadLocation accepts them, because each means the
// server's zone rather than the member's. The zone database is embedded in
// the binary (time/tzdata in cmd/ketju), so the answer does not depend on
// the host.
func knownZone(tz string) bool {
	if tz == "" || tz == "Local" || len(tz) > 64 {
		return false
	}
	_, err := time.LoadLocation(tz)
	return err == nil
}
