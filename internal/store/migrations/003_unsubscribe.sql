-- 003_unsubscribe.sql
--
-- Spec 16: persist the parsed List-Unsubscribe action so the next
-- time the user presses U on the same message the indicator + flow
-- are a local lookup. unsubscribe_url holds:
--   - the HTTPS URL (for ActionOneClickPOST / ActionBrowserGET)
--   - "mailto:<addr>" (for ActionMailto)
--   - NULL when no actionable header is present
-- unsubscribe_one_click is 1 iff the row corresponds to an RFC 8058
-- one-click flow. The list pane uses the column to render the 🚪
-- indicator; the dispatcher uses (url, one_click) to short-circuit
-- the network fetch on subsequent presses.
--
-- Index covers the bulk-flow ("show me every message that has an
-- unsubscribe link") and the screener heuristic ("does this sender
-- usually carry an unsubscribe header?"). Partial-NULL keeps the
-- index small on accounts where most rows have no header.

ALTER TABLE messages ADD COLUMN unsubscribe_url TEXT;
ALTER TABLE messages ADD COLUMN unsubscribe_one_click INTEGER NOT NULL DEFAULT 0;
CREATE INDEX idx_messages_unsubscribe ON messages(account_id, unsubscribe_url) WHERE unsubscribe_url IS NOT NULL;

UPDATE schema_meta SET value = '3' WHERE key = 'version';
