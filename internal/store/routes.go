package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const (
	StrategyDirect   = "direct"
	StrategyEscalate = "escalate"
)

// Tier is one model in a route. Label doubles as the #label prompt marker.
type Tier struct {
	ModelID int64
	Label   string
}

// Route maps a model name that Claude Code sends to a strategy and an ordered
// list of tiers; Tiers[0] is the base model.
type Route struct {
	ID        int64
	Name      string
	Strategy  string
	Tiers     []Tier
	Settings  string
	CreatedAt int64
}

func (s *Store) CreateRoute(ctx context.Context, r Route) (Route, error) {
	if r.Settings == "" {
		r.Settings = "{}"
	}
	r.CreatedAt = time.Now().UnixMilli()
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO routes (name, strategy, settings, created_at) VALUES (?, ?, ?, ?)`,
			r.Name, r.Strategy, r.Settings, r.CreatedAt)
		if err != nil {
			return mapErr(err)
		}
		if r.ID, err = res.LastInsertId(); err != nil {
			return err
		}
		return insertTiers(ctx, tx, r.ID, r.Tiers)
	})
	return r, err
}

func (s *Store) UpdateRoute(ctx context.Context, r Route) error {
	if r.Settings == "" {
		r.Settings = "{}"
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if err := affected(tx.ExecContext(ctx,
			`UPDATE routes SET name = ?, strategy = ?, settings = ? WHERE id = ?`,
			r.Name, r.Strategy, r.Settings, r.ID)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM route_tiers WHERE route_id = ?`, r.ID); err != nil {
			return err
		}
		return insertTiers(ctx, tx, r.ID, r.Tiers)
	})
}

func insertTiers(ctx context.Context, tx *sql.Tx, routeID int64, tiers []Tier) error {
	for i, t := range tiers {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO route_tiers (route_id, position, model_id, label) VALUES (?, ?, ?, ?)`,
			routeID, i, t.ModelID, t.Label); err != nil {
			return mapErr(err)
		}
	}
	return nil
}

func (s *Store) DeleteRoute(ctx context.Context, id int64) error {
	return affected(s.db.ExecContext(ctx, `DELETE FROM routes WHERE id = ?`, id))
}

func (s *Store) GetRoute(ctx context.Context, id int64) (Route, error) {
	return s.loadRoute(ctx, `id = ?`, id)
}

func (s *Store) RouteByName(ctx context.Context, name string) (Route, error) {
	return s.loadRoute(ctx, `name = ?`, name)
}

func (s *Store) loadRoute(ctx context.Context, cond string, arg any) (Route, error) {
	var r Route
	err := s.db.QueryRowContext(ctx, `SELECT id, name, strategy, settings, created_at FROM routes WHERE `+cond, arg).
		Scan(&r.ID, &r.Name, &r.Strategy, &r.Settings, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Route{}, ErrNotFound
	}
	if err != nil {
		return Route{}, err
	}
	r.Tiers, err = s.routeTiers(ctx, r.ID)
	return r, err
}

func (s *Store) ListRoutes(ctx context.Context) ([]Route, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, strategy, settings, created_at FROM routes ORDER BY name`)
	if err != nil {
		return nil, err
	}
	var out []Route
	for rows.Next() {
		var r Route
		if err := rows.Scan(&r.ID, &r.Name, &r.Strategy, &r.Settings, &r.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Tiers, err = s.routeTiers(ctx, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) routeTiers(ctx context.Context, routeID int64) ([]Tier, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT model_id, label FROM route_tiers WHERE route_id = ? ORDER BY position`, routeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Tier
	for rows.Next() {
		var t Tier
		if err := rows.Scan(&t.ModelID, &t.Label); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RoutesUsingModel returns the names of routes that have the model as a tier.
func (s *Store) RoutesUsingModel(ctx context.Context, modelID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT r.name FROM routes r JOIN route_tiers t ON t.route_id = r.id WHERE t.model_id = ? ORDER BY r.name`, modelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}
