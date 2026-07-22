// Package store owns the sqlite database: schema, migrations, writes, queries.
package store

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/url"
	"sort"
	"sync"

	_ "modernc.org/sqlite" // registers the sqlite database/sql driver
)

//go:embed migrations/*.sql
var migrations embed.FS

// Store is a handle on one capybara database file.
type Store struct {
	db   *sql.DB
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
}

// Open opens or creates the database at path and applies pending migrations.
func Open(path string) (*Store, error) {
	dsn := "file:" + url.PathEscape(path) +
		"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db, subs: make(map[chan struct{}]struct{})}, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// Subscribe returns a channel that signals after each committed write, plus a
// cancel func. The one-slot buffer coalesces bursts; a reader that re-queries
// on every signal never misses a write.
func (s *Store) Subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, ok := s.subs[ch]; ok {
			delete(s.subs, ch)
			close(ch)
		}
	}
}

func (s *Store) notify() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(names)
	for i, name := range names {
		target := i + 1
		if target <= version {
			continue
		}
		ddl, err := fs.ReadFile(migrations, name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin %s: %w", name, err)
		}
		if _, err := tx.Exec(string(ddl)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", target)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("bump user_version to %d: %w", target, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", name, err)
		}
	}
	return nil
}
