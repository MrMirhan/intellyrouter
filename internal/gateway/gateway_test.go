package gateway_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"intellyrouter/internal/gateway"
	"intellyrouter/internal/ledger"
	"intellyrouter/internal/provider"
	"intellyrouter/internal/store"
)

const route = "intelly-claude-fast"

// The first system block mimics Claude Code's attribution block, which must stay first.
const streamBody = `{"model":"intelly-claude-fast","max_tokens":1024,"stream":true,` +
	`"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1"},{"type":"text","text":"You are Claude Code.","cache_control":{"type":"ephemeral"}}],` +
	`"messages":[{"role":"user","content":"fix <b>this</b> & that"}]}`

type env struct {
	url    string
	store  *store.Store
	key    string
	dbPath string
}

func setup(t *testing.T, typ provider.Type, upstream http.HandlerFunc) env {
	t.Helper()
	up := httptest.NewServer(upstream)
	t.Cleanup(up.Close)

	dbPath := filepath.Join(t.TempDir(), "gw.db")
	st, err := store.Open(dbPath, bytes.Repeat([]byte{3}, 32))
	must(t, err)
	t.Cleanup(func() { st.Close() })
	ctx := t.Context()
	p, err := st.CreateProvider(ctx, store.Provider{Type: string(typ), Name: "up", BaseURL: up.URL, APIKey: "sk-upstream", Enabled: true})
	must(t, err)
	m, err := st.CreateModel(ctx, store.Model{
		ProviderID: p.ID, ModelID: "claude-haiku-4-5", Enabled: true,
		PriceIn: 1, PriceOut: 5, PriceCacheRead: 0.1, PriceCacheWrite: 1.25,
	})
	must(t, err)
	_, err = st.CreateRoute(ctx, store.Route{Name: route, Strategy: store.StrategyDirect, Tiers: []store.Tier{{ModelID: m.ID, Label: "haiku"}}})
	must(t, err)
	key, _, err := st.CreateGatewayKey(ctx, "test")
	must(t, err)

	log := slog.New(slog.DiscardHandler)
	mux := http.NewServeMux()
	gateway.New(st, ledger.NewRecorder(st, log), up.Client(), log).Register(mux)
	gw := httptest.NewServer(mux)
	t.Cleanup(gw.Close)
	return env{url: gw.URL, store: st, key: key, dbPath: dbPath}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func send(t *testing.T, method, url, body string, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, url, strings.NewReader(body))
	must(t, err)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	must(t, err)
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	must(t, err)
	return resp, out
}

func onlyRequest(t *testing.T, st *store.Store) store.Request {
	t.Helper()
	items, total, err := st.ListRequests(t.Context(), store.RequestFilter{})
	must(t, err)
	if total != 1 {
		t.Fatalf("ledger has %d requests, want 1", total)
	}
	req, err := st.GetRequest(t.Context(), items[0].ID)
	must(t, err)
	return req
}

func TestDirectStreamIsRelayedByteForByte(t *testing.T) {
	fixture, err := os.ReadFile("../../testdata/anthropic/stream_tool_use.sse")
	must(t, err)
	var seen struct {
		path, query, apiKey, auth, intellyKey, beta, session string
		body                                                 []byte
	}
	e := setup(t, provider.Anthropic, func(w http.ResponseWriter, r *http.Request) {
		seen.path, seen.query = r.URL.Path, r.URL.RawQuery
		seen.apiKey, seen.auth, seen.intellyKey = r.Header.Get("X-Api-Key"), r.Header.Get("Authorization"), r.Header.Get("X-Intelly-Key")
		seen.beta, seen.session = r.Header.Get("Anthropic-Beta"), r.Header.Get("X-Claude-Code-Session-Id")
		seen.body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Request-Id", "req_123")
		_, _ = w.Write(fixture)
	})

	resp, out := send(t, http.MethodPost, e.url+"/v1/messages?beta=true", streamBody, map[string]string{
		"X-Intelly-Key":            e.key,
		"Authorization":            "Bearer client-oauth-token",
		"Anthropic-Beta":           "claude-code-20250219,context-1m-2025-08-07",
		"Anthropic-Version":        "2023-06-01",
		"X-Claude-Code-Session-Id": "sess-1",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, out)
	}
	if !bytes.Equal(out, fixture) {
		t.Fatalf("client stream differs from upstream stream:\n%s", out)
	}
	if resp.Header.Get("Request-Id") != "req_123" {
		t.Error("upstream response header not relayed")
	}

	wantBody := strings.Replace(streamBody, `"model":"intelly-claude-fast"`, `"model":"claude-haiku-4-5"`, 1)
	switch {
	case seen.path != "/v1/messages" || seen.query != "beta=true":
		t.Errorf("upstream URL = %s?%s", seen.path, seen.query)
	case seen.apiKey != "sk-upstream":
		t.Errorf("upstream x-api-key = %q", seen.apiKey)
	case seen.auth != "" || seen.intellyKey != "":
		t.Errorf("client credentials leaked upstream: authorization=%q x-intelly-key=%q", seen.auth, seen.intellyKey)
	case seen.beta != "claude-code-20250219,context-1m-2025-08-07":
		t.Errorf("anthropic-beta = %q", seen.beta)
	case seen.session != "":
		t.Errorf("x-claude-code-session-id forwarded upstream")
	case string(seen.body) != wantBody:
		t.Errorf("upstream body:\n got %s\nwant %s", seen.body, wantBody)
	}

	req := onlyRequest(t, e.store)
	if req.Status != ledger.StatusOK || req.HTTPStatus != 200 || req.SessionID != "sess-1" || !req.Stream || len(req.Legs) != 1 {
		t.Fatalf("ledger request = %+v", req)
	}
	leg := req.Legs[0]
	if leg.InputTokens != 1200 || leg.OutputTokens != 87 || leg.CacheReadTokens != 5000 || leg.CacheWriteTokens != 300 || leg.StopReason != "tool_use" {
		t.Fatalf("ledger leg = %+v", leg)
	}
	if want := 2510.0 / 1e6; math.Abs(req.CostUSD-want) > 1e-12 {
		t.Errorf("cost = %v, want %v", req.CostUSD, want)
	}
	// Default reference model is Claude Fable 5.1: 10/50/0.25/12.5 per 1M tokens.
	if want := 21350.0 / 1e6; math.Abs(req.ReferenceCostUSD-want) > 1e-12 {
		t.Errorf("reference cost = %v, want %v", req.ReferenceCostUSD, want)
	}
}

