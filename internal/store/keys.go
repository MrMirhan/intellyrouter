package store

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"time"
)

type GatewayKey struct {
	ID         int64
	Name       string
	Prefix     string
	CreatedAt  int64
	LastUsedAt int64
	RevokedAt  int64
}

const keyColumns = `id, name, prefix, created_at, last_used_at, revoked_at`

// CreateGatewayKey issues a key for Claude Code clients. The plain key is
// returned once; only its SHA-256 hash is stored.
func (s *Store) CreateGatewayKey(ctx context.Context, name string) (string, GatewayKey, error) {
	plain := newToken("ik_")
	k := GatewayKey{Name: name, Prefix: plain[:10], CreatedAt: time.Now().UnixMilli()}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO gateway_keys (name, key_hash, prefix, created_at) VALUES (?, ?, ?, ?)`,
		k.Name, hashToken(plain), k.Prefix, k.CreatedAt)
	if err != nil {
		return "", GatewayKey{}, err
	}
	k.ID, err = res.LastInsertId()
	return plain, k, err
}

// VerifyGatewayKey reports whether plain is an active key and records its use.
func (s *Store) VerifyGatewayKey(ctx context.Context, plain string) (GatewayKey, bool, error) {
	k, err := scanKey(s.db.QueryRowContext(ctx,
		`SELECT `+keyColumns+` FROM gateway_keys WHERE key_hash = ? AND revoked_at IS NULL`, hashToken(plain)))
	if errors.Is(err, ErrNotFound) {
		return GatewayKey{}, false, nil
	}
	if err != nil {
		return GatewayKey{}, false, err
	}
	k.LastUsedAt = time.Now().UnixMilli()
	if _, err := s.db.ExecContext(ctx, `UPDATE gateway_keys SET last_used_at = ? WHERE id = ?`, k.LastUsedAt, k.ID); err != nil {
		return GatewayKey{}, false, err
	}
	return k, true, nil
}

func (s *Store) RevokeGatewayKey(ctx context.Context, id int64) error {
	return affected(s.db.ExecContext(ctx,
		`UPDATE gateway_keys SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, time.Now().UnixMilli(), id))
}

func (s *Store) ListGatewayKeys(ctx context.Context) ([]GatewayKey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+keyColumns+` FROM gateway_keys ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GatewayKey
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func scanKey(row scanner) (GatewayKey, error) {
	var k GatewayKey
	var lastUsed, revoked sql.NullInt64
	err := row.Scan(&k.ID, &k.Name, &k.Prefix, &k.CreatedAt, &lastUsed, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return GatewayKey{}, ErrNotFound
	}
	k.LastUsedAt, k.RevokedAt = lastUsed.Int64, revoked.Int64
	return k, err
}

const adminTokenSetting = "admin_token_hash"

// EnsureAdminToken creates the dashboard admin token when none exists or when
// reset is true. It returns the plain token only when it created one.
func (s *Store) EnsureAdminToken(ctx context.Context, reset bool) (string, error) {
	if !reset {
		_, ok, err := s.Setting(ctx, adminTokenSetting)
		if err != nil || ok {
			return "", err
		}
	}
	plain := newToken("ia_")
	return plain, s.SetSetting(ctx, adminTokenSetting, hashToken(plain))
}

func (s *Store) VerifyAdminToken(ctx context.Context, plain string) (bool, error) {
	if plain == "" {
		return false, nil
	}
	want, ok, err := s.Setting(ctx, adminTokenSetting)
	if err != nil || !ok {
		return false, err
	}
	return subtle.ConstantTimeCompare([]byte(hashToken(plain)), []byte(want)) == 1, nil
}
