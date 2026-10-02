package users

import (
	"context"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/service/member"
)

// Resolve looks handle up for v as a profile route does (member.Account),
// at the service's clock, and returns only the error: nil when the handle
// names an account v may see, *member.Moved when a rename released it
// within domain.HandleReservation and v may see the account it names now,
// and not-found otherwise. The client shell's profile redirect asks it
// (api.ProfileRedirects, TKT-01M3YME9DM), so no handler reads the database
// handle itself.
func (s *Service) Resolve(ctx context.Context, v domain.Viewer, handle string) error {
	_, err := member.Account(ctx, s.DB, v, handle, s.now())
	return err
}
