-- 012_tab_order.sql
--
-- Spec 24 — split inbox tabs. A saved search with non-NULL tab_order
-- is promoted to the list-pane tab strip; tab_order is the 0-based
-- strip position. Order is dense (0..N-1); uniqueness is enforced
-- per account via the partial UNIQUE index. Pre-migration rows are
-- left at NULL (not a tab).

ALTER TABLE saved_searches
    ADD COLUMN tab_order INTEGER;

CREATE UNIQUE INDEX idx_saved_searches_tab_order
    ON saved_searches(account_id, tab_order)
    WHERE tab_order IS NOT NULL;

UPDATE schema_meta SET value = '12' WHERE key = 'version';
