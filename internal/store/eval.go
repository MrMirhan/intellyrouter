package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

const (
	EvalRunning  = "running"
	EvalDone     = "done"
	EvalCanceled = "canceled"
	EvalFailed   = "failed"
)

type EvalRun struct {
	ID         int64
	CreatedAt  int64
	FinishedAt int64
	Status     string
	Mode       string
	Routes     []string
	Tasks      []string
	Parallel   int
	Total      int
	Done       int
	Error      string
	Results    []EvalResult
}

type EvalResult struct {
	Task                 string
	Route                string
	Passed               bool
	DurationMS           int64
	Requests             int
	EscalatedRequests    int
	CostUSD              float64
	SubscriptionValueUSD float64
	APITokens            int64
	SubscriptionTokens   int64
	ClaudeOutput         string
	TestOutput           string
	Error                string
}

const evalRunColumns = `r.id, r.created_at, r.finished_at, r.status, r.mode, r.routes, r.tasks, r.parallel, r.total, r.error,
  (SELECT COUNT(*) FROM eval_results e WHERE e.run_id = r.id)`

func (s *Store) CreateEvalRun(ctx context.Context, r EvalRun) (EvalRun, error) {
	routes, err := json.Marshal(r.Routes)
	if err != nil {
		return EvalRun{}, err
	}
	tasks, err := json.Marshal(r.Tasks)
	if err != nil {
		return EvalRun{}, err
	}
	r.CreatedAt = time.Now().UnixMilli()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO eval_runs (created_at, status, mode, routes, tasks, parallel, total) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.CreatedAt, r.Status, r.Mode, string(routes), string(tasks), r.Parallel, r.Total)
	if err != nil {
		return EvalRun{}, err
	}
	r.ID, err = res.LastInsertId()
	return r, err
}

func (s *Store) AddEvalResult(ctx context.Context, runID int64, r EvalResult) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO eval_results (run_id, task, route, passed, duration_ms, requests, escalated_requests,
  cost_usd, subscription_value_usd, api_tokens, subscription_tokens, claude_output, test_output, error)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		runID, r.Task, r.Route, r.Passed, r.DurationMS, r.Requests, r.EscalatedRequests,
		r.CostUSD, r.SubscriptionValueUSD, r.APITokens, r.SubscriptionTokens, r.ClaudeOutput, r.TestOutput, r.Error)
	return err
}

func (s *Store) FinishEvalRun(ctx context.Context, id int64, status, message string) error {
	return affected(s.db.ExecContext(ctx,
		`UPDATE eval_runs SET status = ?, error = ?, finished_at = ? WHERE id = ?`,
		status, message, time.Now().UnixMilli(), id))
}

// FailInterruptedEvalRuns marks runs that were in progress when the gateway stopped.
func (s *Store) FailInterruptedEvalRuns(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE eval_runs SET status = ?, error = 'the gateway stopped during this run', finished_at = ? WHERE status = ?`,
		EvalFailed, time.Now().UnixMilli(), EvalRunning)
	return err
}

// ListEvalRuns returns runs without results, newest first.
func (s *Store) ListEvalRuns(ctx context.Context) ([]EvalRun, error) {
	var out []EvalRun
	err := s.each(ctx, `SELECT `+evalRunColumns+` FROM eval_runs r ORDER BY r.id DESC LIMIT 200`, nil, func(rows *sql.Rows) error {
		r, err := scanEvalRun(rows)
		if err != nil {
			return err
		}
		out = append(out, r)
		return nil
	})
	return out, err
}

// GetEvalRun returns a run with its results ordered by route and task.
func (s *Store) GetEvalRun(ctx context.Context, id int64) (EvalRun, error) {
	r, err := scanEvalRun(s.db.QueryRowContext(ctx, `SELECT `+evalRunColumns+` FROM eval_runs r WHERE r.id = ?`, id))
	if err != nil {
		return EvalRun{}, err
	}
	err = s.each(ctx, `SELECT task, route, passed, duration_ms, requests, escalated_requests, cost_usd, subscription_value_usd,
  api_tokens, subscription_tokens, claude_output, test_output, error
FROM eval_results WHERE run_id = ? ORDER BY route, task`, []any{id}, func(rows *sql.Rows) error {
		var e EvalResult
		if err := rows.Scan(&e.Task, &e.Route, &e.Passed, &e.DurationMS, &e.Requests, &e.EscalatedRequests, &e.CostUSD,
			&e.SubscriptionValueUSD, &e.APITokens, &e.SubscriptionTokens, &e.ClaudeOutput, &e.TestOutput, &e.Error); err != nil {
			return err
		}
		r.Results = append(r.Results, e)
		return nil
	})
	return r, err
}

func (s *Store) DeleteEvalRun(ctx context.Context, id int64) error {
	return affected(s.db.ExecContext(ctx, `DELETE FROM eval_runs WHERE id = ?`, id))
}

func scanEvalRun(row scanner) (EvalRun, error) {
	var r EvalRun
	var finished sql.NullInt64
	var routes, tasks string
	err := row.Scan(&r.ID, &r.CreatedAt, &finished, &r.Status, &r.Mode, &routes, &tasks, &r.Parallel, &r.Total, &r.Error, &r.Done)
	if errors.Is(err, sql.ErrNoRows) {
		return EvalRun{}, ErrNotFound
	}
	if err != nil {
		return EvalRun{}, err
	}
	r.FinishedAt = finished.Int64
	if err := json.Unmarshal([]byte(routes), &r.Routes); err != nil {
		return EvalRun{}, err
	}
	return r, json.Unmarshal([]byte(tasks), &r.Tasks)
}
