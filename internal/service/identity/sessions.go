package identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	members "github.com/terva-sh/ketju/internal/service/member"
)

// A member's own sessions, listed and ended (TKT-01M29CCC0X, docs/05
// "Sessions and CSRF"), for the settings routes. They live here rather than
// in the handlers so that no handler reads the session store itself
// (TKT-01M3YME9DM). Only listing and ending are here: a session is issued
// through the session package's issuers alone (chokepoint_test.go).
//
// The routes have already asked that the request is a member's own cookie
// session, not an agent's credential; that is request parsing, and stays
// in the handler. Each method still refuses a viewer who is not a member.

// ListSessions is GET /settings/sessions for v: their live sessions, most
// recently used first. current is the request's own session's id
// (session.Session.ID), which the list marks.
func (s *Service) ListSessions(ctx context.Context, v domain.Viewer, current string) (dto.SessionList, error) {
	if err := members.Require(v, "list sessions"); err != nil {
		return dto.SessionList{}, err
	}
	infos, err := s.Sessions.ListForUser(ctx, v.UserID)
	if err != nil {
		return dto.SessionList{}, fmt.Errorf("identity: listing the sessions of %d: %w", v.UserID, err)
	}
	out := dto.SessionList{Sessions: []dto.SessionInfo{}}
	for _, i := range infos {
		if !i.Live {
			continue
		}
		out.Sessions = append(out.Sessions, dto.SessionInfo{ID: i.ID, Current: i.ID == current,
			Device: session.DeviceLabel(i.UserAgent), UserAgent: i.UserAgent, IP: i.IP, Provider: i.Provider,
			CreatedAt: dto.Millis(i.CreatedAt.UnixMilli()), LastUsedAt: dto.Millis(i.LastUsedAt.UnixMilli())})
	}
	return out, nil
}

// EndSession is DELETE /settings/sessions/{id} for v: sign out the device
// the list names id. It reports whether a session ended. An id that is not
// one of v's live sessions, whoever's it is, ends nothing and is
// not-found.
func (s *Service) EndSession(ctx context.Context, v domain.Viewer, id string) (bool, error) {
	if err := members.Require(v, "end a session"); err != nil {
		return false, err
	}
	ended, err := s.Sessions.RevokeOwn(ctx, v.UserID, id)
	switch {
	case err != nil:
		return false, err
	case !ended:
		return false, problem.Coded(problem.CodeNotFound, errors.New("identity: the viewer has no such session"))
	}
	return true, nil
}

// EndEverySession is DELETE /settings/sessions for v: sign out everywhere,
// the session asking included (docs/05 "Sessions and CSRF", decided by the
// run 2026-10-01).
func (s *Service) EndEverySession(ctx context.Context, v domain.Viewer) error {
	if err := members.Require(v, "sign out everywhere"); err != nil {
		return err
	}
	return s.Sessions.RevokeUser(ctx, v.UserID)
}
