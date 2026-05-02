-- 007_delta_tokens_drop_folder_fk.sql
--
-- Remove the foreign-key constraint on delta_tokens.folder_id so the
-- calendar-sync engine can use the pseudo-key "__calendar__" without
-- needing a matching row in the folders table.
--
-- SQLite cannot ALTER a column to remove a constraint; the table must be
-- recreated. We do not need PRAGMA foreign_keys = OFF here because the
-- new table has NO FK on folder_id (so any value is valid), and the
-- copied rows all have folder_id values that were already valid (they
-- satisfy the old constraint), so no FK violation occurs during the copy.

CREATE TABLE delta_tokens_new (
    account_id     INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    folder_id      TEXT    NOT NULL,
    delta_link     TEXT,
    next_link      TEXT,
    last_full_sync INTEGER,
    last_delta_at  INTEGER,
    PRIMARY KEY (account_id, folder_id)
);

INSERT INTO delta_tokens_new
    SELECT account_id, folder_id, delta_link, next_link, last_full_sync, last_delta_at
    FROM delta_tokens;

DROP TABLE delta_tokens;
ALTER TABLE delta_tokens_new RENAME TO delta_tokens;

UPDATE schema_meta SET value = '7' WHERE key = 'version';
