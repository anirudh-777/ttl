// Package db owns SQLite connection setup and embedded migrations.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open opens (or creates) a SQLite database at path and applies any
// pending migrations. WAL mode is enabled so reads don't block writes.
func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)",
		path,
	)
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	d.SetMaxOpenConns(1) // serialize writers; SQLite + WAL handles readers fine
	if err := d.PingContext(context.Background()); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if err := applyMigrations(d); err != nil {
		return nil, err
	}
	return d, nil
}

// applyMigrations runs any *.sql file under migrations/ whose embedded
// name sorts higher than the recorded schema_meta.version. Each file is
// run inside a transaction.
func applyMigrations(d *sql.DB) error {
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS schema_meta (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		version INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);`); err != nil {
		return fmt.Errorf("create schema_meta: %w", err)
	}

	var current int
	if err := d.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_meta`).Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, name := range files {
		version := versionFromName(name)
		if version <= current {
			continue
		}
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		tx, err := d.Begin()
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", name, err)
		}
		if _, err := tx.Exec(string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.Exec(
			`INSERT INTO schema_meta(id, version, updated_at)
			 VALUES (1, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET version = excluded.version, updated_at = excluded.updated_at`,
			version, time.Now().UnixMilli(),
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("update schema_meta: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", name, err)
		}
	}
	return nil
}

func versionFromName(name string) int {
	// Expect "<n>_<label>.sql" — parse leading integer.
	var n int
	for _, c := range name {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}
