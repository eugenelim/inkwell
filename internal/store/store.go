package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	_ "modernc.org/sqlite" // sql driver registration
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// SchemaVersion is the latest migration version this build targets.
const SchemaVersion = 10

// ErrNotFound is returned by Get* methods when no matching row exists.
var ErrNotFound = errors.New("store: not found")

// Store is the public API of the local cache. It is goroutine-safe.
type Store interface {
	// Account
	GetAccount(ctx context.Context) (*Account, error)
	PutAccount(ctx context.Context, a Account) (int64, error)

	// Folders
	ListFolders(ctx context.Context, accountID int64) ([]Folder, error)
	GetFolderByWellKnown(ctx context.Context, accountID int64, name string) (*Folder, error)
	UpsertFolder(ctx context.Context, f Folder) error
	DeleteFolder(ctx context.Context, id string) error
	UpdateFolderDisplayName(ctx context.Context, id, displayName string) error
	// AdjustFolderCounts applies signed deltas to folders.total_count
	// and folders.unread_count atomically (clamped at 0 — never goes
	// negative). Used by the optimistic-apply path in
	// `internal/action` so the sidebar's folder counts move at TUI
	// speed; the next sync cycle's UpsertFolder call rewrites the
	// authoritative value from Graph and any drift heals itself.
	// No-op when the row doesn't exist locally (caller doesn't need
	// to special-case unsynced destination folders).
	AdjustFolderCounts(ctx context.Context, folderID string, totalDelta, unreadDelta int) error

	// Messages
	GetMessage(ctx context.Context, id string) (*Message, error)
	ListMessages(ctx context.Context, q MessageQuery) ([]Message, error)
	UpsertMessage(ctx context.Context, m Message) error
	UpsertMessagesBatch(ctx context.Context, ms []Message) error
	DeleteMessage(ctx context.Context, id string) error
	DeleteMessages(ctx context.Context, ids []string) error
	UpdateMessageFields(ctx context.Context, id string, f MessageFields) error
	// SetUnsubscribe persists the parsed List-Unsubscribe action on the
	// message row (spec 16). url is "" / "https://…" / "mailto:<addr>";
	// oneClick is true iff RFC 8058 one-click POST applies. Idempotent.
	SetUnsubscribe(ctx context.Context, messageID, url string, oneClick bool) error
	// SearchByPredicate runs a caller-supplied SQL WHERE clause + args
	// against the messages table (spec 10 — pattern-based filter).
	// `where` is appended to a fixed `account_id = ?` filter; the
	// caller is responsible for parameterising values to avoid
	// injection. Used by the UI's :filter command (spec 10).
	SearchByPredicate(ctx context.Context, accountID int64, where string, args []any, limit int) ([]Message, error)

	// Bodies
	GetBody(ctx context.Context, messageID string) (*Body, error)
	PutBody(ctx context.Context, b Body) error
	TouchBody(ctx context.Context, messageID string) error
	EvictBodies(ctx context.Context, maxCount int, maxBytes int64) (evicted int, err error)

	// Attachments
	ListAttachments(ctx context.Context, messageID string) ([]Attachment, error)
	UpsertAttachments(ctx context.Context, atts []Attachment) error

	// Delta tokens
	GetDeltaToken(ctx context.Context, accountID int64, folderID string) (*DeltaToken, error)
	PutDeltaToken(ctx context.Context, t DeltaToken) error
	ClearDeltaToken(ctx context.Context, accountID int64, folderID string) error

	// Actions
	EnqueueAction(ctx context.Context, a Action) error
	PendingActions(ctx context.Context) ([]Action, error)
	// ListActionsByType returns every action of the supplied type
	// regardless of status (oldest first). PR 7-ii's crash-recovery
	// path uses this on startup to find Pending / InFlight
	// CreateDraftReply rows that need stage-aware resume; tests use
	// it to inspect Failed rows (which PendingActions excludes).
	ListActionsByType(ctx context.Context, t ActionType) ([]Action, error)
	UpdateActionStatus(ctx context.Context, id string, status ActionStatus, reason string) error
	// UpdateActionParams replaces an action's Params blob. Used by
	// two-stage actions (e.g. ActionCreateDraftReply) that need to
	// record intermediate state — the draft id returned by
	// /me/messages/{src}/createReply — so a crashed second stage
	// can resume idempotently rather than create a duplicate draft.
	UpdateActionParams(ctx context.Context, id string, params map[string]any) error
	// SweepDoneActions deletes done / failed actions older than `before`.
	// Returns rowsAffected for telemetry. Spec 02 §8.
	SweepDoneActions(ctx context.Context, before time.Time) (int64, error)

	// Undo
	PushUndo(ctx context.Context, e UndoEntry) error
	PopUndo(ctx context.Context) (*UndoEntry, error)
	PeekUndo(ctx context.Context) (*UndoEntry, error)
	ClearUndo(ctx context.Context) error

	// Calendar events (spec 12)
	PutEvent(ctx context.Context, e Event) error
	PutEvents(ctx context.Context, events []Event) error
	ListEvents(ctx context.Context, q EventQuery) ([]Event, error)
	DeleteEventsBefore(ctx context.Context, accountID int64, before time.Time) error
	// DeleteEvent removes a single event by Graph ID (used by the
	// delta-sync @removed path in calendar_sync.go).
	DeleteEvent(ctx context.Context, id string) error
	// PutEventAttendees replaces all attendees for the given eventID
	// in one transaction (migration 006 / spec 12 §3). Called when
	// the detail modal loads a live GetEvent result.
	PutEventAttendees(ctx context.Context, eventID string, attendees []EventAttendee) error
	// ListEventAttendees returns the cached attendees for eventID.
	// Returns nil (not an error) when none are cached yet.
	ListEventAttendees(ctx context.Context, eventID string) ([]EventAttendee, error)

	// Muted conversations (spec 19 — local only, no Graph call).
	// MuteConversation is idempotent; UnmuteConversation is a no-op if not muted.
	MuteConversation(ctx context.Context, accountID int64, conversationID string) error
	UnmuteConversation(ctx context.Context, accountID int64, conversationID string) error
	IsConversationMuted(ctx context.Context, accountID int64, conversationID string) (bool, error)
	ListMutedMessages(ctx context.Context, accountID int64, limit int) ([]Message, error)
	CountMutedConversations(ctx context.Context, accountID int64) (int, error)

	// MessageIDsInConversation returns IDs of all messages in a conversation
	// for the account. When includeAllFolders is false, messages in
	// Drafts, Deleted Items, and Junk are excluded (TUI default). When
	// includeAllFolders is true the folder exclusion is skipped (CLI path).
	// Returns nil, nil when conversationID is empty.
	MessageIDsInConversation(ctx context.Context, accountID int64, conversationID string, includeAllFolders bool) ([]string, error)

	// Saved searches
	ListSavedSearches(ctx context.Context, accountID int64) ([]SavedSearch, error)
	PutSavedSearch(ctx context.Context, s SavedSearch) error
	DeleteSavedSearch(ctx context.Context, id int64) error
	DeleteSavedSearchByName(ctx context.Context, accountID int64, name string) error

	// Compose sessions (spec 15 §7 / PR 7-ii crash recovery).
	// PutComposeSession upserts a session row keyed by SessionID;
	// callers use it both to create the row on compose entry and
	// to rewrite the snapshot on focus change. ConfirmComposeSession
	// stamps confirmed_at so the resume scan ignores the row.
	// ListUnconfirmedComposeSessions returns rows with NULL
	// confirmed_at ordered by created_at DESC so the resume modal
	// offers the most-recent crashed draft first.
	// GCConfirmedComposeSessions deletes rows whose confirmed_at is
	// before the supplied cutoff, run on launch with cutoff = now-24h.
	PutComposeSession(ctx context.Context, s ComposeSession) error
	ConfirmComposeSession(ctx context.Context, sessionID string) error
	ListUnconfirmedComposeSessions(ctx context.Context) ([]ComposeSession, error)
	GCConfirmedComposeSessions(ctx context.Context, before time.Time) (int64, error)

	// FTS search
	Search(ctx context.Context, q SearchQuery) ([]MessageMatch, error)

	// Lifecycle
	Close() error
	Vacuum(ctx context.Context) error
}

