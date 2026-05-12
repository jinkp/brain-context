package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrate runs all SQL migration files in order against the given pool.
// Files are executed in lexicographic order (001_, 002_, ...).
// Each file is wrapped in a transaction — if one fails the whole migration aborts.
// Already-applied migrations are tracked in the schema_migrations table.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	// Ensure tracking table exists
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename   TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	// Load applied migrations
	rows, err := pool.Query(ctx, `SELECT filename FROM schema_migrations ORDER BY filename`)
	if err != nil {
		return fmt.Errorf("query applied migrations: %w", err)
	}
	applied := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return fmt.Errorf("scan migration name: %w", err)
		}
		applied[name] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate applied migrations: %w", err)
	}

	// Collect pending migration files
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations directory: %w", err)
	}

	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	// Apply pending migrations
	pending := 0
	for _, filename := range files {
		if applied[filename] {
			continue
		}

		content, err := migrationFiles.ReadFile("migrations/" + filename)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", filename, err)
		}

		log.Printf("[migrate] applying %s", filename)

		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin transaction for %s: %w", filename, err)
		}

		if _, err := tx.Exec(ctx, string(content)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("execute migration %s: %w", filename, err)
		}

		if _, err := tx.Exec(ctx,
			`INSERT INTO schema_migrations (filename) VALUES ($1)`, filename,
		); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", filename, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", filename, err)
		}

		log.Printf("[migrate] ✅ %s applied", filename)
		pending++
	}

	if pending == 0 {
		log.Printf("[migrate] all migrations up to date (%d files)", len(files))
	} else {
		log.Printf("[migrate] applied %d migration(s)", pending)
	}

	return nil
}
