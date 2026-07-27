-- observation PS-012 — a backup-named DB dump at a well-known path (r10 sweep).
-- DUMMY data only. Present so the backup-file signature fires on /db.sql.
CREATE TABLE users (id INT, email TEXT);
INSERT INTO users VALUES (1, 'dummy@example.test');
