-- A '--' in a string is not a comment, and hides nothing after it.
INSERT INTO audit (msg) VALUES ('--'); INSERT INTO sessions (id_hash, kind) VALUES (x'02', 'user');
