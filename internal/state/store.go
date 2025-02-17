// Package state provides a SQLite-backed store for tracking the current state
// of the WSL environment relative to the manifest. It's the foundation for
// the reconcile engine — without knowing what's actually deployed, you can't
// detect drift or plan corrections.
package state

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite" // SQLite driver
)

const (
	// DefaultDBName is the conventional filename for the state database in ~/.lapel/.
	DefaultDBName = "state.db"
)

// Store wraps a SQLite connection with schema-aware CRUD operations.
// A mutex protects concurrent access to the database from multiple goroutines.
type Store struct {
	db    *sql.DB
	mu    sync.RWMutex // protects concurrent reads/writes
}

// New opens or creates the state database at path. Runs migrations if needed.
func New(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open state db: %w", err)
	}

	store := &Store{db: db}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return store, nil
}

// Close releases the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// migrate ensures all tables exist with current schema. Safe to call multiple times.
func (s *Store) migrate() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS distros (
			name TEXT PRIMARY KEY,
			base TEXT NOT NULL DEFAULT '',
			version TEXT NOT NULL DEFAULT '',
			rootfs_url TEXT NOT NULL DEFAULT '',
			default_user TEXT NOT NULL DEFAULT '',
			systemd INTEGER NOT NULL DEFAULT 1,
			provisioned_at DATETIME NOT NULL DEFAULT (datetime('now')),
			updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
		)`,
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

		`CREATE TABLE IF NOT EXISTS packages (
			distro TEXT NOT NULL,
			name TEXT NOT NULL,
			version TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT 'manifest',
			installed_at DATETIME NOT NULL DEFAULT (datetime('now')),
			PRIMARY KEY (distro, name)
		)`,

		`CREATE TABLE IF NOT EXISTS jail_rules (
			distro TEXT NOT NULL,
			rule_type TEXT NOT NULL CHECK(rule_type IN ('filesystem', 'network', 'process', 'resource')),
			key TEXT NOT NULL,
			value TEXT NOT NULL DEFAULT '',
			applied_at DATETIME NOT NULL DEFAULT (datetime('now')),
			PRIMARY KEY (distro, rule_type, key)
		)`,

		`CREATE TABLE IF NOT EXISTS env_vars (
			distro TEXT NOT NULL,
			key TEXT NOT NULL,
			value TEXT NOT NULL DEFAULT '',
			applied_at DATETIME NOT NULL DEFAULT (datetime('now')),
			PRIMARY KEY (distro, key)
		)`,

		`CREATE TABLE IF NOT EXISTS apply_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			distro TEXT NOT NULL,
			action TEXT NOT NULL CHECK(action IN ('plan', 'apply', 'rollback')),
			diff_summary TEXT NOT NULL DEFAULT '',
			applied_at DATETIME NOT NULL DEFAULT (datetime('now'))
		)`,
	}

	for _, q := range queries {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("exec %q: %w", q[:40], err)
		}
	}

	return nil
}

// DistroRecord represents the current state of a provisioned distro.
type DistroRecord struct {
	Name         string    `db:"name"`
	Base         string    `db:"base"`
	Version      string    `db:"version"`
	RootFSURL    string    `db:"rootfs_url"`
	DefaultUser  string    `db:"default_user"`
	Systemd      bool      `db:"systemd"`
	ProvisionedAt time.Time `db:"provisioned_at"`
	UpdatedAt    time.Time `db:"updated_at"`
}

// GetDistro retrieves the current state of a provisioned distro.
func (s *Store) GetDistro(name string) (*DistroRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	row := s.db.QueryRow(
		`SELECT name, base, version, rootfs_url, default_user, systemd, provisioned_at, updated_at
		 FROM distros WHERE name = ?`, name,
	)

	rec := &DistroRecord{}
	err := row.Scan(&rec.Name, &rec.Base, &rec.Version, &rec.RootFSURL,
		&rec.DefaultUser, &rec.Systemd, &rec.ProvisionedAt, &rec.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get distro %q: %w", name, err)
	}

	return rec, nil
}

// UpsertDistro inserts or updates a distro record.
func (s *Store) UpsertDistro(rec *DistroRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO distros (name, base, version, rootfs_url, default_user, systemd, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, datetime('now'))
		 ON CONFLICT(name) DO UPDATE SET
		   base = excluded.base,
		   version = excluded.version,
		   rootfs_url = excluded.rootfs_url,
		   default_user = excluded.default_user,
		   systemd = excluded.systemd,
		   updated_at = datetime('now')`,
		rec.Name, rec.Base, rec.Version, rec.RootFSURL,
		rec.DefaultUser, rec.Systemd,
	)
	if err != nil {
		return fmt.Errorf("upsert distro %q: %w", rec.Name, err)
	}

	return nil
}

// PackageRecord represents an installed package.
type PackageRecord struct {
	Distro      string    `db:"distro"`
	Name        string    `db:"name"`
	Version     string    `db:"version"`
	Source      string    `db:"source"` // 'manifest' or 'manual'
	InstalledAt time.Time `db:"installed_at"`
}

