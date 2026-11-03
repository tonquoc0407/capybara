package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// Backup makes a consistent SQLite snapshot, including committed WAL pages.
// SQLite requires the destination not to exist.
func (s *Store) Backup(ctx context.Context, path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("backup destination already exists: %s", path)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	_, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, path)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	return nil
}

// ValidateBackup checks integrity and schema compatibility without modifying
// the candidate file. An empty or unrelated SQLite database is rejected.
func ValidateBackup(ctx context.Context, path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", "file:"+url.PathEscape(path)+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()
	var integrity string
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		return fmt.Errorf("integrity check: %w", err)
	}
	if integrity != "ok" {
		return fmt.Errorf("integrity check: %s", integrity)
	}
	var version int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	if version < 1 || version > len(names) {
		return fmt.Errorf("unsupported schema version %d", version)
	}
	var exists int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'runs'`).Scan(&exists); err != nil {
		return err
	}
	if exists != 1 {
		return errors.New("backup has no runs table")
	}
	rows, err := db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return errors.New("backup has foreign key violations")
	}
	return rows.Err()
}

// RestoreBackup replaces an offline database with a validated snapshot. It
// preserves the old file alongside the restored one and refuses WAL sidecars,
// which usually mean another process still has the database open.
func RestoreBackup(ctx context.Context, dbPath, backupPath string) (string, error) {
	dbAbs, err := filepath.Abs(dbPath)
	if err != nil {
		return "", err
	}
	backupAbs, err := filepath.Abs(backupPath)
	if err != nil {
		return "", err
	}
	if dbAbs == backupAbs {
		return "", errors.New("backup and database paths are the same")
	}
	if err := ValidateBackup(ctx, backupAbs); err != nil {
		return "", err
	}
	for _, sidecar := range []string{dbAbs + "-wal", dbAbs + "-shm"} {
		if _, err := os.Stat(sidecar); err == nil {
			return "", fmt.Errorf("database appears active (%s); stop all processes first", sidecar)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
	}
	src, err := os.Open(backupAbs)
	if err != nil {
		return "", err
	}
	defer src.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dbAbs), ".capybara-restore-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return "", err
	}
	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	previous := ""
	if _, err := os.Stat(dbAbs); err == nil {
		previous = fmt.Sprintf("%s.pre-restore-%d", dbAbs, time.Now().UTC().UnixNano())
		if err := os.Link(dbAbs, previous); err != nil {
			return "", fmt.Errorf("preserve current database: %w", err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if err := os.Rename(tmpPath, dbAbs); err != nil {
		return "", err
	}
	return previous, nil
}
