package postgres

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

func runMigrations(ctx context.Context, pool *pgxpool.Pool, schema dbSchema) error {
	if schema.name != defaultSchema {
		if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, schema.name)); err != nil {
			return fmt.Errorf("ensure %s schema: %w", schema.name, err)
		}
	}

	migrationsTable := schema.migrationsTable()

	if _, err := pool.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			version    INTEGER PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`, migrationsTable)); err != nil {
		return fmt.Errorf("ensure %s table: %w", migrationsTable, err)
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		files = append(files, entry.Name())
	}
	sort.Strings(files)

	for _, name := range files {
		version, err := migrationVersion(name)
		if err != nil {
			return err
		}

		var applied int
		err = pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(1) FROM %s WHERE version = $1`, migrationsTable), version).Scan(&applied)
		if err != nil {
			return fmt.Errorf("check migration %d: %w", version, err)
		}
		if applied > 0 {
			continue
		}

		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", version, err)
		}

		if _, err := tx.Exec(ctx, "SET LOCAL search_path TO "+schema.searchPath()); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("set search_path for migration %d: %w", version, err)
		}
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration %d: %w", version, err)
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf(`INSERT INTO %s (version) VALUES ($1)`, migrationsTable), version); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %d: %w", version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %d: %w", version, err)
		}
	}

	return nil
}

func migrationVersion(name string) (int, error) {
	prefix := strings.SplitN(name, "_", 2)[0]
	version, err := strconv.Atoi(prefix)
	if err != nil {
		return 0, fmt.Errorf("invalid migration filename %q: %w", name, err)
	}
	return version, nil
}
