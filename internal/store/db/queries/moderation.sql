-- name: CreateFlag :one
INSERT INTO flags (thread_id, ordinal, reporter_id, reason, note, created_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING id;

-- Resolving or dismissing a flag updates it in place; only an open flag can
-- be closed, so a second moderator's click finds nothing to change.
-- name: CloseFlag :execrows
UPDATE flags SET status = ?, resolved_by = ?, resolved_at = ?, resolution_note = ?
WHERE id = ? AND status = 'open';

-- name: GetFlag :one
SELECT * FROM flags WHERE id = ?;

-- The moderator queue: open flags, newest first (docs/08).
-- name: OpenFlags :many
SELECT * FROM flags WHERE status = 'open' ORDER BY created_at DESC, id DESC LIMIT ?;

-- A post's open flags, for the auto-hide threshold (docs/08).
-- name: OpenFlagCount :one
SELECT count(*) FROM flags WHERE thread_id = ? AND ordinal = ? AND status = 'open';

-- The only write audit_log has. Rows are never updated or deleted: the
-- triggers in migration 0006 refuse it, and TestAuditLogAppendOnly refuses a
-- query here that tries.
-- name: AppendAudit :one
INSERT INTO audit_log (at, actor_id, action, target_type, target_id, details_json, ip)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: AuditLogBefore :many
SELECT * FROM audit_log WHERE id < ? ORDER BY id DESC LIMIT ?;

-- name: CreateBlob :exec
INSERT INTO blobs (hash, size, mime, original_name, uploaded_by, uploaded_at, refcount, status, unreferenced_since)
VALUES (?, ?, ?, ?, ?, ?, 0, 'ok', ?);

-- The one lookup the serve path may use: a quarantined blob is not found.
-- name: BlobForServing :one
SELECT * FROM blobs WHERE hash = ? AND status = 'ok';

-- name: SetBlobStatus :execrows
UPDATE blobs SET status = ? WHERE hash = ?;

-- A reference gained clears unreferenced_since; one lost sets it when the
-- count reaches zero. refcount's CHECK refuses going below zero.
-- name: AddBlobRef :execrows
UPDATE blobs SET refcount = refcount + 1, unreferenced_since = NULL WHERE hash = ?;

-- name: DropBlobRef :execrows
UPDATE blobs SET refcount = refcount - 1,
	unreferenced_since = CASE WHEN refcount - 1 = 0 THEN sqlc.arg('at') ELSE NULL END
WHERE hash = sqlc.arg('hash');
