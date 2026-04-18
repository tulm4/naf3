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
)

// MigrationsFS is the embed.FS containing migration files.
//
//go:embed migrations/*.sql
var MigrationsFS embed.FS

// Migrator runs database migrations.
type Migrator struct {
	pool *Pool
}

// NewMigrator creates a new migrator.
func NewMigrator(pool *Pool) *Migrator {
	return &Migrator{pool: pool}
}

// Migrate runs all pending migrations in order.
func (m *Migrator) Migrate(ctx context.Context) error {
	entries, err := fs.ReadDir(MigrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	var filenames []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			filenames = append(filenames, e.Name())
		}
	}
	sort.Strings(filenames)

	for _, name := range filenames {
		if err := m.runFile(ctx, name); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
	}
	return nil
}

func (m *Migrator) runFile(ctx context.Context, filename string) error {
	data, err := MigrationsFS.ReadFile("migrations/" + filename)
	if err != nil {
		return fmt.Errorf("read %s: %w", filename, err)
	}

	err = m.pool.Exec(ctx, string(data))
	if err != nil {
		return fmt.Errorf("exec %s: %w", filename, err)
	}

	return nil
}
