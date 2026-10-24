package postgres

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// migrationItem represents a single versioned migration script.
type migrationItem struct {
	version  int64
	name     string
	upFile   string
	downFile string
}

// Migrator manages embedded schema migrations for PostgreSQL.
type Migrator struct {
	pool       *pgxpool.Pool
	migrations []migrationItem
}

// NewMigrator creates a new Migrator instance with embedded migrations discovered.
func NewMigrator(pool *pgxpool.Pool) (*Migrator, error) {
	if pool == nil {
		return nil, errors.New("postgres migrator: pool cannot be nil")
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("postgres migrator: failed to read embedded migrations: %w", err)
	}

	versionMap := make(map[int64]*migrationItem)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		filename := entry.Name()
		if !strings.HasSuffix(filename, ".sql") {
			continue
		}

		parts := strings.SplitN(filename, "_", 2)
		if len(parts) < 2 {
			continue
		}
		version, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			continue
		}

		item, exists := versionMap[version]
		if !exists {
			item = &migrationItem{version: version}
			versionMap[version] = item
		}

		if strings.HasSuffix(filename, ".up.sql") {
			item.upFile = path.Join("migrations", filename)
			item.name = strings.TrimSuffix(parts[1], ".up.sql")
		} else if strings.HasSuffix(filename, ".down.sql") {
			item.downFile = path.Join("migrations", filename)
		}
	}

	var migrations []migrationItem
	for _, item := range versionMap {
		if item.upFile != "" {
			migrations = append(migrations, *item)
		}
	}
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})

	return &Migrator{
		pool:       pool,
		migrations: migrations,
	}, nil
}

func (m *Migrator) ensureMigrationsTable(ctx context.Context, tx pgx.Tx) error {
	query := `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version BIGINT PRIMARY KEY,
    dirty BOOLEAN NOT NULL DEFAULT FALSE,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);`
	_, err := tx.Exec(ctx, query)
	return err
}

// Version returns the current migration version and dirty flag.
// If no migrations have been applied, version 0 is returned.
func (m *Migrator) Version(ctx context.Context) (int64, bool, error) {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return 0, false, fmt.Errorf("postgres migrator: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if err := m.ensureMigrationsTable(ctx, tx); err != nil {
		return 0, false, fmt.Errorf("postgres migrator: ensure table: %w", err)
	}

	var version int64
	var dirty bool
	err = tx.QueryRow(ctx, "SELECT version, dirty FROM schema_migrations ORDER BY version DESC LIMIT 1").Scan(&version, &dirty)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("postgres migrator: query version: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, false, err
	}
	return version, dirty, nil
}

// Up applies all pending migrations in ascending version order. Each
// migration runs in its own transaction that first takes a
// transaction-scoped advisory lock on the schema's migrations and re-reads
// the applied version, so processes starting concurrently against one
// database (for example a server and a worker) apply every migration once.
func (m *Migrator) Up(ctx context.Context) error {
	for {
		applied, err := m.upNext(ctx)
		if err != nil {
			return err
		}
		if !applied {
			return nil
		}
	}
}

// upNext applies the next pending migration, if any, and reports whether it
// applied one.
func (m *Migrator) upNext(ctx context.Context) (bool, error) {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("postgres migrator: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('gargi.schema_migrations'), hashtext(COALESCE(current_schema(), '')))`); err != nil {
		return false, fmt.Errorf("postgres migrator: lock migrations: %w", err)
	}
	if err := m.ensureMigrationsTable(ctx, tx); err != nil {
		return false, fmt.Errorf("postgres migrator: ensure table: %w", err)
	}
	var currentVersion int64
	var dirty bool
	err = tx.QueryRow(ctx, "SELECT version, dirty FROM schema_migrations ORDER BY version DESC LIMIT 1").Scan(&currentVersion, &dirty)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("postgres migrator: query version: %w", err)
	}
	if dirty {
		return false, fmt.Errorf("postgres migrator: database is in dirty state at version %d; manual intervention required", currentVersion)
	}

	for _, item := range m.migrations {
		if item.version <= currentVersion {
			continue
		}

		content, err := migrationFS.ReadFile(item.upFile)
		if err != nil {
			return false, fmt.Errorf("postgres migrator: read up file %s: %w", item.upFile, err)
		}

		// Mark dirty
		_, err = tx.Exec(ctx, "INSERT INTO schema_migrations (version, dirty, applied_at) VALUES ($1, TRUE, NOW()) ON CONFLICT (version) DO UPDATE SET dirty = TRUE, applied_at = NOW()", item.version)
		if err != nil {
			return false, fmt.Errorf("postgres migrator: mark dirty for version %d: %w", item.version, err)
		}

		// Execute migration SQL
		if _, err := tx.Exec(ctx, string(content)); err != nil {
			return false, fmt.Errorf("postgres migrator: execute up migration %s: %w", item.upFile, err)
		}

		// Mark clean
		_, err = tx.Exec(ctx, "UPDATE schema_migrations SET dirty = FALSE WHERE version = $1", item.version)
		if err != nil {
			return false, fmt.Errorf("postgres migrator: clear dirty for version %d: %w", item.version, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("postgres migrator: commit tx for version %d: %w", item.version, err)
		}
		return true, nil
	}

	return false, nil
}

// Down rolls back the latest applied migration. If all is true, rolls back all applied migrations.
func (m *Migrator) Down(ctx context.Context, all bool) error {
	for {
		currentVersion, dirty, err := m.Version(ctx)
		if err != nil {
			return err
		}
		if currentVersion == 0 {
			return nil
		}
		if dirty {
			return fmt.Errorf("postgres migrator: database is in dirty state at version %d; manual intervention required", currentVersion)
		}

		var targetItem *migrationItem
		for i := range m.migrations {
			if m.migrations[i].version == currentVersion {
				targetItem = &m.migrations[i]
				break
			}
		}

		if targetItem == nil || targetItem.downFile == "" {
			return fmt.Errorf("postgres migrator: down migration not found for version %d", currentVersion)
		}

		content, err := migrationFS.ReadFile(targetItem.downFile)
		if err != nil {
			return fmt.Errorf("postgres migrator: read down file %s: %w", targetItem.downFile, err)
		}

		tx, err := m.pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("postgres migrator: begin tx for rollback version %d: %w", targetItem.version, err)
		}

		// Execute down migration SQL
		if _, err := tx.Exec(ctx, string(content)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("postgres migrator: execute down migration %s: %w", targetItem.downFile, err)
		}

		// Remove from schema_migrations
		_, err = tx.Exec(ctx, "DELETE FROM schema_migrations WHERE version = $1", targetItem.version)
		if err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("postgres migrator: delete schema_migrations for version %d: %w", targetItem.version, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("postgres migrator: commit rollback tx for version %d: %w", targetItem.version, err)
		}

		if !all {
			break
		}
	}

	return nil
}
