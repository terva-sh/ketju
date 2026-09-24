-- A migration may not mint a session either.
INSERT INTO sessions (id_hash, kind) VALUES (x'00', 'user');
