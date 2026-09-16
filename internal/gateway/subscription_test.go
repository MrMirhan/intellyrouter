package gateway_test

import (
	"bytes"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/MrMirhan/intellyrouter/internal/ledger"
	"github.com/MrMirhan/intellyrouter/internal/provider"
	"github.com/MrMirhan/intellyrouter/internal/store"
)

const claudeLogin = "Bearer sk-ant-oat01-client-login"

func TestSubscriptionRoutePassesClaudeLoginThrough(t *testing.T) {
	fixture, err := os.ReadFile("../../testdata/anthropic/stream_tool_use.sse")
	must(t, err)
	var seen struct {
		auth, apiKey, intellyKey, beta string
		body                           []byte
	}
	e := setup(t, provider.AnthropicSubscription, func(w http.ResponseWriter, r *http.Request) {
		seen.auth, seen.apiKey, seen.intellyKey = r.Header.Get("Authorization"), r.Header.Get("X-Api-Key"), r.Header.Get("X-Intelly-Key")
		seen.beta = r.Header.Get("Anthropic-Beta")
		seen.body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Anthropic-Ratelimit-Unified-Status", "allowed")
		w.Header().Set("Anthropic-Ratelimit-Unified-5h-Utilization", "0.42")
		_, _ = w.Write(fixture)
	})

	resp, out := send(t, http.MethodPost, e.url+"/v1/messages?beta=true", streamBody, map[string]string{
		"X-Intelly-Key":  e.key,
		"Authorization":  claudeLogin,
		"Anthropic-Beta": "oauth-2025-04-20,claude-code-20250219",
	})
	if resp.StatusCode != http.StatusOK || !bytes.Equal(out, fixture) {
		t.Fatalf("got %d: %s", resp.StatusCode, out)
	}
	wantBody := strings.Replace(streamBody, `"model":"intelly-claude-fast"`, `"model":"claude-haiku-4-5"`, 1)
	switch {
	case seen.auth != claudeLogin:
		t.Errorf("upstream authorization = %q, want the client's Claude login", seen.auth)
	case seen.apiKey != "" || seen.intellyKey != "":
		t.Errorf("stored key or gateway key sent upstream: x-api-key=%q x-intelly-key=%q", seen.apiKey, seen.intellyKey)
	case seen.beta != "oauth-2025-04-20,claude-code-20250219":
		t.Errorf("anthropic-beta = %q", seen.beta)
	case string(seen.body) != wantBody:
		t.Errorf("upstream body:\n got %s\nwant %s", seen.body, wantBody)
	}

	req := onlyRequest(t, e.store)
	if req.CostUSD != 0 || req.ReferenceCostUSD != 0 || math.Abs(req.SubscriptionValueUSD-2510.0/1e6) > 1e-12 {
		t.Errorf("costs: api=%v reference=%v subscription=%v", req.CostUSD, req.ReferenceCostUSD, req.SubscriptionValueUSD)
	}
	if req.Legs[0].Billing != ledger.BillingSubscription {
		t.Errorf("leg billing = %q", req.Legs[0].Billing)
	}

	limits, ok, err := e.store.Setting(t.Context(), store.SubscriptionLimitsSetting)
	if err != nil || !ok || !strings.Contains(limits, `"anthropic-ratelimit-unified-5h-utilization":"0.42"`) {
		t.Errorf("rate limit snapshot = %q, %v, %v", limits, ok, err)
	}

	// The login must never be written to disk.
	for _, path := range []string{e.dbPath, e.dbPath + "-wal"} {
		b, err := os.ReadFile(path)
		if err == nil && bytes.Contains(b, []byte("sk-ant-oat01")) {
			t.Fatalf("Claude login token found in %s", path)
		}
	}
}

func TestSubscriptionRouteRequiresClaudeLogin(t *testing.T) {
	e := setup(t, provider.AnthropicSubscription, func(http.ResponseWriter, *http.Request) {
		t.Error("upstream must not be called")
	})
	for _, headers := range []map[string]string{
		{"Authorization": "Bearer " + e.key},
		{"X-Api-Key": e.key},
	} {
		resp, out := send(t, http.MethodPost, e.url+"/v1/messages", streamBody, headers)
		if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(string(out), "x-intelly-key") {
			t.Fatalf("headers %v: got %d %s", headers, resp.StatusCode, out)
		}
	}
	items, total, err := e.store.ListRequests(t.Context(), store.RequestFilter{})
	must(t, err)
	if total != 2 || items[0].Status != ledger.StatusError || items[0].HTTPStatus != http.StatusUnauthorized {
		t.Fatalf("ledger = %+v", items)
	}
}
