package database

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var MigrationFiles embed.FS

// MigrateUp applies all pending up migrations in sorted order.
func MigrateUp(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) error {
	// Ensure migration table exists
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version BIGINT PRIMARY KEY,
			dirty BOOLEAN NOT NULL DEFAULT FALSE,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`)
	if err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}

	entries, err := MigrationFiles.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read embedded migrations dir: %w", err)
	}

	type migrationFile struct {
		version int64
		name    string
	}

	var upMigrations []migrationFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".up.sql") {
			parts := strings.Split(name, "_")
			if len(parts) < 2 {
				continue
			}
			version, err := strconv.ParseInt(parts[0], 10, 64)
			if err != nil {
				continue
			}
			upMigrations = append(upMigrations, migrationFile{version: version, name: name})
		}
	}

	sort.Slice(upMigrations, func(i, j int) bool {
		return upMigrations[i].version < upMigrations[j].version
	})

	for _, m := range upMigrations {
		var exists bool
		err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)", m.version).Scan(&exists)
		if err != nil {
			return fmt.Errorf("failed to check migration status for version %d: %w", m.version, err)
		}

		if exists {
			continue
		}

		logger.InfoContext(ctx, "applying database migration", "version", m.version, "file", m.name)

		content, err := MigrationFiles.ReadFile(filepath.ToSlash(filepath.Join("migrations", m.name)))
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", m.name, err)
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("failed to start migration tx for %s: %w", m.name, err)
		}

		if _, err := tx.Exec(ctx, string(content)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("failed executing migration %s: %w", m.name, err)
		}

		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, dirty) VALUES ($1, FALSE)", m.version); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("failed to record migration %s: %w", m.name, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("failed committing migration %s: %w", m.name, err)
		}

		logger.InfoContext(ctx, "applied database migration successfully", "version", m.version)
	}

	return nil
}
