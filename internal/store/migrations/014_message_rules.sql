-- 014_message_rules.sql
--
-- Spec 32: local mirror of Microsoft Graph server-side message rules
-- (the /me/mailFolders/inbox/messageRules endpoint). The server is the
-- source of truth; this table is a read-side cache for offline listing
-- and diffing. Writes go to Graph synchronously and re-populate the
-- mirror on success (spec 32 §3 / §5).

CREATE TABLE message_rules (
    account_id      INTEGER NOT NULL
                            REFERENCES accounts(id) ON DELETE CASCADE,
    rule_id         TEXT    NOT NULL CHECK(length(rule_id) > 0),
    display_name    TEXT    NOT NULL,
    sequence_num    INTEGER NOT NULL CHECK(sequence_num >= 0),
    is_enabled      INTEGER NOT NULL CHECK(is_enabled    IN (0,1)),
    is_read_only    INTEGER NOT NULL CHECK(is_read_only  IN (0,1)),
    has_error       INTEGER NOT NULL CHECK(has_error     IN (0,1)),
    conditions_json TEXT    NOT NULL DEFAULT '{}',
    actions_json    TEXT    NOT NULL DEFAULT '{}',
    exceptions_json TEXT    NOT NULL DEFAULT '{}',
    last_pulled_at  INTEGER NOT NULL,
    PRIMARY KEY (account_id, rule_id)
);

CREATE INDEX idx_message_rules_sequence
    ON message_rules(account_id, sequence_num);

UPDATE schema_meta SET value = '14' WHERE key = 'version';
