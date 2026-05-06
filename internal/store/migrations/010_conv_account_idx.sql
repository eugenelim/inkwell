-- Composite index on (account_id, conversation_id) for
-- MessageIDsInConversation to avoid a full-table scan on account_id.
CREATE INDEX IF NOT EXISTS idx_messages_conv_account
    ON messages(account_id, conversation_id);
UPDATE schema_meta SET value = '10' WHERE key = 'version';
