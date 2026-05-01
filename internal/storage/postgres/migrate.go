// Package postgres provides PostgreSQL migrations.
// Spec: TS 28.541 §5.3
package postgres

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"
)

// MigrationsFS is the embed.FS containing migration files.
//
//go:embed migrations/*.sql
var MigrationsFS embed.FS

// Migration represents a single migration file.
type Migration struct {
	Version   string
	Direction string // "up" or "down"
	Filename  string
}

// MigrationRecord represents a record in the schema_migrations table.
type MigrationRecord struct {
	Version     string
	Direction   string
	AppliedAt   time.Time
	Description string
}

// Migrator runs database migrations with tracking.
type Migrator struct {
	pool *Pool
}

// NewMigrator creates a new migrator.
func NewMigrator(pool *Pool) *Migrator {
	return &Migrator{pool: pool}
}

// EnsureSchemaMigrationsTable creates the tracking table if not exists.
func (m *Migrator) EnsureSchemaMigrationsTable(ctx context.Context) error {
	sql := `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    VARCHAR(14) NOT NULL,
			direction  VARCHAR(4)  NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			description TEXT,
			PRIMARY KEY (version, direction)
		)`

	err := m.pool.Exec(ctx, sql)
	if err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}
	return nil
}

// getAppliedMigrations returns all applied migrations.
func (m *Migrator) getAppliedMigrations(ctx context.Context) (map[string]bool, error) {
	applied := make(map[string]bool)

	if err := m.EnsureSchemaMigrationsTable(ctx); err != nil {
		return nil, err
	}

	sql := `SELECT version, direction FROM schema_migrations`
	rows, err := m.pool.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("query applied migrations: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var version, direction string
		if err := rows.Scan(&version, &direction); err != nil {
			return nil, fmt.Errorf("scan migration record: %w", err)
		}
		applied[version+"_"+direction] = true
	}

	return applied, rows.Err()
}

// listMigrations returns all available migrations sorted by version.
func (m *Migrator) listMigrations(direction string) ([]Migration, error) {
	entries, err := fs.ReadDir(MigrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}

	var migrations []Migration
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()

		parts := strings.Split(name, "_")
		if len(parts) < 2 {
			continue
		}
		version := parts[0]

		if !strings.HasSuffix(name, "."+direction+".sql") {
			continue
		}

		migrations = append(migrations, Migration{
			Version:   version,
			Direction: direction,
			Filename:  name,
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	return migrations, nil
}

// getDescription extracts description from migration filename.
func getDescription(filename string) string {
	name := strings.TrimSuffix(filename, ".up.sql")
	name = strings.TrimSuffix(name, ".down.sql")

	parts := strings.SplitN(name, "_", 2)
	if len(parts) > 1 {
		name = parts[1]
	}

	name = strings.ReplaceAll(name, "_", " ")
	return strings.TrimSpace(name)
}

// Migrate runs all pending up migrations in order.
func (m *Migrator) Migrate(ctx context.Context) error {
	if err := m.EnsureSchemaMigrationsTable(ctx); err != nil {
		return err
	}

	applied, err := m.getAppliedMigrations(ctx)
	if err != nil {
		return fmt.Errorf("get applied migrations: %w", err)
	}

	migrations, err := m.listMigrations("up")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}

	for _, mig := range migrations {
		key := mig.Version + "_up"
		if applied[key] {
			continue
		}

		data, err := MigrationsFS.ReadFile("migrations/" + mig.Filename)
		if err != nil {
			return fmt.Errorf("read %s: %w", mig.Filename, err)
		}

		tx, err := m.pool.BeginTx(ctx)
		if err != nil {
			return fmt.Errorf("begin transaction: %w", err)
		}

		if err := tx.Exec(ctx, string(data)); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", mig.Filename, err)
		}

		sql := `INSERT INTO schema_migrations (version, direction, description) VALUES ($1, $2, $3)`
		if err := tx.Exec(ctx, sql, mig.Version, "up", getDescription(mig.Filename)); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", mig.Filename, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", mig.Filename, err)
		}
	}

	return nil
}

// Rollback runs down migrations.
func (m *Migrator) Rollback(ctx context.Context, steps int) error {
	if err := m.EnsureSchemaMigrationsTable(ctx); err != nil {
		return err
	}

	applied, err := m.getAppliedMigrations(ctx)
	if err != nil {
		return fmt.Errorf("get applied migrations: %w", err)
	}

	migrations, err := m.listMigrations("up")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}

	// Reverse order for rollback
	for i, j := 0, len(migrations)-1; i < j; i, j = i+1, j-1 {
		migrations[i], migrations[j] = migrations[j], migrations[i]
	}

	rolledBack := 0
	for _, mig := range migrations {
		key := mig.Version + "_up"
		if !applied[key] {
			continue
		}

		downFile := strings.Replace(mig.Filename, ".up.sql", ".down.sql", 1)
		data, err := MigrationsFS.ReadFile("migrations/" + downFile)
		if err != nil {
			continue // Down migration might not exist
		}

		tx, err := m.pool.BeginTx(ctx)
		if err != nil {
			return fmt.Errorf("begin transaction: %w", err)
		}

		if err := tx.Exec(ctx, string(data)); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("rollback migration %s: %w", mig.Filename, err)
		}

		sql := `DELETE FROM schema_migrations WHERE version = $1 AND direction = $2`
		if err := tx.Exec(ctx, sql, mig.Version, "up"); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("remove migration record %s: %w", mig.Filename, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit rollback %s: %w", mig.Filename, err)
		}

		rolledBack++
		if rolledBack >= steps {
			break
		}
	}

	return nil
}

// Status returns migration status info.
func (m *Migrator) Status(ctx context.Context) (*MigrationStatus, error) {
	if err := m.EnsureSchemaMigrationsTable(ctx); err != nil {
		return nil, err
	}

	applied, err := m.getAppliedMigrations(ctx)
	if err != nil {
		return nil, fmt.Errorf("get applied migrations: %w", err)
	}

	upMigrations, err := m.listMigrations("up")
	if err != nil {
		return nil, fmt.Errorf("list up migrations: %w", err)
	}

	status := &MigrationStatus{
		Applied: make([]string, 0),
		Pending: make([]string, 0),
	}

	for _, mig := range upMigrations {
		key := mig.Version + "_up"
		desc := getDescription(mig.Filename)
		if applied[key] {
			status.Applied = append(status.Applied, mig.Version+"_"+desc)
		} else {
			status.Pending = append(status.Pending, mig.Version+"_"+desc)
		}
	}

	return status, nil
}

// MigrationStatus holds migration status information.
type MigrationStatus struct {
	Applied []string
	Pending []string
}