func TestUpstreamErrorIsPassedThrough(t *testing.T) {
	const errBody = `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`
	e := setup(t, provider.Anthropic, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, errBody)
	})
	resp, out := send(t, http.MethodPost, e.url+"/v1/messages", streamBody, map[string]string{"X-Api-Key": e.key})
	if resp.StatusCode != http.StatusTooManyRequests || resp.Header.Get("Retry-After") != "7" || string(out) != errBody {
		t.Fatalf("got %d retry-after=%q body=%s", resp.StatusCode, resp.Header.Get("Retry-After"), out)
	}
	req := onlyRequest(t, e.store)
	if req.Status != ledger.StatusUpstreamError || req.HTTPStatus != 429 || req.Error != "rate_limit_error: slow down" {
		t.Fatalf("ledger request = %+v", req)
	}
}

func TestOpenRouterUsesBearerAuth(t *testing.T) {
	e := setup(t, provider.OpenRouter, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-upstream" || r.Header.Get("X-Api-Key") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2}}`)
	})
	body := strings.Replace(streamBody, `"stream":true`, `"stream":false`, 1)
	resp, out := send(t, http.MethodPost, e.url+"/v1/messages", body, map[string]string{"Authorization": "Bearer " + e.key})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, out)
	}
	leg := onlyRequest(t, e.store).Legs[0]
	if leg.InputTokens != 3 || leg.OutputTokens != 2 || leg.StopReason != "end_turn" {
		t.Fatalf("ledger leg = %+v", leg)
	}
}

func TestRejectsBeforeCallingUpstream(t *testing.T) {
	e := setup(t, provider.Anthropic, func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream must not be called")
	})
	cases := []struct {
		name      string
		body      string
		headers   map[string]string
		status    int
		errorType string
	}{
		{"missing key", streamBody, nil, 401, "authentication_error"},
		{"wrong key", streamBody, map[string]string{"X-Api-Key": "ik_wrong"}, 401, "authentication_error"},
		{"unknown route", `{"model":"claude-unknown","messages":[]}`, map[string]string{"X-Api-Key": e.key}, 404, "not_found_error"},
		{"no model", `{"messages":[]}`, map[string]string{"X-Api-Key": e.key}, 400, "invalid_request_error"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp, out := send(t, http.MethodPost, e.url+"/v1/messages", c.body, c.headers)
			var got struct {
				Type  string `json:"type"`
				Error struct {
					Type string `json:"type"`
				} `json:"error"`
			}
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("error body is not JSON: %s", out)
			}
			if resp.StatusCode != c.status || got.Type != "error" || got.Error.Type != c.errorType {
				t.Fatalf("got %d %s", resp.StatusCode, out)
			}
		})
	}
}

func TestModelsListsRoutes(t *testing.T) {
	e := setup(t, provider.Anthropic, func(http.ResponseWriter, *http.Request) {})
	resp, out := send(t, http.MethodGet, e.url+"/v1/models?limit=1000", "", map[string]string{"X-Api-Key": e.key})
	var got struct {
		Data []struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"data"`
		FirstID string `json:"first_id"`
	}
	if err := json.Unmarshal(out, &got); err != nil || resp.StatusCode != 200 {
		t.Fatalf("got %d %s", resp.StatusCode, out)
	}
	if len(got.Data) != 1 || got.Data[0].ID != route || got.Data[0].Type != "model" || got.FirstID != route {
		t.Fatalf("models = %s", out)
	}
}

func TestCountTokensIsForwardedWithoutLedgerEntry(t *testing.T) {
	e := setup(t, provider.Anthropic, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages/count_tokens" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"input_tokens":42}`)
	})
	resp, out := send(t, http.MethodPost, e.url+"/v1/messages/count_tokens", `{"model":"intelly-claude-fast","messages":[]}`, map[string]string{"X-Api-Key": e.key})
	if resp.StatusCode != 200 || string(out) != `{"input_tokens":42}` {
		t.Fatalf("got %d %s", resp.StatusCode, out)
	}
	if _, total, _ := e.store.ListRequests(t.Context(), store.RequestFilter{}); total != 0 {
		t.Fatalf("count_tokens created %d ledger entries", total)
	}
}
