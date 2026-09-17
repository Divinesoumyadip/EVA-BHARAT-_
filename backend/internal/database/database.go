// Package database owns the PostgreSQL connection and applies SQL
// migrations at startup. Migrations are plain, ordered .sql files rather
// than a migration framework - the schema is small enough that a framework
// would be an unnecessary abstraction for this project.
package database

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"sort"
	"time"

	_ "github.com/lib/pq"
)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

// Connect opens a PostgreSQL connection pool and verifies it with a ping
// (retrying briefly, since in container deployments the app and database
// often start at roughly the same time).
func Connect(databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	var pingErr error
	for attempt := 1; attempt <= 10; attempt++ {
		if pingErr = db.Ping(); pingErr == nil {
			return db, nil
		}
		log.Printf("database not ready yet (attempt %d/10): %v", attempt, pingErr)
		time.Sleep(time.Duration(attempt) * time.Second)
	}
	return nil, fmt.Errorf("database unreachable after retries: %w", pingErr)
}

// Migrate applies every embedded .sql file in filename order. Each file is
// expected to be idempotent (IF NOT EXISTS / ON CONFLICT DO NOTHING) so
// running Migrate multiple times, e.g. on every deploy, is safe.
func Migrate(db *sql.DB) error {
	entries, err := fs.ReadDir(embeddedMigrations, "migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		contents, err := embeddedMigrations.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if _, err := db.Exec(string(contents)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		log.Printf("applied migration %s", name)
	}
	return nil
}
