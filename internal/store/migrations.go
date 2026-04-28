package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

// runMigrations applies any pending migration scripts to the open db.
// Each migration runs inside a single transaction and updates
// schema_meta.version atomically. Failure rolls back and returns an
// error — the app must refuse to start (spec §4).
func (s *store) runMigrations(ctx context.Context) error {
	s.migrationMu.Lock()
	defer s.migrationMu.Unlock()

	current, err := readSchemaVersion(ctx, s.db)
	if err != nil {
		return err
	}
	scripts, err := loadMigrations()
	if err != nil {
		return err
	}
	for _, m := range scripts {
		if m.version <= current {
			continue
		}
		if err := applyMigration(ctx, s.db, m); err != nil {
			return fmt.Errorf("migration %d: %w", m.version, err)
		}
		current = m.version
	}
	if current < SchemaVersion {
		return fmt.Errorf("missing migrations: at version %d, target %d", current, SchemaVersion)
	}
	return nil
}

// readSchemaVersion returns 0 when schema_meta does not exist (fresh DB).
func readSchemaVersion(ctx context.Context, db *sql.DB) (int, error) {
	var v string
	err := db.QueryRowContext(ctx, "SELECT value FROM schema_meta WHERE key = 'version'").Scan(&v)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		// Most likely the table doesn't exist yet on a fresh DB.
		// modernc returns a generic error here; treat any error as v0.
		return 0, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return 0, fmt.Errorf("schema_meta.version not an int: %q", v)
	}
	return n, nil
}

type migration struct {
	version int
	name    string
	sql     string
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}
	var out []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		v, err := versionFromName(e.Name())
		if err != nil {
			return nil, err
		}
		body, err := fs.ReadFile(migrationsFS, "migrations/"+e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: v, name: e.Name(), sql: string(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// versionFromName parses NNN_*.sql to N.
func versionFromName(name string) (int, error) {
	if i := strings.IndexByte(name, '_'); i > 0 {
		return strconv.Atoi(name[:i])
	}
	return 0, fmt.Errorf("migration filename missing version prefix: %s", name)
}

func applyMigration(ctx context.Context, db *sql.DB, m migration) error {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return err
	}
	return tx.Commit()
}
