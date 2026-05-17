package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

const migrationDir = "migrations"

type migration struct {
	version       int
	name          string
	sql           string
	noTransaction bool
}

func migrateSQLite(ctx context.Context, db *sql.DB) error {
	migrations, err := loadMigrations(migrationFS, migrationDir)
	if err != nil {
		return err
	}
	if err := ensureMigrationTable(ctx, db); err != nil {
		return err
	}
	applied, err := appliedMigrationVersions(ctx, db)
	if err != nil {
		return err
	}
	for _, migration := range migrations {
		if applied[migration.version] {
			continue
		}
		if err := applyMigration(ctx, db, migration); err != nil {
			return err
		}
	}
	return nil
}

func loadMigrations(fsys fs.FS, dir string) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	migrations := make([]migration, 0, len(entries))
	seen := make(map[int]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		name := path.Join(dir, entry.Name())
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}
		migration, err := parseMigration(name, data)
		if err != nil {
			return nil, err
		}
		if previous := seen[migration.version]; previous != "" {
			return nil, fmt.Errorf("duplicate migration version %d in %s and %s", migration.version, previous, name)
		}
		seen[migration.version] = name
		migrations = append(migrations, migration)
	}
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})
	return migrations, nil
}

func parseMigration(name string, data []byte) (migration, error) {
	version, err := migrationVersion(name)
	if err != nil {
		return migration{}, err
	}
	lines := strings.Split(string(data), "\n")
	inUp := false
	var up []string
	noTransaction := false
	for _, line := range lines {
		directive := gooseDirective(line)
		switch directive {
		case "up":
			inUp = true
			continue
		case "down":
			if inUp {
				inUp = false
				continue
			}
		case "no transaction":
			if inUp {
				noTransaction = true
				continue
			}
		}
		if inUp {
			up = append(up, line)
		}
	}
	sql := strings.TrimSpace(strings.Join(up, "\n"))
	if sql == "" {
		return migration{}, fmt.Errorf("migration %s has no up SQL", name)
	}
	return migration{version: version, name: name, sql: sql, noTransaction: noTransaction}, nil
}

func migrationVersion(name string) (int, error) {
	base := path.Base(name)
	end := 0
	for end < len(base) && base[end] >= '0' && base[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, fmt.Errorf("migration %s has no numeric prefix", name)
	}
	version, err := strconv.Atoi(base[:end])
	if err != nil || version <= 0 {
		return 0, fmt.Errorf("migration %s has invalid version", name)
	}
	return version, nil
}

func gooseDirective(line string) string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "-- +goose ") {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "-- +goose ")))
}

func ensureMigrationTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS goose_db_version (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		version_id INTEGER NOT NULL,
		is_applied INTEGER NOT NULL,
		tstamp TIMESTAMP DEFAULT (datetime('now'))
	)`)
	if err != nil {
		return fmt.Errorf("ensure migration table: %w", err)
	}
	return nil
}

func appliedMigrationVersions(ctx context.Context, db *sql.DB) (map[int]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT version_id, is_applied FROM goose_db_version ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list applied migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[int]bool)
	for rows.Next() {
		var version int
		var isApplied bool
		if err := rows.Scan(&version, &isApplied); err != nil {
			return nil, fmt.Errorf("scan applied migration: %w", err)
		}
		applied[version] = isApplied
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read applied migrations: %w", err)
	}
	return applied, nil
}

func applyMigration(ctx context.Context, db *sql.DB, migration migration) error {
	if migration.noTransaction {
		if _, err := db.ExecContext(ctx, migration.sql); err != nil {
			return fmt.Errorf("apply migration %s: %w", migration.name, err)
		}
		if err := recordMigrationVersion(ctx, db, migration.version); err != nil {
			return fmt.Errorf("record migration %s: %w", migration.name, err)
		}
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", migration.name, err)
	}
	if _, err := tx.ExecContext(ctx, migration.sql); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("apply migration %s: %w", migration.name, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, 1)`, migration.version); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("record migration %s: %w", migration.name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", migration.name, err)
	}
	return nil
}

func recordMigrationVersion(ctx context.Context, db *sql.DB, version int) error {
	_, err := db.ExecContext(ctx, `INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, 1)`, version)
	return err
}
