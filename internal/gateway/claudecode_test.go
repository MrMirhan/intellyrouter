package gateway_test

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"intellyrouter/internal/gateway"
	"intellyrouter/internal/ledger"
	"intellyrouter/internal/provider"
	"intellyrouter/internal/store"
)

// A subscription director asked through Claude Code gives the API executor
// written guidance, and the next checkpoint resumes the same conversation
// with only the new part of the session.
func TestSubscriptionDirectorThroughClaudeCode(t *testing.T) {
	fixture, err := os.ReadFile("../../testdata/anthropic/stream_tool_use.sse")
	must(t, err)
	var mu sync.Mutex
	var executorBody string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		executorBody = string(raw)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(fixture)
	}))
	t.Cleanup(up.Close)

	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nprintf 'ARGS %s\\n' \"$*\" >> \"$FAKE_CLAUDE/args\"\ncat >> \"$FAKE_CLAUDE/prompts\"\nprintf 'PROMPT-END' >> \"$FAKE_CLAUDE/prompts\"\n" +
		`printf '%s' '{"is_error":false,"result":"1. Use a table test.","usage":{"input_tokens":3,"output_tokens":7,"cache_read_input_tokens":100,"cache_creation_input_tokens":50,"cache_creation":{"ephemeral_1h_input_tokens":50}}}'` + "\n"
	must(t, os.WriteFile(bin, []byte(script), 0o755))
	t.Setenv("FAKE_CLAUDE", dir)

	st, err := store.Open(filepath.Join(dir, "cc.db"), bytes.Repeat([]byte{8}, 32))
	must(t, err)
	t.Cleanup(func() { st.Close() })
	ctx := t.Context()
	ds, err := st.CreateProvider(ctx, store.Provider{Type: string(provider.AnthropicCompatible), Name: "deepseek", BaseURL: up.URL, APIKey: "sk-deepseek", Enabled: true})
	must(t, err)
	sub, err := st.CreateProvider(ctx, store.Provider{Type: string(provider.AnthropicSubscription), Name: "claude", Enabled: true})
	must(t, err)
	flash, err := st.CreateModel(ctx, store.Model{ProviderID: ds.ID, ModelID: "deepseek-v4-flash", PriceIn: 0.1, PriceOut: 0.4, Enabled: true})
	must(t, err)
	fable, err := st.CreateModel(ctx, store.Model{ProviderID: sub.ID, ModelID: "claude-fable-5-1", Enabled: true})
	must(t, err)
	_, err = st.CreateRoute(ctx, store.Route{
		Name: "claude-cc", Strategy: store.StrategyGuided, Tiers: []store.Tier{{ModelID: flash.ID, Label: "flash"}},
		Settings: fmt.Sprintf(`{"director":{"model_id":%d,"effort":"high","claude_code":true},"checkpoints":{"steps":0}}`, fable.ID),
	})
	must(t, err)
	key, _, err := st.CreateGatewayKey(ctx, "test")
	must(t, err)

	log := slog.New(slog.DiscardHandler)
	mux := http.NewServeMux()
	gw := gateway.New(st, ledger.NewRecorder(st, log), up.Client(), log)
	gw.UseClaudeCode(bin, filepath.Join(dir, "director"))
	gw.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	post := func(messages string) {
		body := `{"model":"claude-cc","max_tokens":64,"stream":true,"system":"You are Claude Code. Working directory: /repo",` +
			`"tools":[{"name":"Bash","input_schema":{"type":"object"}}],"messages":[` + messages + `]}`
		if resp, out := send(t, http.MethodPost, srv.URL+"/v1/messages", body, map[string]string{"X-Intelly-Key": key, "X-Claude-Code-Session-Id": "s1"}); resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d: %s", resp.StatusCode, out)
		}
	}
	first := `{"role":"user","content":"Fix the calc tests"}`
	post(first)
	post(first + `,{"role":"assistant","content":[{"type":"text","text":"Tests fixed."}]},{"role":"user","content":"Now add docs"}`)

	rawArgs, err := os.ReadFile(filepath.Join(dir, "args"))
	must(t, err)
	rawPrompts, err := os.ReadFile(filepath.Join(dir, "prompts"))
	must(t, err)
	args := strings.Split(string(rawArgs), "ARGS ")[1:]
	prompts := strings.Split(string(rawPrompts), "PROMPT-END")
	id := regexp.MustCompile(`--session-id ([0-9a-f-]{36})`).FindStringSubmatch(args[0])
	if len(args) != 2 || id == nil || !strings.Contains(args[0], "--effort high") || !strings.Contains(args[1], "--resume "+id[1]) {
		t.Fatalf("claude args = %q", args)
	}
	for _, want := range []string{"Working directory: /repo", "Fix the calc tests", "Checkpoint: turn start"} {
		if !strings.Contains(prompts[0], want) {
			t.Fatalf("first prompt misses %q:\n%s", want, prompts[0])
		}
	}
	if !strings.Contains(prompts[1], "# Executor session, continued") || !strings.Contains(prompts[1], "Tests fixed.") ||
		!strings.Contains(prompts[1], "Now add docs") || strings.Contains(prompts[1], "Working directory") || strings.Contains(prompts[1], "Fix the calc tests") {
		t.Fatalf("resumed prompt =\n%s", prompts[1])
	}

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(executorBody, "1. Use a table test.") || !strings.Contains(executorBody, `"name":"ask_director"`) {
		t.Fatalf("executor request = %s", executorBody)
	}
	legs := requestLegs(t, st)
	for i, note := range []string{"via Claude Code", "via Claude Code, resumed"} {
		d := legs[i][0]
		if d.Role != ledger.RoleDirector || d.Billing != ledger.BillingSubscription || d.Status != ledger.StatusOK ||
			d.InputTokens != 3 || d.CacheReadTokens != 100 || !strings.Contains(d.Note, note) {
			t.Fatalf("request %d legs = %+v", i, legs[i])
		}
	}
}
