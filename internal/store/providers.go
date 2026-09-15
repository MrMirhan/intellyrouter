package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Provider struct {
	ID        int64
	Type      string
	Name      string
	BaseURL   string
	APIKey    string
	Enabled   bool
	CreatedAt int64
}

const providerColumns = `id, type, name, base_url, api_key_enc, enabled, created_at`

func (s *Store) CreateProvider(ctx context.Context, p Provider) (Provider, error) {
	p.CreatedAt = time.Now().UnixMilli()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO providers (type, name, base_url, api_key_enc, enabled, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		p.Type, p.Name, p.BaseURL, s.seal(p.APIKey), p.Enabled, p.CreatedAt)
	if err != nil {
		return Provider{}, mapErr(err)
	}
	p.ID, err = res.LastInsertId()
	return p, err
}

func (s *Store) UpdateProvider(ctx context.Context, p Provider) error {
	return affected(s.db.ExecContext(ctx,
		`UPDATE providers SET type = ?, name = ?, base_url = ?, api_key_enc = ?, enabled = ? WHERE id = ?`,
		p.Type, p.Name, p.BaseURL, s.seal(p.APIKey), p.Enabled, p.ID))
}

func (s *Store) DeleteProvider(ctx context.Context, id int64) error {
	return affected(s.db.ExecContext(ctx, `DELETE FROM providers WHERE id = ?`, id))
}

func (s *Store) GetProvider(ctx context.Context, id int64) (Provider, error) {
	return s.scanProvider(s.db.QueryRowContext(ctx, `SELECT `+providerColumns+` FROM providers WHERE id = ?`, id))
}

func (s *Store) ListProviders(ctx context.Context) ([]Provider, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+providerColumns+` FROM providers ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Provider
	for rows.Next() {
		p, err := s.scanProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) scanProvider(row scanner) (Provider, error) {
	var p Provider
	var sealed []byte
	err := row.Scan(&p.ID, &p.Type, &p.Name, &p.BaseURL, &sealed, &p.Enabled, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Provider{}, ErrNotFound
	}
	if err != nil {
		return Provider{}, err
	}
	p.APIKey, err = s.open(sealed)
	return p, err
}
