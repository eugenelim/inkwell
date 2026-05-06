-- 009_mute.sql
--
-- Spec 19: local-only mute for conversation threads. No Graph API is
-- called; the muted state lives entirely in this table.
-- Composite PK (conversation_id, account_id) because Graph
-- conversationId values are tenant-local, not globally unique across
-- multiple signed-in accounts in future.

CREATE TABLE muted_conversations (
    conversation_id TEXT    NOT NULL,
    account_id      INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    muted_at        INTEGER NOT NULL,   -- unix epoch seconds
    PRIMARY KEY (conversation_id, account_id)
);
CREATE INDEX idx_muted_conv_account ON muted_conversations(account_id);

UPDATE schema_meta SET value = '9' WHERE key = 'version';
