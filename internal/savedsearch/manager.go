package savedsearch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/eugenelim/inkwell/internal/config"
	"github.com/eugenelim/inkwell/internal/pattern"
	"github.com/eugenelim/inkwell/internal/store"
)

// EvalResult is the outcome of evaluating a saved search against the local store.
type EvalResult struct {
	MessageIDs  []string
	Count       int
	EvaluatedAt time.Time
	FromCache   bool
}

// Manager manages saved searches for one account. It is safe for
// concurrent use. The store is the source of truth; the TOML mirror
// is a human-readable snapshot written after every mutation.
type Manager struct {
	st        store.Store
	accountID int64
	cfg       config.SavedSearchSettings

	mu    sync.Mutex
	cache map[string]*EvalResult // keyed by saved-search name
}

// New creates a Manager for the given account.
func New(st store.Store, accountID int64, cfg config.SavedSearchSettings) *Manager {
	return &Manager{
		st:        st,
		accountID: accountID,
		cfg:       cfg,
		cache:     make(map[string]*EvalResult),
	}
}

// List returns all saved searches for the account, pinned-first then sort_order.
// Spec 35 §9.6: each row's [store.SavedSearch.LastCompileError] is populated by
// attempting pattern.Compile under the current [body_index].enabled flag. The
// UI greys out rows with a non-empty value rather than dropping them — the
// user keeps visibility into the saved search and can fix the config to
// restore it.
func (m *Manager) List(ctx context.Context) ([]store.SavedSearch, error) {
	rows, err := m.st.ListSavedSearches(ctx, m.accountID)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		_, cerr := pattern.Compile(rows[i].Pattern, pattern.CompileOptions{
			LocalOnly:        true,
			BodyIndexEnabled: m.cfg.BodyIndexEnabled,
		})
		if cerr != nil {
			rows[i].LastCompileError = cerr.Error()
		}
	}
	return rows, nil
}

// Get retrieves one saved search by name. Returns nil, nil if not found.
func (m *Manager) Get(ctx context.Context, name string) (*store.SavedSearch, error) {
	all, err := m.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].Name == name {
			return &all[i], nil
		}
	}
	return nil, nil
}

// Save creates or updates a saved search (upserted by name). The pattern is
// compiled to validate syntax before writing. Invalidates the evaluation cache
// for this name and rewrites the TOML mirror.
func (m *Manager) Save(ctx context.Context, ss store.SavedSearch) error {
	ss.AccountID = m.accountID
	if _, err := pattern.Compile(ss.Pattern, pattern.CompileOptions{LocalOnly: true, BodyIndexEnabled: m.cfg.BodyIndexEnabled}); err != nil {
		return fmt.Errorf("invalid pattern: %w", err)
	}
	if err := m.st.PutSavedSearch(ctx, ss); err != nil {
		return err
	}
	m.invalidate(ss.Name)
	_ = m.writeTOMLMirror(ctx)
	return nil
}

// Delete removes a saved search by ID. Invalidates the full evaluation cache,
// reindexes the spec 24 tab strip (so deleting a tabbed saved search leaves
// no hole in the dense ordering), and rewrites the TOML mirror.
func (m *Manager) Delete(ctx context.Context, id int64) error {
	if err := m.st.DeleteSavedSearch(ctx, id); err != nil {
		return err
	}
	if err := m.st.ReindexTabs(ctx, m.accountID); err != nil {
		return fmt.Errorf("delete: reindex tabs: %w", err)
	}
	m.invalidateAll()
	_ = m.writeTOMLMirror(ctx)
	return nil
}

// DeleteByName removes the saved search matching name for this account.
// Returns an error if none is found. Uses an atomic single-SQL delete
// (store.DeleteSavedSearchByName) to avoid a fetch-then-delete race.
func (m *Manager) DeleteByName(ctx context.Context, name string) error {
	ss, err := m.Get(ctx, name)
	if err != nil {
		return err
	}
	if ss == nil {
		return fmt.Errorf("saved search %q not found", name)
	}
	if err := m.st.DeleteSavedSearchByName(ctx, m.accountID, name); err != nil {
		return err
	}
	if err := m.st.ReindexTabs(ctx, m.accountID); err != nil {
		return fmt.Errorf("delete %q: reindex tabs: %w", name, err)
	}
	m.invalidateAll()
	_ = m.writeTOMLMirror(ctx)
	return nil
}

