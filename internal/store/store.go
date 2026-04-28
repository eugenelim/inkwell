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

	_ "modernc.org/sqlite" // sql driver registration
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// SchemaVersion is the latest migration version this build targets.
const SchemaVersion = 1

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

	// Messages
	GetMessage(ctx context.Context, id string) (*Message, error)
	ListMessages(ctx context.Context, q MessageQuery) ([]Message, error)
	UpsertMessage(ctx context.Context, m Message) error
	UpsertMessagesBatch(ctx context.Context, ms []Message) error
	DeleteMessage(ctx context.Context, id string) error
	DeleteMessages(ctx context.Context, ids []string) error
	UpdateMessageFields(ctx context.Context, id string, f MessageFields) error

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
	UpdateActionStatus(ctx context.Context, id string, status ActionStatus, reason string) error

	// Undo
	PushUndo(ctx context.Context, e UndoEntry) error
	PopUndo(ctx context.Context) (*UndoEntry, error)
	PeekUndo(ctx context.Context) (*UndoEntry, error)
	ClearUndo(ctx context.Context) error

	// Saved searches
	ListSavedSearches(ctx context.Context, accountID int64) ([]SavedSearch, error)
	PutSavedSearch(ctx context.Context, s SavedSearch) error
	DeleteSavedSearch(ctx context.Context, id int64) error

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