// GetPackages retrieves all packages for a distro.
func (s *Store) GetPackages(distro string) ([]PackageRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query(
		`SELECT name, version, source, installed_at FROM packages WHERE distro = ?`, distro,
	)
	if err != nil {
		return nil, fmt.Errorf("get packages for %q: %w", distro, err)
	}
	defer rows.Close()

	var pkgs []PackageRecord
	for rows.Next() {
		var p PackageRecord
		if err := rows.Scan(&p.Name, &p.Version, &p.Source, &p.InstalledAt); err != nil {
			return nil, fmt.Errorf("scan package: %w", err)
		}
		p.Distro = distro
		pkgs = append(pkgs, p)
	}

	return pkgs, rows.Err()
}

// UpsertPackage inserts or updates a package record.
func (s *Store) UpsertPackage(pkg PackageRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO packages (distro, name, version, source) VALUES (?, ?, ?, ?)
		 ON CONFLICT(distro, name) DO UPDATE SET
		   version = excluded.version,
		   source = excluded.source`,
		pkg.Distro, pkg.Name, pkg.Version, pkg.Source,
	)
	if err != nil {
		return fmt.Errorf("upsert package %q: %w", pkg.Name, err)
	}

	return nil
}

// DeletePackage removes a specific package from the store.
func (s *Store) DeletePackage(distro, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM packages WHERE distro = ? AND name = ?`, distro, name)
	if err != nil {
		return fmt.Errorf("delete package %q for %q: %w", name, distro, err)
	}

	return nil
}

// UpsertPackageTx inserts or updates a package record within a transaction.
func (s *Store) UpsertPackageTx(tx *sql.Tx, pkg PackageRecord) error {
	_, err := tx.Exec(
		`INSERT INTO packages (distro, name, version, source) VALUES (?, ?, ?, ?)
		 ON CONFLICT(distro, name) DO UPDATE SET
		   version = excluded.version,
		   source = excluded.source`,
		pkg.Distro, pkg.Name, pkg.Version, pkg.Source,
	)
	if err != nil {
		return fmt.Errorf("upsert package %q (tx): %w", pkg.Name, err)
	}

	return nil
}

// DeletePackageTx removes a specific package within a transaction.
func (s *Store) DeletePackageTx(tx *sql.Tx, distro, name string) error {
	_, err := tx.Exec(`DELETE FROM packages WHERE distro = ? AND name = ?`, distro, name)
	if err != nil {
		return fmt.Errorf("delete package %q for %q (tx): %w", name, distro, err)
	}

	return nil
}

// DeletePackages removes all packages for a distro.
func (s *Store) DeletePackages(distro string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM packages WHERE distro = ?`, distro)
	if err != nil {
		return fmt.Errorf("delete packages for %q: %w", distro, err)
	}

	return nil
}

// JailRule represents an applied jail rule.
type JailRule struct {
	Distro   string `db:"distro"`
	Type     string `db:"rule_type"` // filesystem, network, process, resource
	Key      string `db:"key"`
	Value    string `db:"value"`
}

// GetJailRules retrieves all jail rules for a distro.
func (s *Store) GetJailRules(distro string) ([]JailRule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query(
		`SELECT rule_type, key, value FROM jail_rules WHERE distro = ?`, distro,
	)
	if err != nil {
		return nil, fmt.Errorf("get jail rules for %q: %w", distro, err)
	}
	defer rows.Close()

	var rules []JailRule
	for rows.Next() {
		var r JailRule
		if err := rows.Scan(&r.Type, &r.Key, &r.Value); err != nil {
			return nil, fmt.Errorf("scan jail rule: %w", err)
		}
		r.Distro = distro
		rules = append(rules, r)
	}

	return rules, rows.Err()
}

// UpsertJailRule inserts or updates a jail rule.
func (s *Store) UpsertJailRule(rule JailRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO jail_rules (distro, rule_type, key, value) VALUES (?, ?, ?, ?)
		 ON CONFLICT(distro, rule_type, key) DO UPDATE SET value = excluded.value`,
		rule.Distro, rule.Type, rule.Key, rule.Value,
	)
	if err != nil {
		return fmt.Errorf("upsert jail rule %q/%s: %w", rule.Distro, rule.Key, err)
	}

	return nil
}

