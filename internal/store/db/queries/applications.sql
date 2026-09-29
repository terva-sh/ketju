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
