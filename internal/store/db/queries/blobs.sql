-- The upload, attach and serve paths' queries (TKT-01M29CXK0K, docs/07
-- "Attachments"). BlobForServing, in moderation.sql, stays the serve path's
-- one lookup of a blob row (docs/04 "blobs").

-- A blob in any status, for the upload path, which must tell a quarantined
-- blob from a missing one.
-- name: GetBlob :one
SELECT * FROM blobs WHERE hash = ?;

-- A new blob has no references yet, so it is unreferenced from the moment
-- it is uploaded; the post that carries it adds the first one.
-- original_name and uploaded_by are the first uploader's, kept for an
-- operator: what a post shows is blob_uploads'.
-- name: CreateUploadedBlob :exec
INSERT INTO blobs (hash, size, mime, original_name, uploaded_by, uploaded_at, refcount, status, unreferenced_since,
	width, height)
VALUES (sqlc.arg('hash'), sqlc.arg('size'), sqlc.arg('mime'), sqlc.arg('original_name'), sqlc.arg('uploaded_by'),
	sqlc.arg('uploaded_at'), 0, 'ok', sqlc.arg('uploaded_at'), sqlc.narg('width'), sqlc.narg('height'));

-- The bytes an account has stored that no post names (TKT-01M3V73KM1,
-- docs/07 "Attachments"): its blobs with refcount 0, drafts' and those
-- waiting out their seven days included. The partial index
-- blobs_uploader_unreferenced holds exactly these rows. A blob an account's
-- avatar names is not counted (TKT-01M3YSQ4CR): users_avatar_blob answers
-- that with a seek.
-- name: UnattachedBytes :one
SELECT CAST(coalesce(sum(b.size), 0) AS INTEGER) FROM blobs b
WHERE b.uploaded_by = ? AND b.refcount = 0
  AND NOT EXISTS (SELECT 1 FROM users u WHERE u.avatar_blob = b.hash);

-- The blob GET /avatars/{hash} serves (TKT-01M3YSQ4CR): one whose status is
-- ok and that is, now, the avatar of an account that is not anonymized. A
-- replaced or cleared avatar is not found, whoever kept its hash.
-- name: AvatarForServing :one
SELECT b.* FROM blobs b
WHERE b.hash = sqlc.arg('hash') AND b.status = 'ok'
  AND EXISTS (SELECT 1 FROM users u WHERE u.avatar_blob = sqlc.arg('hash') AND u.anonymized = 0);

-- A blob for an avatar: the first one stored names its uploader, as an
-- upload's does, and is unreferenced, since no post names it. No
-- blob_uploads row is written, so an avatar is never the member's to
-- attach.
-- name: CreateAvatarBlob :exec
INSERT INTO blobs (hash, size, mime, original_name, uploaded_by, uploaded_at, refcount, status, unreferenced_since,
	width, height)
VALUES (sqlc.arg('hash'), sqlc.arg('size'), sqlc.arg('mime'), 'avatar', sqlc.arg('uploaded_by'),
	sqlc.arg('uploaded_at'), 0, 'ok', sqlc.arg('uploaded_at'), sqlc.arg('edge'), sqlc.arg('edge'));

-- An account's avatar: a blob hash, or null for none. An anonymized
-- account is left alone, as its trigger would refuse the write anyway.
-- name: SetUserAvatar :one
UPDATE users SET avatar_blob = sqlc.narg('hash'), updated_at = sqlc.arg('updated_at')
WHERE id = sqlc.arg('id') AND anonymized = 0
RETURNING avatar_blob;

-- An upload of bytes already stored restarts the collector's seven days
-- while nothing refers to them, so a blob uploaded again for a post not yet
-- written is not collected under it. A referenced blob is left alone: its
-- unreferenced_since is null, and the CHECK holds it so.
-- name: RefreshBlob :exec
UPDATE blobs SET unreferenced_since = CAST(sqlc.arg('at') AS INTEGER)
WHERE hash = sqlc.arg('hash') AND refcount = 0;

-- The first upload of a blob by a member names it for them. A later one
-- changes nothing, so a name already posted stands (Terva review 1696).
-- name: InsertBlobUpload :exec
INSERT INTO blob_uploads (hash, user_id, original_name, uploaded_at) VALUES (?, ?, ?, ?)
ON CONFLICT (hash, user_id) DO NOTHING;

-- The name a member uploaded a blob under; no row means they never did.
-- name: BlobUploadedBy :one
SELECT original_name FROM blob_uploads WHERE hash = ? AND user_id = ?;

-- What an attach needs to know of each hash for its poster: the blob's
-- status, and whether the poster uploaded it. A hash with no blob is
-- absent.
-- name: AttachCandidates :many
SELECT b.hash, b.status, EXISTS (
	SELECT 1 FROM blob_uploads u WHERE u.hash = b.hash AND u.user_id = sqlc.arg('user_id')
) AS uploaded
FROM blobs b WHERE b.hash IN (sqlc.slice('hashes'));

-- A page's attachment metadata: each blob's stored facts, and the name the
-- given author uploaded it under, if they did. A hash with no blob is
-- absent.
-- name: AttachmentMeta :many
SELECT b.hash, b.size, b.mime, b.width, b.height,
	CAST(coalesce((SELECT u.original_name FROM blob_uploads u
		WHERE u.hash = b.hash AND u.user_id = sqlc.arg('author_id')), '') AS TEXT) AS original_name
FROM blobs b WHERE b.hash IN (sqlc.slice('hashes'));

-- The posts naming a blob, a page at a time after the keyset (after_thread,
-- after_ordinal), in (thread, ordinal) order.
-- name: PostAttachmentRefs :many
SELECT thread_id, ordinal FROM post_attachments
WHERE hash = sqlc.arg('hash')
  AND (thread_id > sqlc.arg('after_thread')
       OR (thread_id = sqlc.arg('after_thread') AND ordinal > sqlc.arg('after_ordinal')))
ORDER BY thread_id, ordinal
LIMIT sqlc.arg('max');

-- A post's attachments as the projection holds them, for an edited post,
-- whose current revision's record carries none, in the Post's order.
-- name: PostAttachmentsOf :many
SELECT hash FROM post_attachments WHERE thread_id = ? AND ordinal = ? ORDER BY position;

-- name: InsertPostAttachment :exec
INSERT INTO post_attachments (thread_id, ordinal, hash, position) VALUES (?, ?, ?, ?)
ON CONFLICT DO NOTHING;

-- name: DeletePostAttachments :exec
DELETE FROM post_attachments WHERE thread_id = ? AND ordinal = ?;

-- name: ClearPostAttachments :exec
DELETE FROM post_attachments;
