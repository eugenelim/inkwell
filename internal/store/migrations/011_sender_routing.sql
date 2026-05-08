-- 011_sender_routing.sql
--
-- Spec 23: per-sender routing destinations (Imbox / Feed / Paper Trail /
-- Screener). Local-only; no Graph API call. Composite PK is
-- (email_address, account_id) — same column order as muted_conversations
-- (spec 19) — so a multi-account user can route the same sender to
-- different destinations on each account.
--
-- email_address is stored lowercase + trimmed (callers must normalise via
-- store.NormalizeEmail before insert / lookup). The CHECK on length
-- catches buggy callers; the Go layer rejects empty addresses with
-- ErrInvalidAddress before reaching SQLite.

CREATE TABLE sender_routing (
    email_address TEXT    NOT NULL CHECK(length(email_address) > 0),
    account_id    INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    destination   TEXT    NOT NULL CHECK(destination IN ('imbox', 'feed', 'paper_trail', 'screener')),
    added_at      INTEGER NOT NULL,
    PRIMARY KEY (email_address, account_id)
);

CREATE INDEX idx_sender_routing_account_dest
    ON sender_routing(account_id, destination);

-- Expression index on lower(trim(from_address)) so the routing JOIN
-- (lower(trim(m.from_address)) = sr.email_address) does not full-scan
-- messages on a 100k-message store. Without this the call to lower()
-- defeats idx_messages_from (which is on the raw column).
CREATE INDEX idx_messages_from_lower
    ON messages(account_id, lower(trim(from_address)));

-- Partial index on (account_id, received_at DESC) limited to
-- non-empty from_address rows. Used as the LIMIT short-circuit
-- driver for ListMessagesByRouting (spec 23 §9). The partial
-- predicate keeps the index small and prevents SQLite's planner
-- from picking it for every (account_id, ...) query — notably
-- MessageIDsInConversation, which uses idx_messages_conv_account.
-- Predicate-side compatibility with the routing query is automatic
-- because every routed sender necessarily has a non-empty from_address.
CREATE INDEX idx_messages_account_received_routed
    ON messages(account_id, received_at DESC)
    WHERE from_address IS NOT NULL AND length(trim(from_address)) > 0;

UPDATE schema_meta SET value = '11' WHERE key = 'version';