// Evaluate runs the named saved search against the local store and caches the
// result. If force is false and a fresh cached result exists, it is returned
// without re-querying.
func (m *Manager) Evaluate(ctx context.Context, name string, force bool) (*EvalResult, error) {
	m.mu.Lock()
	if !force {
		if r, ok := m.cache[name]; ok && time.Since(r.EvaluatedAt) < m.cfg.CacheTTL {
			m.mu.Unlock()
			r2 := *r
			r2.FromCache = true
			return &r2, nil
		}
	}
	m.mu.Unlock()

	ss, err := m.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if ss == nil {
		return nil, fmt.Errorf("saved search %q not found", name)
	}

	compiled, err := pattern.Compile(ss.Pattern, pattern.CompileOptions{LocalOnly: true, BodyIndexEnabled: m.cfg.BodyIndexEnabled})
	if err != nil {
		return nil, fmt.Errorf("compile %q: %w", name, err)
	}
	ids, err := pattern.Execute(ctx, compiled, m.st, nil, pattern.ExecuteOptions{
		AccountID:       m.accountID,
		LocalMatchLimit: 5000,
	})
	if err != nil {
		return nil, fmt.Errorf("execute %q: %w", name, err)
	}

	r := &EvalResult{
		MessageIDs:  ids,
		Count:       len(ids),
		EvaluatedAt: time.Now(),
	}
	m.mu.Lock()
	m.cache[name] = r
	m.mu.Unlock()
	return r, nil
}

// CountPinned evaluates all pinned searches (using the cache when fresh) and
// returns a map of id → count. Errors per-search are silently skipped so a
// single bad pattern doesn't suppress the rest.
func (m *Manager) CountPinned(ctx context.Context) (map[int64]int, error) {
	searches, err := m.List(ctx)
	if err != nil {
		return nil, err
	}
	counts := make(map[int64]int, len(searches))
	for _, ss := range searches {
		if !ss.Pinned {
			continue
		}
		r, err := m.Evaluate(ctx, ss.Name, false)
		if err != nil {
			continue
		}
		counts[ss.ID] = r.Count
	}
	return counts, nil
}

// SeedDefaults populates the three first-launch saved searches defined in
// spec 11 §7.3. Is a no-op if the table already has any entries.
// upn is the active account's user principal name (used in "From me").
func (m *Manager) SeedDefaults(ctx context.Context, upn string) error {
	existing, err := m.List(ctx)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil
	}
	seeds := []store.SavedSearch{
		{AccountID: m.accountID, Name: "Unread", Pattern: "~N", Pinned: true, SortOrder: 1},
		{AccountID: m.accountID, Name: "Flagged", Pattern: "~F", Pinned: true, SortOrder: 2},
		{AccountID: m.accountID, Name: "From me", Pattern: "~f " + upn, Pinned: false, SortOrder: 3},
	}
	for _, s := range seeds {
		s.CreatedAt = time.Now()
		if err := m.st.PutSavedSearch(ctx, s); err != nil {
			return err
		}
	}
	_ = m.writeTOMLMirror(ctx)
	return nil
}

