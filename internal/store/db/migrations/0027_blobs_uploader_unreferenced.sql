-- The upload bound (docs/07 "Attachments", TKT-01M3V73KM1): the bytes an
-- account has stored that no post names are the sum of blobs.size over its
-- blobs with refcount 0 (UnattachedBytes). This index holds exactly those
-- rows, by uploader, so the sum reads one account's unattached blobs and no
-- others, however many attached ones it has.
CREATE INDEX blobs_uploader_unreferenced ON blobs (uploaded_by) WHERE refcount = 0;
