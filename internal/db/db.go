package db

import (
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB wraps sql.DB with tailfeed-specific helpers.
type DB struct {
	*sql.DB
}

// WrapDB wraps an existing *sql.DB (for testing).
func WrapDB(sqlDB *sql.DB) *DB { return &DB{sqlDB} }

// MigrateForTest runs migrations on an already-open DB (for testing).
func (d *DB) MigrateForTest() error { return d.migrate() }

// Open opens (or creates) the SQLite database in ~/.config/tailfeed/.
func Open() (*DB, error) {
	dir, err := configDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}
	dsn := filepath.Join(dir, "tailfeed.db") + "?_foreign_keys=on&_journal_mode=WAL"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	sqlDB.SetMaxOpenConns(1) // SQLite: single writer
	d := &DB{sqlDB}
	if err := d.migrate(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
}

func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "tailfeed"), nil
}

// Clear deletes all articles, feeds, and groups from the database.
func (d *DB) Clear() error {
	if _, err := d.Exec(`DELETE FROM articles`); err != nil {
		return err
	}
	if _, err := d.Exec(`DELETE FROM feeds`); err != nil {
		return err
	}
	_, err := d.Exec(`DELETE FROM groups`)
	return err
}

func (d *DB) migrate() error {
	// Ensure the tracking table exists.
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	for _, e := range entries {
		var applied int
		_ = d.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, e.Name()).Scan(&applied)
		if applied > 0 {
			continue // already ran
		}
		data, err := migrationsFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		if _, err := d.Exec(string(data)); err != nil {
			// "duplicate column name" means the migration already ran before
			// the schema_migrations table existed — treat as applied.
			if !strings.Contains(err.Error(), "duplicate column name") {
				return fmt.Errorf("run migration %s: %w", e.Name(), err)
			}
		}
		if _, err := d.Exec(`INSERT INTO schema_migrations (name) VALUES (?)`, e.Name()); err != nil {
			return fmt.Errorf("record migration %s: %w", e.Name(), err)
		}
	}
	return nil
}
