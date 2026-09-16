package gateway_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/MrMirhan/intellyrouter/internal/gateway"
	"github.com/MrMirhan/intellyrouter/internal/ledger"
	"github.com/MrMirhan/intellyrouter/internal/provider"
	"github.com/MrMirhan/intellyrouter/internal/store"
)

type upstreamCall struct {
	model, auth, apiKey string
	classifier          bool
}

// setupEscalate builds a route with DeepSeek flash as the base tier and
// classifier, and Claude Opus 5 on the subscription as the top tier.
func setupEscalate(t *testing.T) (env, func() []upstreamCall) {
	t.Helper()
	fixture, err := os.ReadFile("../../testdata/anthropic/stream_tool_use.sse")
	must(t, err)
	var mu sync.Mutex
	var calls []upstreamCall
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		_ = json.Unmarshal(raw, &body)
		c := upstreamCall{
			model: body.Model, auth: r.Header.Get("Authorization"), apiKey: r.Header.Get("X-Api-Key"),
			classifier: !body.Stream && bytes.Contains(raw, []byte("You route requests")),
		}
		mu.Lock()
		calls = append(calls, c)
		mu.Unlock()
		if c.classifier {
			verdict := `{\"escalate\": false, \"reason\": \"routine request\"}`
			if bytes.Contains(raw, []byte("beğenmedim")) {
				verdict = `{\"escalate\": true, \"reason\": \"user is unhappy\"}`
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"type":"message","role":"assistant","content":[{"type":"text","text":"`+verdict+`"}],"stop_reason":"end_turn","usage":{"input_tokens":50,"output_tokens":10}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(fixture)
	}))
	t.Cleanup(up.Close)

	dbPath := filepath.Join(t.TempDir(), "esc.db")
	st, err := store.Open(dbPath, bytes.Repeat([]byte{4}, 32))
	must(t, err)
	t.Cleanup(func() { st.Close() })
	ctx := t.Context()
	ds, err := st.CreateProvider(ctx, store.Provider{Type: string(provider.AnthropicCompatible), Name: "deepseek", BaseURL: up.URL, APIKey: "sk-deepseek", Enabled: true})
	must(t, err)
	claude, err := st.CreateProvider(ctx, store.Provider{Type: string(provider.AnthropicSubscription), Name: "claude", BaseURL: up.URL, Enabled: true})
	must(t, err)
	flash, err := st.CreateModel(ctx, store.Model{ProviderID: ds.ID, ModelID: "deepseek-v4-flash", PriceIn: 0.3, PriceOut: 1.2, Enabled: true})
	must(t, err)
	opus, err := st.CreateModel(ctx, store.Model{ProviderID: claude.ID, ModelID: "claude-opus-5", Enabled: true})
	must(t, err)
	settings := fmt.Sprintf(`{"classifier":{"enabled":true,"model_id":%d,"target":"top"},"failure_streak":{"enabled":true,"threshold":2,"target":"next"}}`, flash.ID)
	_, err = st.CreateRoute(ctx, store.Route{
		Name: "intelly-claude-auto", Strategy: store.StrategyEscalate, Settings: settings,
		Tiers: []store.Tier{{ModelID: flash.ID, Label: "flash"}, {ModelID: opus.ID, Label: "opus"}},
	})
	must(t, err)
	key, _, err := st.CreateGatewayKey(ctx, "test")
	must(t, err)

	log := slog.New(slog.DiscardHandler)
	mux := http.NewServeMux()
	gateway.New(st, ledger.NewRecorder(st, log), up.Client(), log).Register(mux)
	gw := httptest.NewServer(mux)
	t.Cleanup(gw.Close)
	return env{url: gw.URL, store: st, key: key, dbPath: dbPath}, func() []upstreamCall {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(calls)
	}
}

func turnBody(messages string) string {
	return `{"model":"intelly-claude-auto","max_tokens":1024,"stream":true,"messages":[` + messages + `]}`
}

func sendTurn(t *testing.T, e env, messages string) {
	t.Helper()
	resp, out := send(t, http.MethodPost, e.url+"/v1/messages?beta=true", turnBody(messages), map[string]string{
		"X-Intelly-Key": e.key, "Authorization": claudeLogin, "X-Claude-Code-Session-Id": "s1",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, out)
	}
}

