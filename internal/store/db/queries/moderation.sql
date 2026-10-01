-- A reporter's second open flag on a post adds no row: the partial unique
-- index flags_one_open_per_reporter (migration 0014) is the conflict, and
-- the insert does nothing, so it answers no row (sql.ErrNoRows) rather than
-- an id. Only that index is the conflict target, so the CHECK on "other"
-- still refuses a note-less flag, which INSERT OR IGNORE would swallow.
-- name: CreateFlag :one
INSERT INTO flags (thread_id, ordinal, reporter_id, reason, note, created_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (thread_id, ordinal, reporter_id) WHERE status = 'open' DO NOTHING
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

-- Whether a reporter has an open flag on a post: a repeat after the post
-- went down answers the same receipt as any other repeat (TKT-01M29CP2PJ,
-- Terva review 1506).
-- name: HasOpenFlag :one
SELECT EXISTS (
	SELECT 1 FROM flags WHERE thread_id = ? AND ordinal = ? AND reporter_id = ? AND status = 'open'
);

-- A post's open flags, for the auto-hide threshold (docs/08).
-- name: OpenFlagCount :one
SELECT count(*) FROM flags WHERE thread_id = ? AND ordinal = ? AND status = 'open';

-- A post's open flags, dismissed in place by a moderator's restore
-- (TKT-01M29CP2PJ). The ids come back so that the restore can reopen
-- exactly these if its append fails. audit_pending is null, except from the
-- moderator queue's restore, which owes the flags their audit row once its
-- append succeeds (migration 0015, TKT-01M29CRJ4E).
-- name: DismissOpenFlagsForPost :many
UPDATE flags SET status = 'dismissed', resolved_by = sqlc.arg(resolved_by), resolved_at = sqlc.arg(resolved_at),
	resolution_note = sqlc.arg(resolution_note), audit_pending = sqlc.narg(audit_pending)
WHERE thread_id = sqlc.arg(thread_id) AND ordinal = sqlc.arg(ordinal) AND status = 'open'
RETURNING id;

-- A post's open flags, resolved in place by a moderator who confirms an
-- auto-hide (TKT-01M29CP2PJ ConfirmHide).
-- name: ResolveOpenFlagsForPost :many
UPDATE flags SET status = 'resolved', resolved_by = sqlc.arg(resolved_by), resolved_at = sqlc.arg(resolved_at),
	resolution_note = sqlc.arg(resolution_note)
WHERE thread_id = sqlc.arg(thread_id) AND ordinal = sqlc.arg(ordinal) AND status = 'open'
RETURNING id;

-- Reopens flags a restore dismissed when its append wrote nothing. Only a
-- dismissed flag reopens, so a flag some other path closed since stays
-- closed. If a reporter has an open flag on the post again, which the
-- service's per-post lock prevents but a hand-written insert could make,
-- their newer flag stands for them, and the old one stays dismissed rather
-- than breaking flags_one_open_per_reporter and failing the whole reopen.
--
-- A reopened flag is owed no audit row, since nothing it recorded happened,
-- so its audit_pending marker goes with its resolution.
-- name: ReopenFlags :execrows
UPDATE flags SET status = 'open', resolved_by = NULL, resolved_at = NULL, resolution_note = '', audit_pending = NULL
WHERE flags.id IN (sqlc.slice(ids)) AND flags.status = 'dismissed'
  AND NOT EXISTS (
	SELECT 1 FROM flags o
	WHERE o.thread_id = flags.thread_id AND o.ordinal = flags.ordinal
	  AND o.reporter_id = flags.reporter_id AND o.status = 'open'
  );

-- Links closed flags to the audit row that records their closing, in the
-- transaction that writes the row, and clears the marker of any that were
-- owed it (TKT-01M29CRJ4E). A flag that already has a row keeps it.
-- name: SetFlagsAudit :execrows
UPDATE flags SET audit_id = sqlc.arg(audit_id), audit_pending = NULL
WHERE flags.id IN (sqlc.slice(ids)) AND flags.status <> 'open' AND flags.audit_id IS NULL;

-- A post's flags that the queue's restore dismissed and that are still owed
-- their audit row. Only the marker says a row is owed: a null audit_id alone
-- never does (Terva review 1371), since every flag closed by another path,
-- or before migration 0015, has one.
-- name: FlagsOwedAudit :many
SELECT id, CAST(resolved_by AS INTEGER) AS resolved_by, CAST(resolved_at AS INTEGER) AS resolved_at,
	resolution_note, CAST(audit_pending AS TEXT) AS owed_action
FROM flags
WHERE thread_id = ? AND ordinal = ? AND audit_pending IS NOT NULL
ORDER BY id;

-- Clears the marker of flags a failed restore left dismissed that ReopenFlags
-- did not reopen: no restore happened, so no row is owed (TKT-01M29CRJ4E).
-- name: ClearFlagsAuditPending :execrows
UPDATE flags SET audit_pending = NULL
WHERE flags.id IN (sqlc.slice(ids)) AND flags.audit_pending IS NOT NULL;

-- The posts with a flag owed its audit row since before cutoff, for
-- jobs.FlagAuditCatchUp, through the partial index flags_audit_pending.
-- name: PostsOwedFlagAudit :many
SELECT DISTINCT thread_id, ordinal FROM flags
WHERE audit_pending IS NOT NULL AND resolved_at <= sqlc.arg(cutoff)
ORDER BY thread_id, ordinal;

-- The posts the auto-hide owes a hide: at least threshold open flags, and
-- not already down in the projection. The sweep asks the index again for
-- each before it appends anything, so a projection that lags costs at most
-- one wasted look (TKT-01M29CP2PJ AutoHideSweep).
-- name: PostsAtFlagThreshold :many
SELECT f.thread_id, f.ordinal FROM flags f
JOIN post_meta p ON p.thread_id = f.thread_id AND p.ordinal = f.ordinal
WHERE f.status = 'open' AND p.deleted = 0
GROUP BY f.thread_id, f.ordinal
HAVING count(*) >= CAST(sqlc.arg(threshold) AS INTEGER)
ORDER BY f.thread_id, f.ordinal;

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

-- Whether audit_log already records the action a thread-log record made:
-- the projection writer's idempotency check, so a reindex replaying the log
-- adds no second row (TKT-01M29CKRGS). The record is named by its target,
-- its action and its log offset, which the row's details carry.
-- name: AuditExistsForRecord :one
SELECT EXISTS (
	SELECT 1 FROM audit_log
	WHERE target_type = sqlc.arg(target_type) AND target_id = sqlc.arg(target_id) AND action = sqlc.arg(action)
	  AND json_extract(details_json, '$.log_offset') = CAST(sqlc.arg(log_offset) AS INTEGER)
);

-- name: AuditLogBefore :many
SELECT * FROM audit_log WHERE id < ? ORDER BY id DESC LIMIT ?;

-- The ids of the audit rows of an action on a target, oldest first, through
-- the audit_log_target index. A suspension's hidden posts carry the id of
-- its user.suspended row, so these are the suspensions whose hidden posts
-- a restore may put back (TKT-01M29CP2QK).
-- name: AuditIDs :many
SELECT id FROM audit_log WHERE target_type = ? AND target_id = ? AND action = ? ORDER BY id;

-- The next four are what the admin audit view names its targets by, one
-- statement per kind for a whole page of rows (TKT-01M29CP2R4). A target
-- a row names but the table no longer holds is absent from the answer,
-- and the view draws it by its raw id.
--
-- AuditThreads is the title and slug of thread and post targets, and of
-- the posts flag targets are on.
-- name: AuditThreads :many
SELECT id, slug, title FROM threads WHERE id IN (sqlc.slice('ids'));

-- AuditForums is the name and slug of forum targets, and of the forums a
-- thread.moved row names.
-- name: AuditForums :many
SELECT id, slug, name FROM forums WHERE id IN (sqlc.slice('ids'));

-- AuditGroups is the name of group targets.
-- name: AuditGroups :many
SELECT id, name FROM groups WHERE id IN (sqlc.slice('ids'));

-- AuditFlags is the post each flag target is on.
-- name: AuditFlags :many
SELECT id, thread_id, ordinal FROM flags WHERE id IN (sqlc.slice('ids'));

-- The threads holding a post by the user, from the projection: every
-- thread a suspension's hide or restore has to look in (TKT-01M29CP2QK).
-- The thread's own author is taken too, since CreateThread projects its
-- first post before it answers.
-- name: ThreadsWithPostsBy :many
SELECT pm.thread_id FROM post_meta pm WHERE pm.author_id = sqlc.arg(user_id)
UNION
SELECT t.id FROM threads t WHERE t.author_id = sqlc.arg(user_id)
ORDER BY 1;

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
-- avatar and draft-source conditions are guards: nothing writes either
-- reference yet, and the delete should not be what finds out that something
-- now does. Both compare in lower case, as the log scan does, because the
-- hash is stored lower case and free text may name it in either (review
-- 1157). A draft's attachments do name blobs (TKT-01M3V73DP6) and add no
-- reference, so a blob only a draft attaches is kept for as long as the
-- draft lists it. They are stored as the attach check answers them, lower
-- case, and need no lower().
-- name: DeleteCollectableBlob :execrows
DELETE FROM blobs
WHERE blobs.hash = sqlc.arg('hash')
  AND blobs.refcount = 0
  AND blobs.unreferenced_since <= CAST(sqlc.arg('cutoff') AS INTEGER)
  AND NOT EXISTS (SELECT 1 FROM users WHERE instr(lower(users.avatar_blob), blobs.hash) > 0)
  AND NOT EXISTS (SELECT 1 FROM drafts WHERE instr(lower(drafts.source), blobs.hash) > 0)
  AND NOT EXISTS (SELECT 1 FROM drafts WHERE instr(drafts.attachments, blobs.hash) > 0);

-- A thread's slow mode (docs/04 "thread_slow_mode", TKT-01M29CRJ50). No row
-- is sql.ErrNoRows, and the site default applies.
-- name: GetThreadSlowMode :one
SELECT * FROM thread_slow_mode WHERE thread_id = ?;

-- name: PutThreadSlowMode :exec
INSERT INTO thread_slow_mode (thread_id, seconds, set_by, set_at) VALUES (?, ?, ?, ?)
ON CONFLICT (thread_id) DO UPDATE SET seconds = excluded.seconds, set_by = excluded.set_by, set_at = excluded.set_at;
