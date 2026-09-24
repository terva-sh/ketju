/* it's a quote in a comment, which must not open a string */ INSERT INTO audit (msg) VALUES ('--'); INSERT INTO sessions (id_hash, kind) VALUES (x'03', 'user');