// DeleteJailRules removes all jail rules for a distro.
func (s *Store) DeleteJailRules(distro string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM jail_rules WHERE distro = ?`, distro)
	if err != nil {
		return fmt.Errorf("delete jail rules for %q: %w", distro, err)
	}

	return nil
}

// UpsertJailRuleTx inserts or updates a jail rule within a transaction.
func (s *Store) UpsertJailRuleTx(tx *sql.Tx, rule JailRule) error {
	_, err := tx.Exec(
		`INSERT INTO jail_rules (distro, rule_type, key, value) VALUES (?, ?, ?, ?)
		 ON CONFLICT(distro, rule_type, key) DO UPDATE SET value = excluded.value`,
		rule.Distro, rule.Type, rule.Key, rule.Value,
	)
	if err != nil {
		return fmt.Errorf("upsert jail rule %q/%s (tx): %w", rule.Distro, rule.Key, err)
	}

	return nil
}

// EnvVar represents an injected environment variable.
type EnvVar struct {
	Distro string `db:"distro"`
	Key    string `db:"key"`
	Value  string `db:"value"`
}

// GetEnvVars retrieves all env vars for a distro.
func (s *Store) GetEnvVars(distro string) ([]EnvVar, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query(
		`SELECT key, value FROM env_vars WHERE distro = ?`, distro,
	)
	if err != nil {
		return nil, fmt.Errorf("get env vars for %q: %w", distro, err)
	}
	defer rows.Close()

	var vars []EnvVar
	for rows.Next() {
		var v EnvVar
		if err := rows.Scan(&v.Key, &v.Value); err != nil {
			return nil, fmt.Errorf("scan env var: %w", err)
		}
		v.Distro = distro
		vars = append(vars, v)
	}

	return vars, rows.Err()
}

// UpsertEnvVar inserts or updates an env var.
func (s *Store) UpsertEnvVar(v EnvVar) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO env_vars (distro, key, value) VALUES (?, ?, ?)
		 ON CONFLICT(distro, key) DO UPDATE SET value = excluded.value`,
		v.Distro, v.Key, v.Value,
	)
	if err != nil {
		return fmt.Errorf("upsert env var %q: %w", v.Key, err)
	}

	return nil
}

// DeleteEnvVars removes all env vars for a distro.
func (s *Store) DeleteEnvVars(distro string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM env_vars WHERE distro = ?`, distro)
	if err != nil {
		return fmt.Errorf("delete env vars for %q: %w", distro, err)
	}

	return nil
}

// UpsertEnvVarTx inserts or updates an env var within a transaction.
func (s *Store) UpsertEnvVarTx(tx *sql.Tx, v EnvVar) error {
	_, err := tx.Exec(
		`INSERT INTO env_vars (distro, key, value) VALUES (?, ?, ?)
		 ON CONFLICT(distro, key) DO UPDATE SET value = excluded.value`,
		v.Distro, v.Key, v.Value,
	)
	if err != nil {
		return fmt.Errorf("upsert env var %q (tx): %w", v.Key, err)
	}

	return nil
}

// ApplyLogRecord represents a reconcile action.
type ApplyLogRecord struct {
	ID           int       `db:"id"`
	Distro       string    `db:"distro"`
	Action       string    `db:"action"` // plan, apply, rollback
	DiffSummary  string    `db:"diff_summary"`
	AppliedAt    time.Time `db:"applied_at"`
}

// LogApply records a reconcile action.
func (s *Store) LogApply(distro, action, diffSummary string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO apply_log (distro, action, diff_summary) VALUES (?, ?, ?)`,
		distro, action, diffSummary,
	)
	if err != nil {
		return fmt.Errorf("log apply: %w", err)
	}

	return nil
}

// GetApplyLog retrieves recent reconcile actions.
func (s *Store) GetApplyLog(distro string, limit int) ([]ApplyLogRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query(
		`SELECT id, distro, action, diff_summary, applied_at FROM apply_log WHERE distro = ? ORDER BY applied_at DESC LIMIT ?`,
		distro, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("get apply log: %w", err)
	}
	defer rows.Close()

	var logs []ApplyLogRecord
	for rows.Next() {
		var l ApplyLogRecord
		if err := rows.Scan(&l.ID, &l.Distro, &l.Action, &l.DiffSummary, &l.AppliedAt); err != nil {
			return nil, fmt.Errorf("scan log: %w", err)
		}
		logs = append(logs, l)
	}

	return logs, rows.Err()
}

// Tx wraps a database/sql.Tx with the store's mutex for thread safety.
type Tx struct {
	Tx  *sql.Tx
	s   *Store
}

// BeginTx starts a new transaction within the store's lock.
func (s *Store) BeginTx() (*Tx, error) {
	s.mu.Lock()
	tx, err := s.db.Begin()
	if err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	return &Tx{Tx: tx, s: s}, nil
}

// Commit commits the transaction and releases the store's lock.
func (t *Tx) Commit() error {
	err := t.Tx.Commit()
	if err != nil {
		t.s.mu.Unlock()
		return fmt.Errorf("commit tx: %w", err)
	}
	t.s.mu.Unlock()
	return nil
}

// Rollback rolls back the transaction and releases the store's lock.
func (t *Tx) Rollback() error {
	err := t.Tx.Rollback()
	if err != nil {
		t.s.mu.Unlock()
		return fmt.Errorf("rollback tx: %w", err)
	}
	t.s.mu.Unlock()
	return nil
}
