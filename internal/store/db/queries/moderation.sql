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

-- Whether audit_log already records this action on this target. Bootstrap
-- promotion reads it as its idempotency marker (TKT-01M29C800G), through the
-- audit_log_target index.
-- name: AuditExists :one
SELECT EXISTS (
	SELECT 1 FROM audit_log WHERE target_type = ? AND target_id = ? AND action = ?
);

-- name: AuditLogBefore :many
SELECT * FROM audit_log WHERE id < ? ORDER BY id DESC LIMIT ?;

-- A new blob has no references yet, so it is unreferenced from the moment
-- it is uploaded; the post that carries it adds the first one.
-- name: CreateBlob :exec
INSERT INTO blobs (hash, size, mime, original_name, uploaded_by, uploaded_at, refcount, status, unreferenced_since)
VALUES (sqlc.arg('hash'), sqlc.arg('size'), sqlc.arg('mime'), sqlc.arg('original_name'), sqlc.arg('uploaded_by'),
	sqlc.arg('uploaded_at'), 0, 'ok', sqlc.arg('uploaded_at'));

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
	unreferenced_since = CASE WHEN refcount - 1 = 0 THEN CAST(sqlc.arg('at') AS INTEGER) ELSE NULL END
WHERE hash = sqlc.arg('hash');

-- Blob GC's candidates: blobs that have sat at zero references since before
-- cutoff, oldest first, one page at a time after the keyset (after_since,
-- after_hash), so a pass reaches every candidate however many earlier ones
-- it keeps (TKT-01M29C5Y2S, review 1156). A candidate is only a candidate:
-- DeleteCollectableBlob checks every condition again as it deletes.
-- name: BlobGCCandidates :many
SELECT hash, CAST(unreferenced_since AS INTEGER) AS unreferenced_since FROM blobs
WHERE refcount = 0 AND unreferenced_since <= CAST(sqlc.arg('cutoff') AS INTEGER)
  AND (unreferenced_since > CAST(sqlc.arg('after_since') AS INTEGER)
       OR (unreferenced_since = CAST(sqlc.arg('after_since') AS INTEGER) AND hash > CAST(sqlc.arg('after_hash') AS TEXT)))
ORDER BY unreferenced_since, hash
LIMIT sqlc.arg('max');

-- Whether a blob has a row, for the collector's recovery of a removal that
-- a failed commit or a crash interrupted.
-- name: BlobExists :one
SELECT EXISTS (SELECT 1 FROM blobs WHERE hash = ?);

-- DeleteCollectableBlob deletes one blob row only if it is still
-- collectable at the moment of the delete: still at zero references, still
-- at zero since before cutoff, and named by no user's avatar and no draft.
-- A reference added after the candidates were read therefore wins. The
-- avatar and draft conditions are guards: nothing writes either reference
-- yet, and the delete should not be what finds out that something now does.
-- name: DeleteCollectableBlob :execrows
DELETE FROM blobs
WHERE blobs.hash = sqlc.arg('hash')
  AND blobs.refcount = 0
  AND blobs.unreferenced_since <= CAST(sqlc.arg('cutoff') AS INTEGER)
  AND NOT EXISTS (SELECT 1 FROM users WHERE instr(users.avatar_blob, blobs.hash) > 0)
  AND NOT EXISTS (SELECT 1 FROM drafts WHERE instr(drafts.source, blobs.hash) > 0);
