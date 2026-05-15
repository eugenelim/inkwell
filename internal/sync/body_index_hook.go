package sync

import (
	"context"
	"log/slog"

	"github.com/eugenelim/inkwell/internal/store"
)

// MaybeIndexBody is the BodyDecodedCallback the renderer fires after
// a successful body decode (cache fill via FetchBodyAsync or warm hit
// re-decoded after eviction). It is the **only** path that writes
// to body_text in the production flow; `inkwell index rebuild`
// (spec 35 §7.3) is the other ingestion site and is driven through
// the CLI, not this hook.
//
// Per `docs/CONVENTIONS.md` §7 invariant 3, this function never logs
// the message id, folder id, or content — even on error. Failures
// are captured under "body index write failed" at WARN with err only.
//
// Receiver is *engine (the concrete impl), not the Engine interface —
// keeps the hook private to package sync while letting render's
// BodyDecodedCallback type bind to it.
func (e *engine) MaybeIndexBody(ctx context.Context, m *store.Message, indexableText string) {
	if !e.opts.BodyIndexEnabled || m == nil || indexableText == "" {
		return
	}
	if !e.bodyIndexFolderAllowed(m.FolderID) {
		return
	}
	text := indexableText
	truncated := false
	if max := int(e.opts.BodyIndexMaxBodyBytes); max > 0 && len(text) > max {
		text = text[:max]
		truncated = true
	}
	entry := store.BodyIndexEntry{
		MessageID: m.ID,
		AccountID: m.AccountID,
		FolderID:  m.FolderID,
		Content:   text,
		Truncated: truncated,
	}
	if err := e.st.IndexBody(ctx, entry); err != nil {
		// Spec 35 §8.5: no message-id / folder-id / content at INFO+.
		e.logger.Warn("body index write failed", slog.String("err", err.Error()))
	}
}

// bodyIndexFolderAllowed reports whether folderID falls inside the
// configured allow-list. An empty allow-list means "all subscribed
// folders" (spec 35 §7.2). Folder allow-list entries are matched
// against the folder row's `display_name` or `well_known_name`,
// resolved at config-load time. For v1 we hold them as strings and
// compare lookup-style: an entry matches when the folder's display
// name equals it OR the folder's well-known name equals it.
//
// Folder lookups go through the store to avoid stuffing the engine
// with another cache; the resolved folder is read at most once per
// indexed message and `ListFolders` is already on the hot path for
// the sync loop.
func (e *engine) bodyIndexFolderAllowed(folderID string) bool {
	if len(e.opts.BodyIndexFolderAllowlist) == 0 {
		return true
	}
	// Cheap probe: walk the in-memory folder set. For 5–50 entries
	// this is cheaper than another DB round-trip; for larger
	// mailboxes we cache the resolved id set on first use.
	resolved := e.bodyIndexResolved()
	_, ok := resolved[folderID]
	return ok
}

// bodyIndexResolved returns the set of folder ids that match the
// configured allow-list. Cached for the engine's lifetime — folder
// ids are stable in Graph (renames keep the id), so refreshing on
// every call wastes a query.
func (e *engine) bodyIndexResolved() map[string]struct{} {
	e.bodyIndexMu.Lock()
	defer e.bodyIndexMu.Unlock()
	if e.bodyIndexResolvedSet != nil {
		return e.bodyIndexResolvedSet
	}
	set := make(map[string]struct{})
	folders, err := e.st.ListFolders(context.Background(), 0)
	if err != nil {
		// On error fall back to an empty set; the next caller will
		// retry. Not logged — folder lookup failures noise up the
		// hot path and aren't actionable here.
		return set
	}
	want := make(map[string]struct{}, len(e.opts.BodyIndexFolderAllowlist))
	for _, name := range e.opts.BodyIndexFolderAllowlist {
		want[name] = struct{}{}
	}
	for _, f := range folders {
		if _, ok := want[f.DisplayName]; ok {
			set[f.ID] = struct{}{}
			continue
		}
		if f.WellKnownName != "" {
			if _, ok := want[f.WellKnownName]; ok {
				set[f.ID] = struct{}{}
			}
		}
	}
	e.bodyIndexResolvedSet = set
	return set
}
