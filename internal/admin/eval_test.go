package admin_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"intellyrouter/internal/admin"
	"intellyrouter/internal/eval"
	"intellyrouter/internal/store"
)

func TestEvalEndpoints(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "admin.db"), bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := t.Context()
	token, _ := st.EnsureAdminToken(ctx, false)
	p, err := st.CreateProvider(ctx, store.Provider{Type: "anthropic", Name: "p", APIKey: "k", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	m, err := st.CreateModel(ctx, store.Model{ProviderID: p.ID, ModelID: "claude-haiku-4-5", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateRoute(ctx, store.Route{Name: "intelly-claude-fast", Strategy: store.StrategyDirect, Tiers: []store.Tier{{ModelID: m.ID}}}); err != nil {
		t.Fatal(err)
	}

	tasks := t.TempDir()
	mustWrite(t, filepath.Join(tasks, "noop", "task.json"), `{"id":"noop","language":"python","difficulty":"easy","prompt":"Do nothing.","test_command":["true"]}`)
	mustWrite(t, filepath.Join(tasks, "noop", "repo", "README.md"), "noop\n")
	claude := filepath.Join(t.TempDir(), "claude")
	mustWrite(t, claude, "#!/bin/sh\necho '{\"type\":\"result\"}'\n")

	log := slog.New(slog.DiscardHandler)
	mux := http.NewServeMux()
	admin.New(st, http.DefaultClient, log, eval.NewManager(st, tasks, "http://127.0.0.1:1", claude, log)).Register(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client{t: t, base: srv.URL, token: token}

	info := decodeInto[struct {
		ClaudePath string `json:"claude_path"`
		Error      string `json:"error"`
		Tasks      []struct {
			ID string `json:"id"`
		} `json:"tasks"`
	}](t, c.do("GET", "/api/admin/eval/tasks", nil, http.StatusOK))
	if info.ClaudePath != claude || info.Error != "" || len(info.Tasks) != 1 {
		t.Fatalf("eval tasks = %+v", info)
	}

	start := map[string]any{"routes": []string{"intelly-claude-fast"}, "mode": "key", "parallel": 1}
	c.do("POST", "/api/admin/eval/runs", start, http.StatusBadRequest)
	start["confirm"] = true
	created := decodeInto[struct {
		ID int64 `json:"id"`
	}](t, c.do("POST", "/api/admin/eval/runs", start, http.StatusCreated))
	if fresh := c.do("GET", fmt.Sprintf("/api/admin/eval/runs/%d", created.ID), nil, http.StatusOK); !bytes.Contains(fresh, []byte(`"summaries":`)) || !bytes.Contains(fresh, []byte(`"results":`)) {
		t.Fatalf("run detail is missing the result arrays: %s", fresh)
	}

	type runView struct {
		Status    string         `json:"status"`
		Summaries []eval.Summary `json:"summaries"`
	}
	var run runView
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if err := json.Unmarshal(c.do("GET", fmt.Sprintf("/api/admin/eval/runs/%d", created.ID), nil, http.StatusOK), &run); err != nil {
			t.Fatal(err)
		}
		if run.Status != store.EvalRunning {
			break
		}
	}
	if run.Status != store.EvalDone || len(run.Summaries) != 1 || run.Summaries[0].Passed != 1 {
		t.Fatalf("run = %+v", run)
	}

	c.do("POST", fmt.Sprintf("/api/admin/eval/runs/%d/cancel", created.ID), map[string]any{}, http.StatusConflict)
	if runs := decodeInto[[]runView](t, c.do("GET", "/api/admin/eval/runs", nil, http.StatusOK)); len(runs) != 1 {
		t.Fatalf("runs = %+v", runs)
	}
	c.do("DELETE", fmt.Sprintf("/api/admin/eval/runs/%d", created.ID), nil, http.StatusNoContent)
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
