package eval

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MrMirhan/intellyrouter/internal/store"
)

func newManager(t *testing.T, claudeScript string) (*Manager, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "eval.db"), bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := t.Context()
	p, err := st.CreateProvider(ctx, store.Provider{Type: "anthropic", Name: "p", APIKey: "k", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	m, err := st.CreateModel(ctx, store.Model{ProviderID: p.ID, ModelID: "claude-haiku-4-5", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateRoute(ctx, store.Route{Name: "intelly-claude-fast", Strategy: store.StrategyDirect, Tiers: []store.Tier{{ModelID: m.ID, Label: "haiku"}}}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	newTask(t, root)
	claude := filepath.Join(t.TempDir(), "claude")
	writeFile(t, claude, claudeScript)
	t.Setenv("FAKE_LOG", t.TempDir())
	return NewManager(st, root, "http://127.0.0.1:1", claude, slog.New(slog.DiscardHandler)), st
}

func waitFinished(t *testing.T, st *store.Store, id int64) store.EvalRun {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		run, err := st.GetEvalRun(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status != store.EvalRunning {
			return run
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("eval run did not finish")
	return store.EvalRun{}
}

func TestManagerRunsAndStoresResults(t *testing.T) {
	m, st := newManager(t, fakeClaude)
	ctx := t.Context()

	for _, bad := range []StartRequest{
		{Routes: []string{"missing"}, Mode: ModeKey, Parallel: 1},
		{Routes: []string{"intelly-claude-fast"}, TaskIDs: []string{"nope"}, Mode: ModeKey, Parallel: 1},
		{Routes: []string{"intelly-claude-fast"}, Mode: "other", Parallel: 1},
		{Routes: []string{"intelly-claude-fast"}, Mode: ModeKey, Parallel: 9},
	} {
		if _, err := m.Start(ctx, bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("Start(%+v) err = %v, want ErrInvalid", bad, err)
		}
	}

	run, err := m.Start(ctx, StartRequest{Routes: []string{"intelly-claude-fast"}, Mode: ModeKey, Parallel: 1})
	if err != nil {
		t.Fatal(err)
	}
	done := waitFinished(t, st, run.ID)
	if done.Status != store.EvalDone || done.Total != 1 || len(done.Results) != 1 || !done.Results[0].Passed {
		t.Fatalf("run = %+v", done)
	}
	keys, _ := st.ListGatewayKeys(ctx)
	if len(keys) != 1 || keys[0].RevokedAt == 0 {
		t.Fatalf("eval gateway key was not revoked: %+v", keys)
	}
}

func TestManagerCancel(t *testing.T) {
	m, st := newManager(t, "#!/bin/sh\nexec sleep 30\n")
	ctx := t.Context()
	run, err := m.Start(ctx, StartRequest{Routes: []string{"intelly-claude-fast"}, Mode: ModeKey, Parallel: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(ctx, StartRequest{Routes: []string{"intelly-claude-fast"}, Mode: ModeKey, Parallel: 1}); !errors.Is(err, ErrBusy) {
		t.Fatalf("second Start err = %v, want ErrBusy", err)
	}
	if err := m.Cancel(run.ID + 1); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("Cancel(other) err = %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := m.Cancel(run.ID); err != nil {
		t.Fatal(err)
	}
	if got := waitFinished(t, st, run.ID); got.Status != store.EvalCanceled {
		t.Fatalf("canceled run status = %s", got.Status)
	}
	m.Shutdown()
}

func TestManagerRejectsKeyModeForSubscriptionRoutes(t *testing.T) {
	m, st := newManager(t, "#!/bin/sh\nexit 0\n")
	ctx := t.Context()
	fast, err := st.RouteByName(ctx, "intelly-claude-fast")
	if err != nil {
		t.Fatal(err)
	}
	sub, err := st.CreateProvider(ctx, store.Provider{Type: "anthropic-subscription", Name: "claude", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	fable, err := st.CreateModel(ctx, store.Model{ProviderID: sub.ID, ModelID: "claude-fable-5-1", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	// Only the director uses the subscription.
	if _, err := st.CreateRoute(ctx, store.Route{
		Name: "claude-guided-fable", Strategy: store.StrategyGuided, Tiers: fast.Tiers,
		Settings: fmt.Sprintf(`{"director":{"model_id":%d}}`, fable.ID),
	}); err != nil {
		t.Fatal(err)
	}
	_, err = m.Start(ctx, StartRequest{Routes: []string{"claude-guided-fable"}, Mode: ModeKey, Parallel: 1})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "subscription mode") {
		t.Fatalf("Start = %v", err)
	}
}
