-- Applications (docs/04 "applications", docs/05 "mode = approval"). An
-- applicant's own acts, submit and withdraw, are written through
-- internal/service/applications, which checks every refusal in the
-- transaction that writes.

-- ApplicantIdentity is what the application rules read about the identity a
-- pre-user session is bound to: whether it already has a user, whether it is
-- blocked, and the claims that prefill the form.
-- name: ApplicantIdentity :one
SELECT id, provider_key, email, email_verified, raw_claims_json, user_id, blocked_at
FROM identities WHERE id = ?;

-- LatestApplicationForIdentity is the identity's newest application, pending
-- or decided. The 24-hour rule counts from its created_at.
-- name: LatestApplicationForIdentity :one
SELECT id, form_id, form_version, status, created_at, decided_at
FROM applications WHERE identity_id = ?
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: InsertApplication :one
INSERT INTO applications (identity_id, form_id, form_version, answers_json, status, created_at)
VALUES (?, ?, ?, ?, 'pending', ?)
RETURNING id;

-- WithdrawApplication withdraws the identity's pending application, if it
-- has one. Zero rows means it had none.
-- name: WithdrawApplication :execrows
UPDATE applications SET status = 'withdrawn', decided_at = ?
WHERE identity_id = ? AND status = 'pending';

-- Reviewing applications (docs/05 "mode = approval" step 5, TKT-01M29CP2MG).
-- Staff decide them through internal/service/applications, whose writes
-- are each one transaction, and every write that decides is guarded in its
-- own statement, so a decision another moderator made a moment before is
-- found by the UPDATE rather than by a read that could go stale.

-- ListApplications is one page of the review list, newest first, after the
-- keyset (before_at, before_id); a null status lists every status. The
-- identity's provider, email and verified flag come with each row, null
-- once the identity was removed.
-- name: ListApplications :many
SELECT a.id, a.status, a.created_at, i.provider_key, i.email, i.email_verified
FROM applications AS a
LEFT JOIN identities AS i ON i.id = a.identity_id
WHERE (sqlc.narg(status) IS NULL OR a.status = sqlc.narg(status))
  AND (a.created_at < sqlc.arg(before_at) OR (a.created_at = sqlc.arg(before_at) AND a.id < sqlc.arg(before_id)))
ORDER BY a.created_at DESC, a.id DESC
LIMIT sqlc.arg(lim);

-- name: GetApplication :one
SELECT * FROM applications WHERE id = ?;

-- OtherApplicationsForIdentity is every other application from the same
-- identity, newest first: the prior applications the review screen shows.
-- name: OtherApplicationsForIdentity :many
SELECT a.id, a.status, a.created_at, i.provider_key, i.email, i.email_verified
FROM applications AS a
LEFT JOIN identities AS i ON i.id = a.identity_id
WHERE a.identity_id = sqlc.arg(identity_id) AND a.id <> sqlc.arg(id)
ORDER BY a.created_at DESC, a.id DESC
LIMIT 50;

-- DecideApplication approves or denies a pending application. Zero rows
-- means it was no longer pending: another moderator decided it, or the
-- applicant withdrew it. user_id is the member an approval created, set in
-- the same statement as the status, because the table's CHECK that ties
-- the two is evaluated per statement (review 1344); a denial passes null.
-- name: DecideApplication :execrows
UPDATE applications
SET status = sqlc.arg(status), decided_by = sqlc.arg(decided_by), decided_at = sqlc.arg(decided_at),
    decision_note = sqlc.narg(decision_note), user_id = sqlc.narg(user_id)
WHERE id = sqlc.arg(id) AND status = 'pending';

-- LinkIdentityToUser gives an applicant's identity the user an approval
-- created. Zero rows means the identity is gone, already has a user, or is
-- blocked.
-- name: LinkIdentityToUser :execrows
UPDATE identities SET user_id = ?, updated_at = ?
WHERE id = ? AND user_id IS NULL AND blocked_at IS NULL;

-- BlockIdentity marks an identity so its re-applications are refused
-- (docs/05 step 5). Zero rows means it is gone or has a user.
-- name: BlockIdentity :execrows
UPDATE identities SET blocked_at = ?, blocked_by = ?, updated_at = ?
WHERE id = ? AND user_id IS NULL;

-- UnblockIdentity clears the block and nothing else. Zero rows means the
-- identity is gone or was not blocked.
-- name: UnblockIdentity :execrows
UPDATE identities SET blocked_at = NULL, blocked_by = NULL, updated_at = ?
WHERE id = ? AND blocked_at IS NOT NULL;

-- name: InsertApplicationNote :one
INSERT INTO application_notes (application_id, author_id, body, created_at)
VALUES (?, ?, ?, ?)
RETURNING id;

-- name: ApplicationNotes :many
SELECT id, author_id, body, created_at FROM application_notes
WHERE application_id = ?
ORDER BY created_at, id;
