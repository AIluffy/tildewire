package store

import (
	"context"
	"database/sql"

	_ "modernc.org/sqlite"

	"github.com/AIluffy/tildewire/internal/store/generated"
)

// Store owns SQLite persistence.
type Store struct {
	db      *sql.DB
	queries *generated.Queries
}

const maxFetchHistoryEntries = 500
const (
	dedupeCandidateLimit     = 250
	dedupeCandidateThreshold = 16
)

// Open opens a SQLite database file.
func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	configureSQLiteDB(db)
	store := &Store{db: db, queries: generated.New(db)}
	if err := store.ping(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

// New wraps an existing database handle.
func New(db *sql.DB) *Store {
	configureSQLiteDB(db)
	return &Store{db: db, queries: generated.New(db)}
}

func configureSQLiteDB(db *sql.DB) {
	// SQLite has a single writer; one connection keeps in-process refresh writes queued
	// instead of racing into SQLITE_BUSY from separate pooled connections.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}

// Migrate applies embedded SQLite migrations.
func (s *Store) Migrate(ctx context.Context) error {
	if err := migrateSQLite(ctx, s.db); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, "PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000; PRAGMA synchronous=NORMAL;")
	return err
}

func (s *Store) ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, "PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;")
	return err
}
