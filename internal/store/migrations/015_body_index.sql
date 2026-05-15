-- spec 35 §5 — opt-in local body index.
-- Decoded plaintext per message; populated by IndexBody from
-- render.DecodeForIndex (the same htmlToText pipeline that feeds the
-- viewer, with width-wrapping skipped). Eviction is governed by
-- [body_index] caps in CONFIG.md, NOT by the body LRU.
CREATE TABLE body_text (
    message_id        TEXT PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
    account_id        INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    folder_id         TEXT NOT NULL REFERENCES folders(id) ON DELETE CASCADE,
    content           TEXT NOT NULL,             -- decoded plaintext (post-html2text, pre-wrap)
    content_size      INTEGER NOT NULL,          -- length(content) in bytes
    indexed_at        INTEGER NOT NULL,          -- unix seconds
    last_accessed_at  INTEGER NOT NULL,          -- driven by viewer opens + index hits
    truncated         INTEGER NOT NULL DEFAULT 0 -- 1 if body exceeded [body_index].max_body_bytes
);

CREATE INDEX idx_body_text_lru     ON body_text(last_accessed_at);
CREATE INDEX idx_body_text_folder  ON body_text(folder_id);
CREATE INDEX idx_body_text_account ON body_text(account_id);

-- Token index for keyword body search. External content over body_text;
-- tokenizer matches messages_fts (`unicode61 remove_diacritics 2`).
CREATE VIRTUAL TABLE body_fts USING fts5(
    content,
    content='body_text',
    content_rowid='rowid',
    tokenize='unicode61 remove_diacritics 2'
);

-- Trigram index for substring / regex narrowing (spec 35 §3.3).
-- detail=none keeps the index smallest; offsets aren't needed because
-- the regex post-filter re-runs against body_text.content directly.
CREATE VIRTUAL TABLE body_trigram USING fts5(
    content,
    content='body_text',
    content_rowid='rowid',
    tokenize='trigram',
    detail=none
);

-- Triggers keep both FTS tables in sync with body_text.
CREATE TRIGGER body_text_ai AFTER INSERT ON body_text BEGIN
    INSERT INTO body_fts(rowid, content) VALUES (new.rowid, new.content);
    INSERT INTO body_trigram(rowid, content) VALUES (new.rowid, new.content);
END;

CREATE TRIGGER body_text_ad AFTER DELETE ON body_text BEGIN
    INSERT INTO body_fts(body_fts, rowid, content) VALUES('delete', old.rowid, old.content);
    INSERT INTO body_trigram(body_trigram, rowid, content) VALUES('delete', old.rowid, old.content);
END;

CREATE TRIGGER body_text_au AFTER UPDATE OF content ON body_text BEGIN
    INSERT INTO body_fts(body_fts, rowid, content) VALUES('delete', old.rowid, old.content);
    INSERT INTO body_trigram(body_trigram, rowid, content) VALUES('delete', old.rowid, old.content);
    INSERT INTO body_fts(rowid, content) VALUES (new.rowid, new.content);
    INSERT INTO body_trigram(rowid, content) VALUES (new.rowid, new.content);
END;

UPDATE schema_meta SET value = '15' WHERE key = 'version';
