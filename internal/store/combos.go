package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ComboProviderType is the type of the built-in provider that holds combos.
// Each combo is a model of that provider, so routes use a combo like any
// other model.
const ComboProviderType = "combo"

// Combo strategies.
const (
	ComboFallback   = "fallback"
	ComboRoundRobin = "round-robin"
	ComboLeastUsed  = "least-used"
)

// ComboMember is one model in a combo and its share of the traffic.
type ComboMember struct {
	// ModelID is a model row.
	ModelID int64
	// Weight is the member's share, at least 1. Round-robin gives a member
	// with weight 3 three turns for every one a weight-1 member gets, and
	// least-used compares load per unit of weight. Fallback ignores it.
	Weight int
}

type Combo struct {
	// ID is the combo's model row.
	ID       int64
	Name     string
	Strategy string
	Enabled  bool
	// Members are the combo's models in order.
	Members []ComboMember
}

// Slug turns a provider name into a slug: "Claude subscription" becomes
// "claude-subscription".
func Slug(name string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(strings.TrimSpace(name)) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-' {
			b.WriteRune(c)
		} else {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-._")
}

func comboProviderID(ctx context.Context, tx *sql.Tx) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM providers WHERE type = ? ORDER BY id LIMIT 1`, ComboProviderType).Scan(&id)
	if !errors.Is(err, sql.ErrNoRows) {
		return id, err
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO providers (type, name, slug, base_url, enabled, created_at) VALUES (?, 'Combos', 'combo', '', 1, ?)`,
		ComboProviderType, time.Now().UnixMilli())
	if err != nil {
		return 0, mapErr(err)
	}
	return res.LastInsertId()
}

func (s *Store) CreateCombo(ctx context.Context, c Combo) (Combo, error) {
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		providerID, err := comboProviderID(ctx, tx)
		if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO models (provider_id, model_id, display_name, enabled) VALUES (?, ?, ?, ?)`,
			providerID, c.Name, c.Name, c.Enabled)
		if err != nil {
			return mapErr(err)
		}
		if c.ID, err = res.LastInsertId(); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO combos (model_id, strategy) VALUES (?, ?)`, c.ID, c.Strategy); err != nil {
			return err
		}
		return insertMembers(ctx, tx, c.ID, c.Members)
	})
	if err != nil {
		return Combo{}, err
	}
	return c, nil
}

func (s *Store) UpdateCombo(ctx context.Context, c Combo) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if err := affected(tx.ExecContext(ctx,
			`UPDATE models SET model_id = ?, display_name = ?, enabled = ? WHERE id = ? AND id IN (SELECT model_id FROM combos)`,
			c.Name, c.Name, c.Enabled, c.ID)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE combos SET strategy = ? WHERE model_id = ?`, c.Strategy, c.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM combo_members WHERE combo_id = ?`, c.ID); err != nil {
			return err
		}
		return insertMembers(ctx, tx, c.ID, c.Members)
	})
}

func insertMembers(ctx context.Context, tx *sql.Tx, comboID int64, members []ComboMember) error {
	for i, m := range members {
		weight := max(m.Weight, 1)
		if _, err := tx.ExecContext(ctx, `INSERT INTO combo_members (combo_id, position, model_id, weight) VALUES (?, ?, ?, ?)`,
			comboID, i, m.ModelID, weight); err != nil {
			return mapErr(err)
		}
	}
	return nil
}

// DeleteCombo removes a combo. It returns ErrInUse while a route tier uses it.
func (s *Store) DeleteCombo(ctx context.Context, id int64) error {
	return affected(s.db.ExecContext(ctx, `DELETE FROM models WHERE id = ? AND id IN (SELECT model_id FROM combos)`, id))
}

// ComboVision reports whether any member of the combo accepts images. A model
// that is not a combo has no members, so it reports false.
func (s *Store) ComboVision(ctx context.Context, id int64) (bool, error) {
	var vision bool
	err := s.db.QueryRowContext(ctx, `
SELECT COALESCE(MAX(m.vision), 0) FROM models m
JOIN combo_members cm ON cm.model_id = m.id WHERE cm.combo_id = ?`, id).Scan(&vision)
	return vision, err
}

func (s *Store) GetCombo(ctx context.Context, id int64) (Combo, error) {
	combos, err := s.listCombos(ctx, `WHERE c.model_id = ?`, id)
	if err != nil {
		return Combo{}, err
	}
	if len(combos) == 0 {
		return Combo{}, ErrNotFound
	}
	return combos[0], nil
}

func (s *Store) ListCombos(ctx context.Context) ([]Combo, error) {
	return s.listCombos(ctx, "")
}

func (s *Store) listCombos(ctx context.Context, where string, args ...any) ([]Combo, error) {
	var combos []Combo
	err := s.each(ctx, `SELECT m.id, m.model_id, m.enabled, c.strategy FROM combos c JOIN models m ON m.id = c.model_id `+where+` ORDER BY m.model_id`,
		args, func(rows *sql.Rows) error {
			var c Combo
			if err := rows.Scan(&c.ID, &c.Name, &c.Enabled, &c.Strategy); err != nil {
				return err
			}
			combos = append(combos, c)
			return nil
		})
	if err != nil {
		return nil, err
	}
	for i := range combos {
		err := s.each(ctx, `SELECT model_id, weight FROM combo_members WHERE combo_id = ? ORDER BY position`, []any{combos[i].ID}, func(rows *sql.Rows) error {
			var m ComboMember
			if err := rows.Scan(&m.ModelID, &m.Weight); err != nil {
				return err
			}
			combos[i].Members = append(combos[i].Members, m)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return combos, nil
}

// CombosUsingModel returns the names of the combos that contain a model.
func (s *Store) CombosUsingModel(ctx context.Context, modelID int64) ([]string, error) {
	var names []string
	err := s.each(ctx, `SELECT DISTINCT m.model_id FROM combo_members cm JOIN models m ON m.id = cm.combo_id WHERE cm.model_id = ? ORDER BY m.model_id`,
		[]any{modelID}, func(rows *sql.Rows) error {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			names = append(names, name)
			return nil
		})
	return names, err
}
