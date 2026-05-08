-- 013_bundled_senders.sql
--
-- Spec 26: local-only per-sender bundling opt-in. No Graph API call;
-- the bundled state lives entirely in this table.
-- Composite PK (account_id, address) — matches the convention of every
-- other per-account local-only metadata table. The address is stored
-- lowercased; the UI / CLI / store all normalise before INSERT/SELECT.

CREATE TABLE bundled_senders (
    account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    address    TEXT    NOT NULL,             -- lowercased email address
    added_at   INTEGER NOT NULL,             -- unix epoch seconds
    PRIMARY KEY (account_id, address)
);
CREATE INDEX idx_bundled_senders_account ON bundled_senders(account_id);

UPDATE schema_meta SET value = '13' WHERE key = 'version';
