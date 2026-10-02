package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
)

// listing is a session store holding one user's sessions, for the session
// list's methods (sessions.go, TKT-01M3YME9DM). The issuers are not used:
// calling one panics through the nil embedded interface.
type listing struct {
	Sessions
	owner   int64
	infos   []session.Info
	asked   []int64 // the user ids each call named
	revoked []string
	all     bool
}

func (l *listing) ListForUser(_ context.Context, userID int64) ([]session.Info, error) {
	l.asked = append(l.asked, userID)
	if userID != l.owner {
		return nil, nil
	}
	return l.infos, nil
}

func (l *listing) RevokeOwn(_ context.Context, userID int64, id string) (bool, error) {
	l.asked = append(l.asked, userID)
	if userID != l.owner {
		return false, nil
	}
	for _, i := range l.infos {
		if i.ID == id && i.Live {
			l.revoked = append(l.revoked, id)
			return true, nil
		}
	}
	return false, nil
}

func (l *listing) RevokeUser(_ context.Context, userID int64) error {
	l.asked = append(l.asked, userID)
	l.all = userID == l.owner
	return nil
}

const firefoxUA = "Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0"

func sessionWorld() (*Service, *listing, domain.Viewer) {
	at := time.UnixMilli(1_757_500_000_000)
	l := &listing{owner: 7, infos: []session.Info{
		{ID: "laptop", Provider: "github", UserAgent: firefoxUA, IP: "192.0.2.10", CreatedAt: at, LastUsedAt: at.Add(time.Minute), Live: true},
		{ID: "stale", Provider: "github", CreatedAt: at, LastUsedAt: at},
		{ID: "phone", Provider: "club", CreatedAt: at, LastUsedAt: at, Live: true},
	}}
	return &Service{Sessions: l}, l, domain.Viewer{UserID: 7, Active: true, Role: domain.RoleMember}
}

// ListSessions lists the viewer's live sessions in the store's order, marks
// the current one, and labels the device; an expired row is left out.
func TestListSessionsIsTheViewersLiveSessions(t *testing.T) {
	s, l, v := sessionWorld()
	got, err := s.ListSessions(t.Context(), v, "phone")
	if err != nil {
		t.Fatal(err)
	}
	at := time.UnixMilli(1_757_500_000_000)
	want := []dto.SessionInfo{
		{ID: "laptop", Device: "Firefox on Linux", UserAgent: firefoxUA, IP: "192.0.2.10", Provider: "github",
			CreatedAt: dto.Millis(at.UnixMilli()), LastUsedAt: dto.Millis(at.Add(time.Minute).UnixMilli())},
		{ID: "phone", Current: true, Device: session.DeviceLabel(""), Provider: "club",
			CreatedAt: dto.Millis(at.UnixMilli()), LastUsedAt: dto.Millis(at.UnixMilli())},
	}
	if len(got.Sessions) != len(want) {
		t.Fatalf("listed %+v, want %+v", got.Sessions, want)
	}
	for i := range want {
		if got.Sessions[i] != want[i] {
			t.Errorf("session %d is %+v, want %+v", i, got.Sessions[i], want[i])
		}
	}
	if len(l.asked) != 1 || l.asked[0] != v.UserID {
		t.Errorf("asked the store for %v, want the viewer alone", l.asked)
	}

	// Nobody's sessions is an empty list, not null.
	s.Sessions = &listing{owner: 7}
	if got, err := s.ListSessions(t.Context(), v, ""); err != nil || got.Sessions == nil || len(got.Sessions) != 0 {
		t.Errorf("no sessions: %+v, %v", got, err)
	}
}

// EndSession ends one of the viewer's live sessions. Another member's id,
// an expired one and one nobody has are each not-found, and end nothing.
func TestEndSessionEndsOnlyTheViewersOwn(t *testing.T) {
	s, l, v := sessionWorld()
	if ended, err := s.EndSession(t.Context(), v, "laptop"); !ended || err != nil {
		t.Fatalf("ending the viewer's own: %v, %v", ended, err)
	}
	for _, tc := range []struct {
		name string
		v    domain.Viewer
		id   string
	}{
		{"another member asking for the viewer's session", domain.Viewer{UserID: 8, Active: true, Role: domain.RoleMember}, "phone"},
		{"an expired session", v, "stale"},
		{"an id nobody has", v, "nobody"},
	} {
		ended, err := s.EndSession(t.Context(), tc.v, tc.id)
		if ended || problem.FromError(err) != problem.CodeNotFound {
			t.Errorf("%s: %v, %v; want not-found", tc.name, ended, err)
		}
	}
	if len(l.revoked) != 1 || l.revoked[0] != "laptop" {
		t.Errorf("revoked %v, want laptop alone", l.revoked)
	}
}

// EndEverySession ends every session of the viewer's, and names nobody
// else to the store.
func TestEndEverySessionNamesTheViewer(t *testing.T) {
	s, l, v := sessionWorld()
	if err := s.EndEverySession(t.Context(), v); err != nil {
		t.Fatal(err)
	}
	if !l.all || len(l.asked) != 1 || l.asked[0] != v.UserID {
		t.Errorf("all %v, asked %v", l.all, l.asked)
	}
}

// Each method refuses a viewer who is not a member before it asks the
// store anything: anonymous is unauthenticated and a pre-user forbidden.
func TestSessionMethodsRefuseNonMembers(t *testing.T) {
	for _, tc := range []struct {
		name string
		v    domain.Viewer
		want problem.Code
	}{
		{"anonymous", domain.Viewer{}, problem.CodeUnauthenticated},
		{"a pre-user", domain.Viewer{PreUser: true}, problem.CodeForbidden},
	} {
		s, l, _ := sessionWorld()
		_, err1 := s.ListSessions(t.Context(), tc.v, "")
		_, err2 := s.EndSession(t.Context(), tc.v, "laptop")
		err3 := s.EndEverySession(t.Context(), tc.v)
		for i, err := range []error{err1, err2, err3} {
			if problem.FromError(err) != tc.want {
				t.Errorf("%s, method %d: %v, want %s", tc.name, i, err, tc.want)
			}
		}
		if len(l.asked) != 0 {
			t.Errorf("%s: the store was asked for %v", tc.name, l.asked)
		}
	}
}

// A store failure is the method's error, not a not-found.
func TestEndSessionPassesAStoreFailureOn(t *testing.T) {
	boom := errors.New("disk on fire")
	s, _, v := sessionWorld()
	s.Sessions = failing{err: boom}
	if _, err := s.EndSession(t.Context(), v, "laptop"); !errors.Is(err, boom) {
		t.Errorf("EndSession = %v, want the store's error", err)
	}
	if _, err := s.ListSessions(t.Context(), v, ""); !errors.Is(err, boom) {
		t.Errorf("ListSessions = %v, want the store's error", err)
	}
	if err := s.EndEverySession(t.Context(), v); !errors.Is(err, boom) {
		t.Errorf("EndEverySession = %v, want the store's error", err)
	}
}

// failing is a session store whose every session-list call fails.
type failing struct {
	Sessions
	err error
}

func (f failing) ListForUser(context.Context, int64) ([]session.Info, error) { return nil, f.err }

func (f failing) RevokeOwn(context.Context, int64, string) (bool, error) { return false, f.err }

func (f failing) RevokeUser(context.Context, int64) error { return f.err }
