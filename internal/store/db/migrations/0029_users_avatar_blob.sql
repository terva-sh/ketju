-- Avatars (docs/05 "Display name and bio", TKT-01M3YSQ4CR). Two reads ask
-- whether an account's avatar_blob names a blob: GET /avatars/{hash}, which
-- serves only a current avatar (AvatarForServing), and the upload bound,
-- which does not count one (UnattachedBytes). This index holds only the
-- accounts that have an avatar, so each asks it with a seek.
CREATE INDEX users_avatar_blob ON users (avatar_blob) WHERE avatar_blob IS NOT NULL;
