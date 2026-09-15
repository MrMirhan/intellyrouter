package store

import (
	"context"
	"database/sql"
	"errors"
)

type Model struct {
	ID              int64
	ProviderID      int64
	ModelID         string
	DisplayName     string
	PriceIn         float64
	PriceOut        float64
	PriceCacheRead  float64
	PriceCacheWrite float64
	Context         int64
	Enabled         bool
}

const modelColumns = `id, provider_id, model_id, display_name, price_in, price_out, price_cache_read, price_cache_write, context, enabled`

// SyncModels inserts the models a provider reports and refreshes their names
// and context sizes. Enabled flags are kept; prices are filled only when unset.
func (s *Store) SyncModels(ctx context.Context, providerID int64, models []Model) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		for _, m := range models {
			_, err := tx.ExecContext(ctx, `
INSERT INTO models (provider_id, model_id, display_name, price_in, price_out, price_cache_read, price_cache_write, context, enabled)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)
ON CONFLICT (provider_id, model_id) DO UPDATE SET
  display_name = excluded.display_name,
  context = excluded.context,
  price_in = CASE WHEN models.price_in = 0 AND models.price_out = 0 THEN excluded.price_in ELSE models.price_in END,
  price_out = CASE WHEN models.price_in = 0 AND models.price_out = 0 THEN excluded.price_out ELSE models.price_out END,
  price_cache_read = CASE WHEN models.price_in = 0 AND models.price_out = 0 THEN excluded.price_cache_read ELSE models.price_cache_read END,
  price_cache_write = CASE WHEN models.price_in = 0 AND models.price_out = 0 THEN excluded.price_cache_write ELSE models.price_cache_write END`,
				providerID, m.ModelID, m.DisplayName, m.PriceIn, m.PriceOut, m.PriceCacheRead, m.PriceCacheWrite, m.Context)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) CreateModel(ctx context.Context, m Model) (Model, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO models (provider_id, model_id, display_name, price_in, price_out, price_cache_read, price_cache_write, context, enabled) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ProviderID, m.ModelID, m.DisplayName, m.PriceIn, m.PriceOut, m.PriceCacheRead, m.PriceCacheWrite, m.Context, m.Enabled)
	if err != nil {
		return Model{}, mapErr(err)
	}
	m.ID, err = res.LastInsertId()
	return m, err
}

func (s *Store) UpdateModel(ctx context.Context, m Model) error {
	return affected(s.db.ExecContext(ctx,
		`UPDATE models SET display_name = ?, price_in = ?, price_out = ?, price_cache_read = ?, price_cache_write = ?, context = ?, enabled = ? WHERE id = ?`,
		m.DisplayName, m.PriceIn, m.PriceOut, m.PriceCacheRead, m.PriceCacheWrite, m.Context, m.Enabled, m.ID))
}

func (s *Store) GetModel(ctx context.Context, id int64) (Model, error) {
	return scanModel(s.db.QueryRowContext(ctx, `SELECT `+modelColumns+` FROM models WHERE id = ?`, id))
}

// ModelByModelID returns the first model with the given upstream model ID.
func (s *Store) ModelByModelID(ctx context.Context, modelID string) (Model, error) {
	return scanModel(s.db.QueryRowContext(ctx, `SELECT `+modelColumns+` FROM models WHERE model_id = ? ORDER BY id LIMIT 1`, modelID))
}

// ListModels returns the models of one provider, or of all providers when providerID is 0.
func (s *Store) ListModels(ctx context.Context, providerID int64) ([]Model, error) {
	query := `SELECT ` + modelColumns + ` FROM models`
	var args []any
	if providerID != 0 {
		query += ` WHERE provider_id = ?`
		args = append(args, providerID)
	}
	rows, err := s.db.QueryContext(ctx, query+` ORDER BY provider_id, model_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Model
	for rows.Next() {
		m, err := scanModel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func scanModel(row scanner) (Model, error) {
	var m Model
	err := row.Scan(&m.ID, &m.ProviderID, &m.ModelID, &m.DisplayName, &m.PriceIn, &m.PriceOut, &m.PriceCacheRead, &m.PriceCacheWrite, &m.Context, &m.Enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return Model{}, ErrNotFound
	}
	return m, err
}
