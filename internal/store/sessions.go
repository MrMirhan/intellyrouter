package store

import (
	"context"
	"database/sql"
)

type SessionFilter struct {
	Route  string
	Limit  int
	Offset int
}

type ModelCalls struct {
	Model   string
	Billing string
	Calls   int64
}

type SessionSummary struct {
	SessionID            string
	FirstTS              int64
	LastTS               int64
	Requests             int64
	Errors               int64
	Agents               int64
	Routes               []string
	Models               []ModelCalls
	CostUSD              float64
	SubscriptionValueUSD float64
	InputTokens          int64
	OutputTokens         int64
	CacheReadTokens      int64
	CacheWriteTokens     int64
	DirectorCalls        int64
	AdvisorCalls         int64
	CapturedRequests     int64
}

type SessionModelStats struct {
	Role             string
	Model            string
	Billing          string
	Calls            int64
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	CostUSD          float64
	LatencyMS        int64
}

// Checkpoint is a director or advisor leg of a session.
type Checkpoint struct {
	RequestID int64
	TS        int64
	Role      string
	Model     string
	Billing   string
	Status    string
	Note      string
	LatencyMS int64
}

type Session struct {
	Summary     SessionSummary
	ByModel     []SessionModelStats
	Checkpoints []Checkpoint
	Work        WorkTotals
	// Requests are oldest first, with legs.
	Requests []Request
}

