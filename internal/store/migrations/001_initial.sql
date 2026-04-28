-- Initial schema for the local mail cache (spec 02 §3, schema version 1).

CREATE TABLE schema_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE accounts (
    id            INTEGER PRIMARY KEY,
    tenant_id     TEXT NOT NULL,
    client_id     TEXT NOT NULL,
    upn           TEXT NOT NULL,
    display_name  TEXT,
    object_id     TEXT,
    last_signin   INTEGER,
    UNIQUE(tenant_id, upn)
);

CREATE TABLE folders (
    id                TEXT PRIMARY KEY,
    account_id        INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    parent_folder_id  TEXT REFERENCES folders(id) ON DELETE CASCADE,
    display_name      TEXT NOT NULL,
    well_known_name   TEXT,
    total_count       INTEGER NOT NULL DEFAULT 0,
    unread_count      INTEGER NOT NULL DEFAULT 0,
    is_hidden         INTEGER NOT NULL DEFAULT 0,
    last_synced_at    INTEGER
);

CREATE INDEX idx_folders_parent ON folders(parent_folder_id);
CREATE INDEX idx_folders_account ON folders(account_id);
CREATE UNIQUE INDEX idx_folders_well_known ON folders(account_id, well_known_name) WHERE well_known_name IS NOT NULL;

CREATE TABLE messages (
    id                       TEXT PRIMARY KEY,
    account_id               INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    folder_id                TEXT NOT NULL REFERENCES folders(id) ON DELETE CASCADE,
    internet_message_id      TEXT,
    conversation_id          TEXT,
    conversation_index       BLOB,
    subject                  TEXT,
    body_preview             TEXT,
    from_address             TEXT,
    from_name                TEXT,
    to_addresses             TEXT,
    cc_addresses             TEXT,
    bcc_addresses            TEXT,
    received_at              INTEGER,
    sent_at                  INTEGER,
    is_read                  INTEGER NOT NULL DEFAULT 0,
    is_draft                 INTEGER NOT NULL DEFAULT 0,
    flag_status              TEXT,
    flag_due_at              INTEGER,
    flag_completed_at        INTEGER,
    importance               TEXT,
    inference_class          TEXT,
    has_attachments          INTEGER NOT NULL DEFAULT 0,
    categories               TEXT,
    web_link                 TEXT,
    last_modified_at         INTEGER,
    cached_at                INTEGER NOT NULL,
    envelope_etag            TEXT
);

CREATE INDEX idx_messages_folder_received  ON messages(folder_id, received_at DESC);
CREATE INDEX idx_messages_conversation     ON messages(conversation_id);
CREATE INDEX idx_messages_from             ON messages(from_address);
CREATE INDEX idx_messages_received         ON messages(received_at DESC);
CREATE INDEX idx_messages_flag             ON messages(flag_status) WHERE flag_status = 'flagged';
CREATE INDEX idx_messages_unread           ON messages(folder_id, is_read) WHERE is_read = 0;

CREATE TABLE bodies (
    message_id        TEXT PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
    content_type      TEXT NOT NULL,
    content           TEXT NOT NULL,
    content_size      INTEGER NOT NULL,
    fetched_at        INTEGER NOT NULL,
    last_accessed_at  INTEGER NOT NULL
);

CREATE INDEX idx_bodies_lru ON bodies(last_accessed_at);

CREATE TABLE attachments (
    id           TEXT PRIMARY KEY,
    message_id   TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    content_type TEXT,
    size         INTEGER NOT NULL,
    is_inline    INTEGER NOT NULL DEFAULT 0,
    content_id   TEXT
);

CREATE INDEX idx_attachments_message ON attachments(message_id);

CREATE TABLE delta_tokens (
    account_id     INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    folder_id      TEXT NOT NULL REFERENCES folders(id) ON DELETE CASCADE,
    delta_link     TEXT,
    next_link      TEXT,
    last_full_sync INTEGER,
    last_delta_at  INTEGER,
    PRIMARY KEY (account_id, folder_id)
);

CREATE TABLE actions (
    id             TEXT PRIMARY KEY,
    account_id     INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    type           TEXT NOT NULL,
    message_ids    TEXT NOT NULL,
    params         TEXT,
    status         TEXT NOT NULL,
    failure_reason TEXT,
    created_at     INTEGER NOT NULL,
    started_at     INTEGER,
    completed_at   INTEGER
);

CREATE INDEX idx_actions_status ON actions(status) WHERE status IN ('pending', 'in_flight');
CREATE INDEX idx_actions_created ON actions(created_at);

CREATE TABLE undo (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    action_type  TEXT NOT NULL,
    message_ids  TEXT NOT NULL,
    params       TEXT,
    label        TEXT NOT NULL,
    created_at   INTEGER NOT NULL
);

CREATE TABLE saved_searches (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id    INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    pattern       TEXT NOT NULL,
    pinned        INTEGER NOT NULL DEFAULT 0,
    sort_order    INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL,
    UNIQUE (account_id, name)
);

CREATE VIRTUAL TABLE messages_fts USING fts5(
    subject,
    body_preview,
    from_name,
    from_address,
    content='messages',
    content_rowid='rowid',
    tokenize='unicode61 remove_diacritics 2'
);

CREATE TRIGGER messages_fts_ai AFTER INSERT ON messages BEGIN
    INSERT INTO messages_fts(rowid, subject, body_preview, from_name, from_address)
    VALUES (new.rowid, new.subject, new.body_preview, new.from_name, new.from_address);
END;

CREATE TRIGGER messages_fts_ad AFTER DELETE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, subject, body_preview, from_name, from_address)
    VALUES('delete', old.rowid, old.subject, old.body_preview, old.from_name, old.from_address);
END;

CREATE TRIGGER messages_fts_au AFTER UPDATE OF subject, body_preview, from_name, from_address ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, subject, body_preview, from_name, from_address)
    VALUES('delete', old.rowid, old.subject, old.body_preview, old.from_name, old.from_address);
    INSERT INTO messages_fts(rowid, subject, body_preview, from_name, from_address)
    VALUES (new.rowid, new.subject, new.body_preview, new.from_name, new.from_address);
END;

INSERT INTO schema_meta (key, value) VALUES ('version', '1');
