package eval

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"sync"

	"intellyrouter/internal/store"
)

var (
	ErrInvalid    = errors.New("invalid eval request")
	ErrBusy       = errors.New("an eval run is already in progress")
	ErrNotRunning = errors.New("this eval run is not in progress")
)

const MaxParallel = 4

// Manager runs one eval at a time inside the gateway process and stores the
// results, so the dashboard can start, follow, and cancel runs.
type Manager struct {
	store    *store.Store
	tasksDir string
	gateway  string
	claude   string
	log      *slog.Logger

	mu     sync.Mutex
	active int64
	cancel context.CancelFunc
	done   chan struct{}
}

// NewManager creates a manager. gateway is the URL that Claude Code uses to
// reach this gateway, and claude is the Claude Code binary name or path.
func NewManager(st *store.Store, tasksDir, gateway, claude string, log *slog.Logger) *Manager {
	return &Manager{store: st, tasksDir: tasksDir, gateway: gateway, claude: claude, log: log}
}

func (m *Manager) TasksDir() string { return m.tasksDir }

func (m *Manager) Tasks() ([]Task, error) { return LoadTasks(m.tasksDir) }

// ClaudePath returns the resolved Claude Code binary, or an error when it is missing.
func (m *Manager) ClaudePath() (string, error) { return exec.LookPath(m.claude) }

type StartRequest struct {
	Routes   []string
	TaskIDs  []string
	Mode     Mode
	Parallel int
}

// Start validates the request, records a run, and runs it in the background
// with a gateway key that is revoked when the run ends.
func (m *Manager) Start(ctx context.Context, req StartRequest) (store.EvalRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		return store.EvalRun{}, ErrBusy
	}
	tasks, err := m.selectTasks(req.TaskIDs)
	if err != nil {
		return store.EvalRun{}, err
	}
	if len(req.Routes) == 0 {
		return store.EvalRun{}, fmt.Errorf("%w: select at least one route", ErrInvalid)
	}
	for _, name := range req.Routes {
		if _, err := m.store.RouteByName(ctx, name); errors.Is(err, store.ErrNotFound) {
			return store.EvalRun{}, fmt.Errorf("%w: route %q does not exist", ErrInvalid, name)
		} else if err != nil {
			return store.EvalRun{}, err
		}
	}
	if req.Mode != ModeKey && req.Mode != ModeSubscription {
		return store.EvalRun{}, fmt.Errorf("%w: mode must be %q or %q", ErrInvalid, ModeKey, ModeSubscription)
	}
	if req.Parallel < 1 || req.Parallel > MaxParallel {
		return store.EvalRun{}, fmt.Errorf("%w: parallel must be between 1 and %d", ErrInvalid, MaxParallel)
	}
	claude, err := m.ClaudePath()
	if err != nil {
		return store.EvalRun{}, fmt.Errorf("%w: cannot find the Claude Code binary %q", ErrInvalid, m.claude)
	}

	ids := make([]string, 0, len(tasks))
	for _, t := range tasks {
		ids = append(ids, t.ID)
	}
	plain, key, err := m.store.CreateGatewayKey(ctx, "eval run")
	if err != nil {
		return store.EvalRun{}, err
	}
	run, err := m.store.CreateEvalRun(ctx, store.EvalRun{
		Status: store.EvalRunning, Mode: string(req.Mode), Routes: req.Routes, Tasks: ids,
		Parallel: req.Parallel, Total: len(tasks) * len(req.Routes),
	})
	if err != nil {
		_ = m.store.RevokeGatewayKey(ctx, key.ID)
		return store.EvalRun{}, err
	}

	runCtx, cancel := context.WithCancel(context.Background())
	m.active, m.cancel, m.done = run.ID, cancel, make(chan struct{})
	cfg := Config{Gateway: m.gateway, GatewayKey: plain, Mode: req.Mode, Claude: claude, Ledger: StoreLedger{Store: m.store}}
	go m.execute(runCtx, run, tasks, cfg, key.ID, m.done)
	return run, nil
}

func (m *Manager) selectTasks(ids []string) ([]Task, error) {
	all, err := LoadTasks(m.tasksDir)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("%w: no tasks found in %s", ErrInvalid, m.tasksDir)
	}
	if len(ids) == 0 {
		return all, nil
	}
	byID := make(map[string]Task, len(all))
	for _, t := range all {
		byID[t.ID] = t
	}
	out := make([]Task, 0, len(ids))
	for _, id := range ids {
		t, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("%w: task %q does not exist", ErrInvalid, id)
		}
		out = append(out, t)
	}
	return out, nil
}

func (m *Manager) execute(ctx context.Context, run store.EvalRun, tasks []Task, cfg Config, keyID int64, done chan struct{}) {
	defer close(done)
	bg := context.WithoutCancel(ctx)
	type job struct {
		task  Task
		route string
	}
	jobs := make(chan job)
	var mu sync.Mutex
	var saveErr error
	var wg sync.WaitGroup
	for range run.Parallel {
		wg.Go(func() {
			for j := range jobs {
				r := Run(ctx, cfg, j.task, j.route)
				err := m.store.AddEvalResult(bg, run.ID, store.EvalResult{
					Task: r.Task, Route: r.Route, Passed: r.Passed, DurationMS: r.DurationMS,
					Requests: r.Requests, EscalatedRequests: r.EscalatedRequests,
					CostUSD: r.CostUSD, SubscriptionValueUSD: r.SubscriptionValueUSD,
					APITokens: r.APITokens, SubscriptionTokens: r.SubscriptionTokens,
					ClaudeOutput: r.ClaudeOutput, TestOutput: r.TestOutput, Error: r.Error,
				})
				if err != nil {
					m.log.Error("save eval result", "run", run.ID, "err", err)
					mu.Lock()
					saveErr = errors.Join(saveErr, err)
					mu.Unlock()
				}
			}
		})
	}
dispatch:
	for _, t := range tasks {
		for _, route := range run.Routes {
			select {
			case jobs <- job{t, route}:
			case <-ctx.Done():
				break dispatch
			}
		}
	}
	close(jobs)
	wg.Wait()

	status, message := store.EvalDone, ""
	switch {
	case ctx.Err() != nil:
		status = store.EvalCanceled
	case saveErr != nil:
		status, message = store.EvalFailed, saveErr.Error()
	}
	if err := m.store.RevokeGatewayKey(bg, keyID); err != nil {
		m.log.Warn("revoke eval gateway key", "run", run.ID, "err", err)
	}
	if err := m.store.FinishEvalRun(bg, run.ID, status, message); err != nil {
		m.log.Error("finish eval run", "run", run.ID, "err", err)
	}
	m.mu.Lock()
	m.active, m.cancel = 0, nil
	m.mu.Unlock()
}

// Cancel stops the run with the given ID.
func (m *Manager) Cancel(id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel == nil || m.active != id {
		return ErrNotRunning
	}
	m.cancel()
	return nil
}

// Shutdown cancels the active run and waits until it has recorded its state.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	cancel, done := m.cancel, m.done
	m.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}
