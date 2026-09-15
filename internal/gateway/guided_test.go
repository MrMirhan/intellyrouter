package gateway_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"intellyrouter/internal/gateway"
	"intellyrouter/internal/ledger"
	"intellyrouter/internal/provider"
	"intellyrouter/internal/store"
)

type guidedCall struct {
	model, auth string
	director    bool
	guidance    bool
	stream      bool
	consult     bool // the executor request offers ask_director
	answered    bool // the executor request carries the director's answer
}

// setupGuided builds a guided route: DeepSeek flash and pro as executor tiers
// and Claude Fable 5.1 as director, on an API key or on the subscription.
func setupGuided(t *testing.T, directorType provider.Type) (env, func() []guidedCall) {
	t.Helper()
	fixture, err := os.ReadFile("../../testdata/anthropic/stream_tool_use.sse")
	must(t, err)
	askFixture, err := os.ReadFile("../../testdata/anthropic/stream_ask_director.sse")
	must(t, err)
	afterAnswer, err := os.ReadFile("../../testdata/anthropic/stream_after_answer.sse")
	must(t, err)
	var mu sync.Mutex
	var calls []guidedCall
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		_ = json.Unmarshal(raw, &body)
		c := guidedCall{
			model: body.Model, auth: r.Header.Get("Authorization"), stream: body.Stream,
			director: bytes.Contains(raw, []byte("You direct a coding agent")),
			guidance: bytes.Contains(raw, []byte("director-guidance")),
			consult:  bytes.Contains(raw, []byte(`"name":"ask_director"`)),
			answered: bytes.Contains(raw, []byte("director-answer")),
		}
		mu.Lock()
		calls = append(calls, c)
		mu.Unlock()
		if c.director {
			if bytes.Contains(raw, []byte(`"tools"`)) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"type":"message","role":"assistant","content":[{"type":"text","text":"1. Fix Add in calc.go.\n2. Run go test."}],"stop_reason":"end_turn","usage":{"input_tokens":900,"output_tokens":120}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		switch {
		case c.answered:
			_, _ = w.Write(afterAnswer)
		case bytes.Contains(raw, []byte("ASK_TEST")):
			_, _ = w.Write(askFixture)
		default:
			_, _ = w.Write(fixture)
		}
	}))
	t.Cleanup(up.Close)

	dbPath := filepath.Join(t.TempDir(), "guided.db")
	st, err := store.Open(dbPath, bytes.Repeat([]byte{6}, 32))
	must(t, err)
	t.Cleanup(func() { st.Close() })
	ctx := t.Context()
	ds, err := st.CreateProvider(ctx, store.Provider{Type: string(provider.AnthropicCompatible), Name: "deepseek", BaseURL: up.URL, APIKey: "sk-deepseek", Enabled: true})
	must(t, err)
	claude, err := st.CreateProvider(ctx, store.Provider{Type: string(directorType), Name: "claude", BaseURL: up.URL, APIKey: "sk-anthropic", Enabled: true})
	must(t, err)
	flash, err := st.CreateModel(ctx, store.Model{ProviderID: ds.ID, ModelID: "deepseek-v4-flash", PriceIn: 0.1, PriceOut: 0.4, Enabled: true})
	must(t, err)
	pro, err := st.CreateModel(ctx, store.Model{ProviderID: ds.ID, ModelID: "deepseek-v4-pro", PriceIn: 0.5, PriceOut: 2, Enabled: true})
	must(t, err)
	fable, err := st.CreateModel(ctx, store.Model{ProviderID: claude.ID, ModelID: "claude-fable-5-1", Enabled: true})
	must(t, err)
	_, err = st.CreateRoute(ctx, store.Route{
		Name: "claude-guided", Strategy: store.StrategyGuided,
		Tiers:    []store.Tier{{ModelID: flash.ID, Label: "flash"}, {ModelID: pro.ID, Label: "pro"}},
		Settings: fmt.Sprintf(`{"director":{"model_id":%d},"checkpoints":{"steps":0}}`, fable.ID),
	})
	must(t, err)
	key, _, err := st.CreateGatewayKey(ctx, "test")
	must(t, err)

	log := slog.New(slog.DiscardHandler)
	mux := http.NewServeMux()
	gateway.New(st, ledger.NewRecorder(st, log), up.Client(), log).Register(mux)
	gw := httptest.NewServer(mux)
	t.Cleanup(gw.Close)
	return env{url: gw.URL, store: st, key: key, dbPath: dbPath}, func() []guidedCall {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(calls)
	}
}

func sendGuided(t *testing.T, e env, messages string) []byte {
	t.Helper()
	body := `{"model":"claude-guided","max_tokens":1024,"stream":true,"tools":[{"name":"Bash","input_schema":{"type":"object"}}],"messages":[` + messages + `]}`
	resp, out := send(t, http.MethodPost, e.url+"/v1/messages", body, map[string]string{
		"X-Intelly-Key": e.key, "Authorization": claudeLogin, "X-Claude-Code-Session-Id": "s1",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, out)
	}
	return out
}

