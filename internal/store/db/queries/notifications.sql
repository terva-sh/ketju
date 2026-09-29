-- Notifications (docs/04 "notifications"). They are written through
-- internal/service/notify, in the transaction of the act that causes them,
-- and nothing here marks one read: read_at records that the recipient read
-- it, and only the recipient does that (TKT-01M29CTY86).

-- NotifyStaffOfApplication writes one new_application row naming the
-- application for every active user with role moderator or admin (docs/05
-- "mode = approval" step 3, TKT-01M29CP2N0). Recipients are chosen by role,
-- the predicate that gates review, not by the moderators group: a group
-- member without the role cannot open the review queue. Agents with the
-- role are staff too, and are notified.
-- name: NotifyStaffOfApplication :execrows
INSERT INTO notifications (user_id, kind, application_id, at)
SELECT id, 'new_application', sqlc.arg(application_id), sqlc.arg(at)
FROM users
WHERE status = 'active' AND role IN ('moderator', 'admin');

-- NotifyApplicationDecided writes the approved applicant's
-- application_decided row, as the user the approval created, with the
-- deciding moderator as actor. A denied applicant has no user, so no row.
-- name: NotifyApplicationDecided :exec
INSERT INTO notifications (user_id, kind, application_id, actor_id, at)
VALUES (sqlc.arg(user_id), 'application_decided', sqlc.arg(application_id), sqlc.arg(actor_id), sqlc.arg(at));