// Edit atomically updates an existing saved search. If newName ≠ originalName
// the old row is deleted and a new one is inserted with the new name.
// The cache is invalidated for both names so the next Evaluate re-queries.
func (m *Manager) Edit(ctx context.Context, originalName, newName, pat string, pinned bool) error {
	if _, err := pattern.Compile(pat, pattern.CompileOptions{LocalOnly: true, BodyIndexEnabled: m.cfg.BodyIndexEnabled}); err != nil {
		return fmt.Errorf("invalid pattern: %w", err)
	}
	if originalName == newName {
		// Name unchanged — update in place.
		old, err := m.Get(ctx, originalName)
		if err != nil {
			return err
		}
		if old == nil {
			return fmt.Errorf("saved search %q not found", originalName)
		}
		updated := *old
		updated.Pattern = pat
		updated.Pinned = pinned
		if err := m.st.PutSavedSearch(ctx, updated); err != nil {
			return err
		}
		m.invalidate(originalName)
		_ = m.writeTOMLMirror(ctx)
		return nil
	}
	// Name changed — delete old, create new.
	old, err := m.Get(ctx, originalName)
	if err != nil {
		return err
	}
	if old == nil {
		return fmt.Errorf("saved search %q not found", originalName)
	}
	if err := m.st.DeleteSavedSearchByName(ctx, m.accountID, originalName); err != nil {
		return err
	}
	newSS := store.SavedSearch{
		AccountID: m.accountID,
		Name:      newName,
		Pattern:   pat,
		Pinned:    pinned,
		SortOrder: old.SortOrder,
		CreatedAt: old.CreatedAt,
	}
	if err := m.st.PutSavedSearch(ctx, newSS); err != nil {
		return err
	}
	m.invalidate(originalName)
	m.invalidate(newName)
	_ = m.writeTOMLMirror(ctx)
	return nil
}

// EvaluatePattern compiles and executes patternSrc against the local store,
// returning the count of matching messages. Used by the edit modal to dry-run
// a pattern before committing.
func (m *Manager) EvaluatePattern(ctx context.Context, patternSrc string) (int, error) {
	compiled, err := pattern.Compile(patternSrc, pattern.CompileOptions{LocalOnly: true, BodyIndexEnabled: m.cfg.BodyIndexEnabled})
	if err != nil {
		return 0, fmt.Errorf("invalid pattern: %w", err)
	}
	ids, err := pattern.Execute(ctx, compiled, m.st, nil, pattern.ExecuteOptions{
		AccountID:       m.accountID,
		LocalMatchLimit: 5000,
	})
	if err != nil {
		return 0, err
	}
	return len(ids), nil
}

// InvalidateCache discards all cached evaluation results so the next
// Evaluate call re-queries the store. Called by the UI after a sync
// event that could affect match counts.
func (m *Manager) InvalidateCache() {
	m.invalidateAll()
}

func (m *Manager) invalidate(name string) {
	m.mu.Lock()
	delete(m.cache, name)
	m.mu.Unlock()
}

func (m *Manager) invalidateAll() {
	m.mu.Lock()
	m.cache = make(map[string]*EvalResult)
	m.mu.Unlock()
}

// writeTOMLMirror writes a human-readable snapshot of all saved searches to
// the configured mirror path. Errors are non-fatal (logged by caller).
func (m *Manager) writeTOMLMirror(ctx context.Context) error {
	if m.cfg.TOMLMirrorPath == "" {
		return nil
	}
	searches, err := m.List(ctx)
	if err != nil {
		return err
	}
	path := expandHome(m.cfg.TOMLMirrorPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	var sb strings.Builder
	for _, s := range searches {
		sb.WriteString("[[search]]\n")
		sb.WriteString("name = " + tomlQuote(s.Name) + "\n")
		sb.WriteString("pattern = " + tomlLiteralOrBasicQuote(s.Pattern) + "\n")
		if s.Pinned {
			sb.WriteString("pinned = true\n")
		} else {
			sb.WriteString("pinned = false\n")
		}
		sb.WriteString("sort_order = " + strconv.Itoa(s.SortOrder) + "\n")
		if s.TabOrder != nil {
			sb.WriteString("tab_order = " + strconv.Itoa(*s.TabOrder) + "\n")
		}
		sb.WriteString("\n")
	}
	// #nosec G306 — mode 0600: mail.db is 0600; mirror is same sensitivity.
	return os.WriteFile(path, []byte(sb.String()), 0o600)
}

func expandHome(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, p[1:])
}

func tomlQuote(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}

// tomlLiteralOrBasicQuote uses a TOML literal string (single-quoted) when
// the value contains no single-quotes; otherwise falls back to basic quoting.
func tomlLiteralOrBasicQuote(s string) string {
	if !strings.Contains(s, "'") {
		return "'" + s + "'"
	}
	return tomlQuote(s)
}
