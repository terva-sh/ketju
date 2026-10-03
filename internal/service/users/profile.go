package users

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/forms"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/member"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// A member's own display name and bio (docs/05 "Display name and bio",
// decided by the run 2026-10-02; TKT-01M3V6XVVG): GET and PATCH
// /settings/profile. The routes are a member session's only; the handler
// refuses an agent by its credential before calling here, as it does for
// the session and identity routes. Nothing is audited: a member's edit of
// their own profile is neither moderation nor administration (docs/04
// "audit_log").
//
// The answer of every route that shows an author reads the users row again,
// so a new display name shows on the next request; the response cache's
// key holds a digest of the page's authors (threads.authorsDigest), so a
// page cached before the change is not served after it.

// BioMaxLen bounds a bio's Markdown source, in characters.
const BioMaxLen = 2000

// ProfilePatch is PATCH /settings/profile's body. Each key is kept raw, as
// SettingsPatch's are, so a value of the wrong JSON type is a field error and
// a key left out is nil and left as it is.
type ProfilePatch struct {
	DisplayName json.RawMessage `json:"display_name"`
	Bio         json.RawMessage `json:"bio"`
}

// CheckDisplayName is the one rule for a display name, a member's own
// (UpdateProfile) and an agent's (CreateAgent): trimmed, valid UTF-8, at
// most DisplayNameMaxLen characters, and no control character. It answers
// the trimmed name, or the field code of forms that refuses it. An empty
// name is allowed; what an empty name means is the caller's.
func CheckDisplayName(s string) (string, string) {
	name := strings.TrimSpace(s)
	switch {
	case !utf8.ValidString(name):
		return "", forms.CodeInvalidText
	case utf8.RuneCountInString(name) > DisplayNameMaxLen:
		return "", forms.CodeTooLong
	case strings.IndexFunc(name, unicode.IsControl) >= 0:
		return "", forms.CodeInvalidText
	}
	return name, ""
}

// checkBio is the rule for a bio: valid UTF-8, at most BioMaxLen
// characters, and no control character but tab, line feed and carriage
// return, which Markdown uses. It is stored as sent.
func checkBio(s string) string {
	switch {
	case !utf8.ValidString(s):
		return forms.CodeInvalidText
	case utf8.RuneCountInString(s) > BioMaxLen:
		return forms.CodeTooLong
	case strings.IndexFunc(s, func(r rune) bool { return unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r' }) >= 0:
		return forms.CodeInvalidText
	}
	return ""
}

// Profile is GET /settings/profile for v: their display name and their bio
// as its Markdown source.
func (s *Service) Profile(ctx context.Context, v domain.Viewer) (dto.ProfileSettings, error) {
	if err := member.Require(v, "read a profile's settings"); err != nil {
		return dto.ProfileSettings{}, err
	}
	var out dto.ProfileSettings
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		u, err := q.GetUser(ctx, v.UserID)
		if err != nil {
			return err
		}
		out = dto.ProfileSettings{DisplayName: u.DisplayName, Bio: u.BioSource, Avatar: u.AvatarBlob.String}
		return nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		return dto.ProfileSettings{}, problem.Coded(problem.CodeUnauthenticated, fmt.Errorf("users: no account %d", v.UserID))
	}
	if err != nil {
		return dto.ProfileSettings{}, err
	}
	return out, nil
}

// UpdateProfile is PATCH /settings/profile: v changes the fields p names
// and keeps the other. Every named value is checked first, and any refusal
// is validation-failed with a field error for each: wrong_type for null or a
// value that is not a string, too_long over DisplayNameMaxLen or BioMaxLen
// characters, and invalid_text for text that is not UTF-8 or has a control
// character the field does not allow. A refusal writes nothing. The write
// is one statement (UpdateUserProfile), which answers the profile as
// stored.
func (s *Service) UpdateProfile(ctx context.Context, v domain.Viewer, p ProfilePatch) (dto.ProfileSettings, error) {
	if err := member.Require(v, "change a profile"); err != nil {
		return dto.ProfileSettings{}, err
	}
	var bad []forms.FieldError
	refuse := func(field, code string) { bad = append(bad, forms.FieldError{Field: field, Code: code}) }
	var name, bio sql.NullString
	if p.DisplayName != nil {
		var raw string
		if isNull(p.DisplayName) || json.Unmarshal(p.DisplayName, &raw) != nil {
			refuse("display_name", forms.CodeWrongType)
		} else if n, code := CheckDisplayName(raw); code != "" {
			refuse("display_name", code)
		} else {
			name = sql.NullString{String: n, Valid: true}
		}
	}
	if p.Bio != nil {
		var raw string
		if isNull(p.Bio) || json.Unmarshal(p.Bio, &raw) != nil {
			refuse("bio", forms.CodeWrongType)
		} else if code := checkBio(raw); code != "" {
			refuse("bio", code)
		} else {
			bio = sql.NullString{String: raw, Valid: true}
		}
	}
	if len(bad) > 0 {
		return dto.ProfileSettings{}, &forms.Invalid{Kind: forms.ErrInvalidAnswers, Fields: bad}
	}
	if !name.Valid && !bio.Valid {
		return s.Profile(ctx, v)
	}
	var row dbq.UpdateUserProfileRow
	err := s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		row, err = q.UpdateUserProfile(ctx, dbq.UpdateUserProfileParams{DisplayName: name, BioSource: bio,
			UpdatedAt: s.now().UnixMilli(), ID: v.UserID})
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return dto.ProfileSettings{}, problem.Coded(problem.CodeUnauthenticated, fmt.Errorf("users: no account %d", v.UserID))
	}
	if err != nil {
		return dto.ProfileSettings{}, fmt.Errorf("users: writing the profile of %d: %w", v.UserID, err)
	}
	return dto.ProfileSettings{DisplayName: row.DisplayName, Bio: row.BioSource, Avatar: row.AvatarBlob.String}, nil
}