// ListSessions returns sessions with the latest activity first and the total
// number of matches. A route filter keeps sessions with a request on that route.
func (s *Store) ListSessions(ctx context.Context, f SessionFilter) ([]SessionSummary, int, error) {
	where, args := "1 = 1", []any{}
	if f.Route != "" {
		where, args = "r.session_id IN (SELECT session_id FROM requests WHERE route = ?)", []any{f.Route}
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT r.session_id) FROM requests r WHERE r.session_id != '' AND `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	items, err := s.sessionSummaries(ctx, where, append(args, f.Limit, f.Offset))
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// Session returns the summary, per-model usage, director checkpoints, work
// totals, and requests of a session.
func (s *Store) Session(ctx context.Context, id string) (Session, error) {
	summaries, err := s.sessionSummaries(ctx, "r.session_id = ?", []any{id, 1, 0})
	if err != nil {
		return Session{}, err
	}
	if len(summaries) == 0 {
		return Session{}, ErrNotFound
	}
	out := Session{Summary: summaries[0], ByModel: []SessionModelStats{}, Checkpoints: []Checkpoint{}}
	err = s.each(ctx, `
SELECT l.role, l.model, l.billing, COUNT(*), SUM(l.input_tokens), SUM(l.output_tokens), SUM(l.cache_read_tokens), SUM(l.cache_write_tokens),
  SUM(l.cost_usd), SUM(l.latency_ms)
FROM legs l JOIN requests r ON r.id = l.request_id WHERE r.session_id = ?
GROUP BY l.role, l.model, l.billing ORDER BY COUNT(*) DESC, l.model, l.role`,
		[]any{id}, func(rows *sql.Rows) error {
			var m SessionModelStats
			if err := rows.Scan(&m.Role, &m.Model, &m.Billing, &m.Calls, &m.InputTokens, &m.OutputTokens,
				&m.CacheReadTokens, &m.CacheWriteTokens, &m.CostUSD, &m.LatencyMS); err != nil {
				return err
			}
			out.ByModel = append(out.ByModel, m)
			return nil
		})
	if err != nil {
		return Session{}, err
	}
	err = s.each(ctx, `
SELECT r.id, r.ts, l.role, l.model, l.billing, l.status, l.note, l.latency_ms
FROM legs l JOIN requests r ON r.id = l.request_id WHERE r.session_id = ? AND l.role IN ('director', 'advisor')
ORDER BY r.ts, r.id, l.seq`,
		[]any{id}, func(rows *sql.Rows) error {
			var c Checkpoint
			if err := rows.Scan(&c.RequestID, &c.TS, &c.Role, &c.Model, &c.Billing, &c.Status, &c.Note, &c.LatencyMS); err != nil {
				return err
			}
			out.Checkpoints = append(out.Checkpoints, c)
			return nil
		})
	if err != nil {
		return Session{}, err
	}
	if out.Work, err = s.workTotals(ctx, "r.session_id = ?", id); err != nil {
		return Session{}, err
	}
	if out.Requests, err = s.SessionRequests(ctx, id); err != nil {
		return Session{}, err
	}
	return out, nil
}

// SessionRequests returns the requests of a session with their legs, oldest first.
func (s *Store) SessionRequests(ctx context.Context, sessionID string) ([]Request, error) {
	var out []Request
	err := s.each(ctx, `SELECT `+requestColumns+` FROM requests WHERE session_id = ? ORDER BY ts, id`,
		[]any{sessionID}, func(rows *sql.Rows) error {
			r, err := scanRequest(rows)
			if err != nil {
				return err
			}
			out = append(out, r)
			return nil
		})
	if err != nil {
		return nil, err
	}
	legs, err := s.legsWhere(ctx, `r.session_id = ?`, sessionID)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Legs = legs[out[i].ID]
	}
	return out, nil
}

// sessionSummaries aggregates the sessions matched by where; the last two args
// are the limit and offset.
func (s *Store) sessionSummaries(ctx context.Context, where string, args []any) ([]SessionSummary, error) {
	out := []SessionSummary{}
	index := make(map[string]int)
	err := s.each(ctx, `
SELECT r.session_id, MIN(r.ts), MAX(r.ts), COUNT(*), COALESCE(SUM(r.status NOT IN ('ok', 'canceled')), 0),
  COUNT(DISTINCT r.agent_id), COALESCE(SUM(r.cost_usd), 0), COALESCE(SUM(r.subscription_value_usd), 0), COUNT(c.request_id)
FROM requests r LEFT JOIN request_content c ON c.request_id = r.id
WHERE r.session_id != '' AND `+where+`
GROUP BY r.session_id ORDER BY MAX(r.ts) DESC, r.session_id LIMIT ? OFFSET ?`,
		args, func(rows *sql.Rows) error {
			sum := SessionSummary{Routes: []string{}, Models: []ModelCalls{}}
			if err := rows.Scan(&sum.SessionID, &sum.FirstTS, &sum.LastTS, &sum.Requests, &sum.Errors, &sum.Agents,
				&sum.CostUSD, &sum.SubscriptionValueUSD, &sum.CapturedRequests); err != nil {
				return err
			}
			index[sum.SessionID] = len(out)
			out = append(out, sum)
			return nil
		})
	if err != nil || len(out) == 0 {
		return out, err
	}
	ids := make([]any, len(out))
	for i, sum := range out {
		ids[i] = sum.SessionID
	}
	in := placeholders(len(ids))
	err = s.each(ctx, `
SELECT r.session_id, l.model, l.billing, COUNT(*), SUM(l.input_tokens), SUM(l.output_tokens), SUM(l.cache_read_tokens),
  SUM(l.cache_write_tokens), SUM(l.role = 'director'), SUM(l.role = 'advisor')
FROM legs l JOIN requests r ON r.id = l.request_id WHERE r.session_id IN (`+in+`)
GROUP BY r.session_id, l.model, l.billing ORDER BY COUNT(*) DESC, l.model`,
		ids, func(rows *sql.Rows) error {
			var id string
			var m ModelCalls
			var input, output, cacheRead, cacheWrite, director, advisor int64
			if err := rows.Scan(&id, &m.Model, &m.Billing, &m.Calls, &input, &output, &cacheRead, &cacheWrite, &director, &advisor); err != nil {
				return err
			}
			sum := &out[index[id]]
			sum.Models = append(sum.Models, m)
			sum.InputTokens += input
			sum.OutputTokens += output
			sum.CacheReadTokens += cacheRead
			sum.CacheWriteTokens += cacheWrite
			sum.DirectorCalls += director
			sum.AdvisorCalls += advisor
			return nil
		})
	if err != nil {
		return nil, err
	}
	err = s.each(ctx, `SELECT DISTINCT session_id, route FROM requests WHERE session_id IN (`+in+`) ORDER BY route`,
		ids, func(rows *sql.Rows) error {
			var id, route string
			if err := rows.Scan(&id, &route); err != nil {
				return err
			}
			sum := &out[index[id]]
			sum.Routes = append(sum.Routes, route)
			return nil
		})
	if err != nil {
		return nil, err
	}
	return out, nil
}
