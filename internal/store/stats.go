package store

import (
	"context"
	"database/sql"
)

type StatsTotals struct {
	Requests             int64
	Errors               int64
	EscalateRequests     int64
	EscalatedRequests    int64
	CostUSD              float64
	SubscriptionValueUSD float64
	ReferenceCostUSD     float64
	SavingsUSD           float64
	InputTokens          int64
	OutputTokens         int64
	CacheReadTokens      int64
	CacheWriteTokens     int64
	APITokens            int64
	SubscriptionTokens   int64
	ClassifierCostUSD    float64
}

type StatsPoint struct {
	TS                   int64
	Requests             int64
	CostUSD              float64
	SubscriptionValueUSD float64
	ReferenceCostUSD     float64
	APITokens            int64
	SubscriptionTokens   int64
}

type RouteStats struct {
	Route                string
	Requests             int64
	CostUSD              float64
	SubscriptionValueUSD float64
	ReferenceCostUSD     float64
}

type ModelStats struct {
	Provider         string
	Model            string
	Billing          string
	Calls            int64
	CostUSD          float64
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
}

type Stats struct {
	Totals  StatsTotals
	Series  []StatsPoint
	ByRoute []RouteStats
	ByModel []ModelStats
}

const (
	legTokens = `(l.input_tokens + l.output_tokens + l.cache_read_tokens + l.cache_write_tokens)`
	// API tokens exclude classifier calls, which are routing overhead, not work.
	apiTokens          = `COALESCE(SUM(CASE WHEN l.billing = 'api' AND l.role != 'classifier' THEN ` + legTokens + ` END), 0)`
	subscriptionTokens = `COALESCE(SUM(CASE WHEN l.billing = 'subscription' THEN ` + legTokens + ` END), 0)`
)

// Stats aggregates requests with ts >= since (Unix ms) into buckets of bucketMS.
// Savings count only requests that carry a reference cost.
func (s *Store) Stats(ctx context.Context, since, bucketMS int64) (Stats, error) {
	var st Stats
	t := &st.Totals
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*),
  COALESCE(SUM(r.status NOT IN ('ok', 'canceled')), 0),
  COALESCE(SUM(r.strategy = 'escalate'), 0),
  COALESCE(SUM(EXISTS (SELECT 1 FROM legs l WHERE l.request_id = r.id AND l.role = 'escalation')), 0),
  COALESCE(SUM(r.cost_usd), 0),
  COALESCE(SUM(r.subscription_value_usd), 0),
  COALESCE(SUM(r.reference_cost_usd), 0),
  COALESCE(SUM(CASE WHEN r.reference_cost_usd > 0 THEN r.reference_cost_usd - r.cost_usd ELSE 0 END), 0)
FROM requests r WHERE r.ts >= ?`, since).Scan(
		&t.Requests, &t.Errors, &t.EscalateRequests, &t.EscalatedRequests,
		&t.CostUSD, &t.SubscriptionValueUSD, &t.ReferenceCostUSD, &t.SavingsUSD)
	if err != nil {
		return Stats{}, err
	}
	err = s.db.QueryRowContext(ctx, `
SELECT COALESCE(SUM(l.input_tokens), 0), COALESCE(SUM(l.output_tokens), 0),
  COALESCE(SUM(l.cache_read_tokens), 0), COALESCE(SUM(l.cache_write_tokens), 0),
  `+apiTokens+`, `+subscriptionTokens+`,
  COALESCE(SUM(CASE WHEN l.role = 'classifier' THEN l.cost_usd END), 0)
FROM legs l JOIN requests r ON r.id = l.request_id WHERE r.ts >= ?`, since).Scan(
		&t.InputTokens, &t.OutputTokens, &t.CacheReadTokens, &t.CacheWriteTokens,
		&t.APITokens, &t.SubscriptionTokens, &t.ClassifierCostUSD)
	if err != nil {
		return Stats{}, err
	}

	st.Series = []StatsPoint{}
	bucketIndex := make(map[int64]int)
	err = s.each(ctx, `
SELECT (r.ts / ?) * ? AS bucket, COUNT(*), SUM(r.cost_usd), SUM(r.subscription_value_usd), SUM(r.reference_cost_usd)
FROM requests r WHERE r.ts >= ? GROUP BY bucket ORDER BY bucket`,
		[]any{bucketMS, bucketMS, since}, func(rows *sql.Rows) error {
			var p StatsPoint
			if err := rows.Scan(&p.TS, &p.Requests, &p.CostUSD, &p.SubscriptionValueUSD, &p.ReferenceCostUSD); err != nil {
				return err
			}
			bucketIndex[p.TS] = len(st.Series)
			st.Series = append(st.Series, p)
			return nil
		})
	if err != nil {
		return Stats{}, err
	}
	err = s.each(ctx, `
SELECT (r.ts / ?) * ? AS bucket, `+apiTokens+`, `+subscriptionTokens+`
FROM legs l JOIN requests r ON r.id = l.request_id WHERE r.ts >= ? GROUP BY bucket`,
		[]any{bucketMS, bucketMS, since}, func(rows *sql.Rows) error {
			var bucket, api, sub int64
			if err := rows.Scan(&bucket, &api, &sub); err != nil {
				return err
			}
			if i, ok := bucketIndex[bucket]; ok {
				st.Series[i].APITokens, st.Series[i].SubscriptionTokens = api, sub
			}
			return nil
		})
	if err != nil {
		return Stats{}, err
	}

	st.ByRoute = []RouteStats{}
	err = s.each(ctx, `
SELECT r.route, COUNT(*), SUM(r.cost_usd), SUM(r.subscription_value_usd), SUM(r.reference_cost_usd)
FROM requests r WHERE r.ts >= ? GROUP BY r.route ORDER BY COUNT(*) DESC, r.route`,
		[]any{since}, func(rows *sql.Rows) error {
			var g RouteStats
			if err := rows.Scan(&g.Route, &g.Requests, &g.CostUSD, &g.SubscriptionValueUSD, &g.ReferenceCostUSD); err != nil {
				return err
			}
			st.ByRoute = append(st.ByRoute, g)
			return nil
		})
	if err != nil {
		return Stats{}, err
	}

	st.ByModel = []ModelStats{}
	err = s.each(ctx, `
SELECT l.provider, l.model, l.billing, COUNT(*), SUM(l.cost_usd),
  SUM(l.input_tokens), SUM(l.output_tokens), SUM(l.cache_read_tokens), SUM(l.cache_write_tokens)
FROM legs l JOIN requests r ON r.id = l.request_id WHERE r.ts >= ?
GROUP BY l.provider, l.model, l.billing ORDER BY COUNT(*) DESC, l.model`,
		[]any{since}, func(rows *sql.Rows) error {
			var g ModelStats
			if err := rows.Scan(&g.Provider, &g.Model, &g.Billing, &g.Calls, &g.CostUSD,
				&g.InputTokens, &g.OutputTokens, &g.CacheReadTokens, &g.CacheWriteTokens); err != nil {
				return err
			}
			st.ByModel = append(st.ByModel, g)
			return nil
		})
	if err != nil {
		return Stats{}, err
	}
	return st, nil
}

func (s *Store) each(ctx context.Context, query string, args []any, scan func(*sql.Rows) error) error {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}
