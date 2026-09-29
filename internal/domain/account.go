package domain

import (
	"strconv"
)

// Account statuses (docs/04 "users", docs/05 "Account lifecycle").
const (
	StatusActive      = "active"
	StatusPending     = "pending"
	StatusSuspended   = "suspended"
	StatusDeactivated = "deactivated"
)

// AnonymizedHandlePrefix starts the handle an anonymized account is given,
// deleted-user-<id> (docs/05 "Account lifecycle"). Registration refuses
// every handle that starts with it (docs/04 "users"), so no other account
// can hold one. The prefix does not mark an anonymized account: the
// users.anonymized column does (TKT-01M29CKRFS, member.Anonymized).
const AnonymizedHandlePrefix = "deleted-user"

// DeletedMemberHandle is the placeholder handle for member id:
// deleted-user-<id>, what an anonymized account is renamed to, and what a
// post whose author the database lacks is drawn with (TKT-01M3KB7FXC).
func DeletedMemberHandle(id int64) string {
	return AnonymizedHandlePrefix + "-" + strconv.FormatInt(id, 10)
}

// Staff reports whether v is an active moderator or admin: the site roles
// docs/07 lets see a pending account, an account's status, and the deleted
// posts in anyone's history.
func (v Viewer) Staff() bool {
	return v.Member() && (v.Role == RoleModerator || v.Role == RoleAdmin)
}

// MaySeeAccount reports whether v is served the profile and post history of
// an account in status (docs/05 "Account lifecycle"). A pending account has
// not joined, so it is not there except to staff; a suspended or deactivated
// account's posts are kept with the handle shown, so it is served to every
// member. The caller has already required a member.
func (v Viewer) MaySeeAccount(status string) bool {
	return status != StatusPending || v.Staff()
}