// Options configure [Open].
type Options struct {
	// MmapSizeBytes maps the SQLite page cache via mmap. Default 256MB.
	MmapSizeBytes int64
	// CacheSizeKB sets the SQLite page cache budget in KB. Default 64MB.
	CacheSizeKB int
}

// DefaultOptions returns the spec §2 defaults.
func DefaultOptions() Options {
	return Options{
		MmapSizeBytes: 256 * 1024 * 1024,
		CacheSizeKB:   65536,
	}
}

// Open creates or opens the SQLite cache file at path with mode 0600,
// applies PRAGMAs, runs pending migrations transactionally, and returns
// a goroutine-safe [Store] handle.
func Open(path string, opts Options) (Store, error) {
	if opts.MmapSizeBytes <= 0 {
		opts.MmapSizeBytes = DefaultOptions().MmapSizeBytes
	}
	if opts.CacheSizeKB <= 0 {
		opts.CacheSizeKB = DefaultOptions().CacheSizeKB
	}

	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("store: mkdir %s: %w", dir, err)
		}
	}

	created := false
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		// #nosec G304 — path is the user's mail.db location (default ~/Library/Application Support/inkwell/mail.db, configurable via [storage] db_path). Single-user desktop tool; the user owns the path.
		f, ferr := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
		if ferr != nil {
			return nil, fmt.Errorf("store: create %s: %w", path, ferr)
		}
		_ = f.Close()
		created = true
	}
	// Always tighten file mode in case it drifted.
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, fmt.Errorf("store: chmod %s: %w", path, err)
	}

	// _txlock=immediate forces every transaction to BEGIN IMMEDIATE
	// (spec §6) so writers contend up-front rather than discovering the
	// conflict at upgrade time. Per-connection pragmas (busy_timeout,
	// foreign_keys) MUST go in the DSN so every pooled connection picks
	// them up, not just whichever connection ran the boot-time PRAGMA.
	dsn := path + "?_txlock=immediate" +
		"&_pragma=busy_timeout%3d5000" +
		"&_pragma=foreign_keys%3don" +
		"&_pragma=journal_mode%3dwal" +
		"&_pragma=synchronous%3dnormal" +
		"&_pragma=temp_store%3dmemory" +
		"&_pragma=mmap_size%3d" + strconv.FormatInt(opts.MmapSizeBytes, 10) +
		"&_pragma=cache_size%3d-" + strconv.Itoa(opts.CacheSizeKB)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: sql.Open: %w", err)
	}
	// modernc/sqlite serialises writes per connection in WAL; tune the
	// pool to NumCPU capped at 8 (spec §6).
	maxConns := 8
	db.SetMaxOpenConns(maxConns)
	db.SetMaxIdleConns(maxConns)

	s := &store{db: db, path: path, opts: opts, created: created}
	if err := s.runMigrations(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: migrations: %w", err)
	}
	// Spec §3.9: undo is session-scoped — clear on every open.
	if _, err := db.ExecContext(context.Background(), "DELETE FROM undo"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: clear undo: %w", err)
	}
	return s, nil
}

type store struct {
	db      *sql.DB
	path    string
	opts    Options
	created bool

	// migrationMu serialises the migration pass; runtime concurrency is
	// delegated to SQLite WAL.
	migrationMu sync.Mutex
}

func (s *store) Close() error { return s.db.Close() }

func (s *store) Vacuum(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "VACUUM")
	return err
}
