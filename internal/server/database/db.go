package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "github.com/mattn/go-sqlite3"
	"go.uber.org/zap"
)

// DB wraps the database connection
type DB struct {
	db     *sql.DB
	logger *zap.Logger
}

// New creates a new database connection and runs migrations
func New(dbPath string, logger *zap.Logger) (*DB, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_loc=auto")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Connection pool settings for SQLite
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	d := &DB{db: db, logger: logger}

	if err := d.runMigrations(); err != nil {
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return d, nil
}

// Close closes the database connection
func (d *DB) Close() error {
	return d.db.Close()
}

// DB returns the underlying sql.DB
func (d *DB) DB() *sql.DB {
	return d.db
}

// runMigrations runs SQL migrations from the migrations directory with version tracking.
// A "schema_migrations" table tracks which migrations have been applied.
// Only migrations not yet recorded in this table will be executed.
func (d *DB) runMigrations() error {
	// Create migrations tracking table
	_, err := d.db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT (datetime('now'))
		)
	`)
	if err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	migrationsDir := "migrations"

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		d.logger.Warn("migrations directory not found, skipping", zap.String("dir", migrationsDir))
		return nil
	}

	// Filter and sort SQL files
	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)

	for _, filename := range files {
		// Check if already applied
		var count int
		err := d.db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", filename).Scan(&count)
		if err != nil {
			return fmt.Errorf("check migration %s: %w", filename, err)
		}
		if count > 0 {
			continue // already applied
		}

		filePath := filepath.Join(migrationsDir, filename)
		data, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", filename, err)
		}

		d.logger.Info("running migration", zap.String("file", filename))

		// Execute migration in a transaction
		tx, err := d.db.Begin()
		if err != nil {
			return fmt.Errorf("begin transaction for %s: %w", filename, err)
		}

		if _, err := tx.Exec(string(data)); err != nil {
			tx.Rollback()
			return fmt.Errorf("execute migration %s: %w", filename, err)
		}

		if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", filename); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %s: %w", filename, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", filename, err)
		}
	}

	return nil
}
