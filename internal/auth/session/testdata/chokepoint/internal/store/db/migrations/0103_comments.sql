-- Not doors: sessions written about, in comments of both kinds.
-- INSERT INTO sessions (id_hash) VALUES (x'04');
/* UPDATE sessions SET user_id = 1;
   INSERT INTO sessions (id_hash) VALUES (x'05'); */
INSERT INTO audit (msg) VALUES ('INSERT INTO sessions is only text here');
