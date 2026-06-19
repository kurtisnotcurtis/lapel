// Package state provides a SQLite-backed store for tracking the persisted
// desired-state baselines of managed Lapel resources (environments, providers,
// and workspaces) so `lapel plan` and `lapel status` can detect drift.
package state

import (
	"database/sql"
	"fmt"
	"sync"

	_ "modernc.org/sqlite" // SQLite driver
)

const (
	// DefaultDBName is the conventional filename for the state database in .lapel/.
	DefaultDBName = "state.db"
)

// Store wraps a SQLite connection with schema-aware desired-state operations.
type Store struct {
	db *sql.DB
	mu sync.RWMutex
}

// New opens or creates the state database at path and runs schema migrations.
func New(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open state db: %w", err)
	}

	store := &Store{db: db}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return store, nil
}

// Close releases the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// migrate ensures all desired-state baseline tables exist. Safe to call repeatedly.
func (s *Store) migrate() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS environments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE NOT NULL,
			distribution TEXT NOT NULL DEFAULT '',
			desired_hash TEXT NOT NULL DEFAULT '',
			actual_hash TEXT NOT NULL DEFAULT '',
			spec_json TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS providers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE NOT NULL,
			type TEXT NOT NULL DEFAULT '',
			base_url TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			desired_hash TEXT NOT NULL DEFAULT '',
			actual_hash TEXT NOT NULL DEFAULT '',
			spec_json TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS workspaces (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE NOT NULL,
			path TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT '',
			import_mode TEXT NOT NULL DEFAULT '',
			desired_hash TEXT NOT NULL DEFAULT '',
			actual_hash TEXT NOT NULL DEFAULT '',
			spec_json TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
		)`,
	}

	for _, q := range queries {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("exec %q: %w", q[:40], err)
		}
	}

	return nil
}
