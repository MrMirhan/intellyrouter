// Package store persists providers, routes, keys, and the request ledger in SQLite.
package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

var (
	ErrNotFound = errors.New("store: not found")
	ErrConflict = errors.New("store: already exists")
	ErrInUse    = errors.New("store: still referenced by a route")
)

type Store struct {
	db   *sql.DB
	aead cipher.AEAD
}

type scanner interface {
	Scan(dest ...any) error
}

// Open opens the database at path, applies pending migrations, and uses
// masterKey (32 bytes) to encrypt provider API keys.
func Open(path string, masterKey []byte) (*Store, error) {
	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, fmt.Errorf("store: master key: %w", err)
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, err
	}
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, aead: aead}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		return err
	}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		base := strings.TrimPrefix(name, "migrations/")
		version, err := strconv.Atoi(strings.SplitN(base, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("store: migration %s: bad version prefix", base)
		}
		var applied int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&applied); err != nil {
			return err
		}
		if applied > 0 {
			continue
		}
		text, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		if err := s.inTx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, string(text)); err != nil {
				return fmt.Errorf("store: migration %s: %w", base, err)
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES (?)`, version)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) seal(plain string) []byte {
	if plain == "" {
		return nil
	}
	return s.aead.Seal(nil, nil, []byte(plain), nil)
}

func (s *Store) open(sealed []byte) (string, error) {
	if len(sealed) == 0 {
		return "", nil
	}
	plain, err := s.aead.Open(nil, nil, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("store: decrypt api key (wrong master key?): %w", err)
	}
	return string(plain), nil
}

func affected(res sql.Result, err error) error {
	if err != nil {
		return mapErr(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	switch msg := err.Error(); {
	case strings.Contains(msg, "UNIQUE constraint failed"):
		return fmt.Errorf("%w: %v", ErrConflict, err)
	case strings.Contains(msg, "FOREIGN KEY constraint failed"):
		return fmt.Errorf("%w: %v", ErrInUse, err)
	}
	return err
}
