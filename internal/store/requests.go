package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

type Request struct {
	ID                   int64
	TS                   int64
	SessionID            string
	AgentID              string
	Route                string
	Strategy             string
	ClientModel          string
	Stream               bool
	Status               string
	HTTPStatus           int
	Error                string
	CostUSD              float64
	SubscriptionValueUSD float64
	ReferenceCostUSD     float64
	LatencyMS            int64
	Legs                 []Leg
}

type Leg struct {
	Seq              int
	Role             string
	Provider         string
	Model            string
	Billing          string
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	CostUSD          float64
	LatencyMS        int64
	Status           string
	StopReason       string
	Note             string
}

const requestColumns = `id, ts, session_id, agent_id, route, strategy, client_model, stream, status, http_status, error, cost_usd, subscription_value_usd, reference_cost_usd, latency_ms`

func (s *Store) InsertRequest(ctx context.Context, r Request) (int64, error) {
	var id int64
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO requests (ts, session_id, agent_id, route, strategy, client_model, stream, status, http_status, error, cost_usd, subscription_value_usd, reference_cost_usd, latency_ms)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.TS, r.SessionID, r.AgentID, r.Route, r.Strategy, r.ClientModel, r.Stream, r.Status, r.HTTPStatus, r.Error, r.CostUSD, r.SubscriptionValueUSD, r.ReferenceCostUSD, r.LatencyMS)
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		for _, l := range r.Legs {
			_, err := tx.ExecContext(ctx, `INSERT INTO legs (request_id, seq, role, provider, model, billing, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, cost_usd, latency_ms, status, stop_reason, note)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				id, l.Seq, l.Role, l.Provider, l.Model, l.Billing, l.InputTokens, l.OutputTokens, l.CacheReadTokens, l.CacheWriteTokens, l.CostUSD, l.LatencyMS, l.Status, l.StopReason, l.Note)
			if err != nil {
				return err
			}
		}
		return nil
	})
	return id, err
}

type RequestFilter struct {
	SessionID string
	Route     string
	Status    string
	Limit     int
	Offset    int
}

// ListRequests returns matching requests without legs, newest first, and the
// total number of matches.
func (s *Store) ListRequests(ctx context.Context, f RequestFilter) ([]Request, int, error) {
	conds := []string{"1 = 1"}
	var args []any
	for _, c := range []struct{ column, value string }{
		{"session_id", f.SessionID}, {"route", f.Route}, {"status", f.Status},
	} {
		if c.value != "" {
			conds = append(conds, c.column+" = ?")
			args = append(args, c.value)
		}
	}
	where := strings.Join(conds, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM requests WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+requestColumns+` FROM requests WHERE `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`,
		append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Request
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// GetRequest returns a request with its legs in call order.
func (s *Store) GetRequest(ctx context.Context, id int64) (Request, error) {
	r, err := scanRequest(s.db.QueryRowContext(ctx, `SELECT `+requestColumns+` FROM requests WHERE id = ?`, id))
	if err != nil {
		return Request{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT seq, role, provider, model, billing, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, cost_usd, latency_ms, status, stop_reason, note
FROM legs WHERE request_id = ? ORDER BY seq`, id)
	if err != nil {
		return Request{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var l Leg
		if err := rows.Scan(&l.Seq, &l.Role, &l.Provider, &l.Model, &l.Billing, &l.InputTokens, &l.OutputTokens, &l.CacheReadTokens, &l.CacheWriteTokens, &l.CostUSD, &l.LatencyMS, &l.Status, &l.StopReason, &l.Note); err != nil {
			return Request{}, err
		}
		r.Legs = append(r.Legs, l)
	}
	return r, rows.Err()
}

func scanRequest(row scanner) (Request, error) {
	var r Request
	err := row.Scan(&r.ID, &r.TS, &r.SessionID, &r.AgentID, &r.Route, &r.Strategy, &r.ClientModel, &r.Stream, &r.Status, &r.HTTPStatus, &r.Error, &r.CostUSD, &r.SubscriptionValueUSD, &r.ReferenceCostUSD, &r.LatencyMS)
	if errors.Is(err, sql.ErrNoRows) {
		return Request{}, ErrNotFound
	}
	return r, err
}