func requestLegs(t *testing.T, st *store.Store) [][]store.Leg {
	t.Helper()
	items, _, err := st.ListRequests(t.Context(), store.RequestFilter{})
	must(t, err)
	var out [][]store.Leg
	for i := len(items) - 1; i >= 0; i-- {
		req, err := st.GetRequest(t.Context(), items[i].ID)
		must(t, err)
		out = append(out, req.Legs)
	}
	return out
}

func TestEscalateClassifierRunsOncePerTurn(t *testing.T) {
	e, calls := setupEscalate(t)
	const prompt = `{"role":"user","content":"add a login page"}`
	sendTurn(t, e, prompt)
	sendTurn(t, e, prompt+`,{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}`)
	sendTurn(t, e, prompt+`,{"role":"assistant","content":"Done."},{"role":"user","content":"bunu beğenmedim, düzelt"}`)

	got := calls()
	want := []upstreamCall{
		{model: "deepseek-v4-flash", auth: "Bearer sk-deepseek", apiKey: "sk-deepseek", classifier: true},
		{model: "deepseek-v4-flash", auth: "Bearer sk-deepseek", apiKey: "sk-deepseek"},
		{model: "deepseek-v4-flash", auth: "Bearer sk-deepseek", apiKey: "sk-deepseek"},
		{model: "deepseek-v4-flash", auth: "Bearer sk-deepseek", apiKey: "sk-deepseek", classifier: true},
		{model: "claude-opus-5", auth: claudeLogin},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("upstream calls:\n got %+v\nwant %+v", got, want)
	}

	legs := requestLegs(t, e.store)
	if len(legs) != 3 || len(legs[0]) != 2 || len(legs[1]) != 1 || len(legs[2]) != 2 {
		t.Fatalf("legs per request = %+v", legs)
	}
	if legs[0][0].Role != ledger.RoleClassifier || legs[0][1].Role != ledger.RoleExecutor || !strings.Contains(legs[0][1].Note, "tier flash: base") {
		t.Errorf("first request legs = %+v", legs[0])
	}
	esc := legs[2][1]
	if esc.Role != ledger.RoleEscalation || esc.Billing != ledger.BillingSubscription || !strings.Contains(esc.Note, "classifier: user is unhappy") {
		t.Errorf("escalated leg = %+v", esc)
	}

	items, _, err := e.store.ListRequests(t.Context(), store.RequestFilter{})
	must(t, err)
	first := items[len(items)-1]
	// Base tokens priced at Opus 5 (5/25/0.5/6.25) for the savings estimate.
	if want := 12550.0 / 1e6; math.Abs(first.ReferenceCostUSD-want) > 1e-12 {
		t.Errorf("reference cost = %v, want %v", first.ReferenceCostUSD, want)
	}
	// Classifier 50/10 tokens plus executor 1200/87 tokens at 0.3/1.2.
	if want := (27.0 + 464.4) / 1e6; math.Abs(first.CostUSD-want) > 1e-12 {
		t.Errorf("api cost = %v, want %v", first.CostUSD, want)
	}
}

func TestEscalateMarkerAndFailureStreak(t *testing.T) {
	e, calls := setupEscalate(t)
	sendTurn(t, e, `{"role":"user","content":"#opus refactor the auth module"}`)
	sendTurn(t, e, `{"role":"user","content":"run the tests"},`+
		`{"role":"assistant","content":[{"type":"tool_use","id":"a","name":"Bash","input":{}}]},`+
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"a","is_error":true,"content":"fail"}]},`+
		`{"role":"assistant","content":[{"type":"tool_use","id":"b","name":"Bash","input":{}}]},`+
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"b","is_error":true,"content":"fail"}]}`)

	got := calls()
	if len(got) != 3 || got[0].model != "claude-opus-5" || got[0].classifier ||
		!got[1].classifier || got[2].model != "claude-opus-5" {
		t.Fatalf("upstream calls = %+v", got)
	}
	legs := requestLegs(t, e.store)
	if !strings.Contains(legs[0][0].Note, "marker #opus") || !strings.Contains(legs[1][1].Note, "2 failed tool calls in a row") {
		t.Fatalf("notes = %q, %q", legs[0][0].Note, legs[1][1].Note)
	}
}