const (
	guidedPrompt = `{"role":"user","content":"Fix the calc tests"}`
	readStep     = `,{"role":"assistant","content":[{"type":"tool_use","id":"r1","name":"Read","input":{"file_path":"calc.go"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"r1","content":"package calc"}]}`
	failStep1    = `,{"role":"assistant","content":[{"type":"tool_use","id":"b1","name":"Bash","input":{}},{"type":"tool_use","id":"b2","name":"Bash","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"b1","is_error":true,"content":"Exit code 1"},{"type":"tool_result","tool_use_id":"b2","content":"--- FAIL: TestAdd"}]}`
	failStep2    = `,{"role":"assistant","content":[{"type":"tool_use","id":"b3","name":"Bash","input":{}},{"type":"tool_use","id":"b4","name":"Bash","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"b3","is_error":true,"content":"Exit code 1"},{"type":"tool_result","tool_use_id":"b4","is_error":true,"content":"Exit code 2"}]}`
)

func TestGuidedDirectorSteersExecutors(t *testing.T) {
	e, calls := setupGuided(t, provider.Anthropic)
	// A side query without tools, such as title generation, skips the director.
	resp, out := send(t, http.MethodPost, e.url+"/v1/messages", `{"model":"claude-guided","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"Write a title"}]}`,
		map[string]string{"X-Intelly-Key": e.key, "X-Claude-Code-Session-Id": "s1"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("side query status %d: %s", resp.StatusCode, out)
	}
	sendGuided(t, e, guidedPrompt)
	sendGuided(t, e, guidedPrompt+readStep)
	sendGuided(t, e, guidedPrompt+readStep+failStep1)
	sendGuided(t, e, guidedPrompt+readStep+failStep1+failStep2)

	want := []guidedCall{
		{model: "deepseek-v4-flash", auth: "Bearer sk-deepseek", stream: true},
		{model: "claude-fable-5-1", director: true},
		{model: "deepseek-v4-flash", auth: "Bearer sk-deepseek", guidance: true, stream: true, consult: true},
		{model: "deepseek-v4-flash", auth: "Bearer sk-deepseek", guidance: true, stream: true, consult: true},
		{model: "claude-fable-5-1", director: true},
		{model: "deepseek-v4-flash", auth: "Bearer sk-deepseek", guidance: true, stream: true, consult: true},
		{model: "claude-fable-5-1", director: true},
		{model: "deepseek-v4-pro", auth: "Bearer sk-deepseek", guidance: true, stream: true, consult: true},
	}
	got := calls()
	// The API director uses the provider key; the Claude login never reaches it.
	for i := range got {
		if got[i].director && got[i].auth != "" {
			t.Fatalf("director call %d sent Authorization %q", i, got[i].auth)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("upstream calls:\n got %+v\nwant %+v", got, want)
	}

	legs := requestLegs(t, e.store)
	if len(legs) != 5 || len(legs[0]) != 1 || legs[1][0].Role != ledger.RoleDirector || !strings.Contains(legs[1][0].Note, "checkpoint: turn start") ||
		!strings.Contains(legs[1][1].Note, "tier flash; guidance from turn start") ||
		!strings.Contains(legs[4][1].Note, "tier pro (moved up after repeated failures)") {
		t.Fatalf("legs = %+v", legs)
	}
}

func TestGuidedSubscriptionDirectorTakesTheStep(t *testing.T) {
	e, calls := setupGuided(t, provider.AnthropicSubscription)
	sendGuided(t, e, guidedPrompt)
	sendGuided(t, e, guidedPrompt+readStep)

	want := []guidedCall{
		{model: "claude-fable-5-1", auth: claudeLogin, stream: true},
		{model: "deepseek-v4-flash", auth: "Bearer sk-deepseek", stream: true},
	}
	if got := calls(); !slices.Equal(got, want) {
		t.Fatalf("upstream calls:\n got %+v\nwant %+v", got, want)
	}
	legs := requestLegs(t, e.store)
	if legs[0][0].Role != ledger.RoleDirector || legs[0][0].Billing != ledger.BillingSubscription || !strings.Contains(legs[0][0].Note, "director step: turn start") {
		t.Fatalf("director leg = %+v", legs[0][0])
	}
}

func TestGuidedExecutorAsksDirector(t *testing.T) {
	e, calls := setupGuided(t, provider.Anthropic)
	out := string(sendGuided(t, e, `{"role":"user","content":"ASK_TEST fix calc"}`))

	want := []guidedCall{
		{model: "claude-fable-5-1", director: true},
		{model: "deepseek-v4-flash", auth: "Bearer sk-deepseek", guidance: true, stream: true, consult: true},
		{model: "claude-fable-5-1", director: true, guidance: true},
		{model: "deepseek-v4-flash", auth: "Bearer sk-deepseek", guidance: true, stream: true, consult: true, answered: true},
	}
	if got := calls(); !slices.Equal(got, want) {
		t.Fatalf("upstream calls:\n got %+v\nwant %+v", got, want)
	}
	switch {
	case strings.Contains(out, "ask_director"):
		t.Fatalf("the hidden question reached Claude Code:\n%s", out)
	case strings.Count(out, "event: message_start") != 1 || strings.Count(out, "event: message_stop") != 1:
		t.Fatalf("client stream is not one message:\n%s", out)
	case !strings.Contains(out, "Checking the code.") || !strings.Contains(out, "calc.go has the bug.") || !strings.Contains(out, `"stop_reason":"end_turn"`):
		t.Fatalf("client stream misses the continuation:\n%s", out)
	}

	legs := requestLegs(t, e.store)
	if len(legs) != 1 || len(legs[0]) != 4 || legs[0][2].Role != ledger.RoleDirector ||
		!strings.Contains(legs[0][2].Note, "question: Which file has the bug?") ||
		!strings.Contains(legs[0][3].Note, "continued after the director's answer") {
		t.Fatalf("legs = %+v", legs)
	}
}
