package gateway_test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"intellyrouter/internal/ledger"
	"intellyrouter/internal/provider"
	"intellyrouter/internal/store"
)

func TestStepsMoveToATierWithRoom(t *testing.T) {
	fixture, err := os.ReadFile("../../testdata/anthropic/stream_tool_use.sse")
	must(t, err)
	var mu sync.Mutex
	var models []string
	e := setup(t, provider.Anthropic, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		models = append(models, body.Model)
		mu.Unlock()
		if body.Model == "overflowing-model" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 250000 tokens > 200000 maximum"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(fixture)
	})
	ctx := t.Context()
	providers, err := e.store.ListProviders(ctx)
	must(t, err)
	model := func(id string, window int64) store.Model {
		m, err := e.store.CreateModel(ctx, store.Model{ProviderID: providers[0].ID, ModelID: id, Context: window, Enabled: true})
		must(t, err)
		return m
	}
	small, overflowing, big := model("small-model", 1000), model("overflowing-model", 0), model("big-model", 0)
	for name, tiers := range map[string][]store.Tier{
		"claude-fit":      {{ModelID: small.ID, Label: "small"}, {ModelID: big.ID, Label: "big"}},
		"claude-overflow": {{ModelID: overflowing.ID, Label: "overflowing"}, {ModelID: big.ID, Label: "big"}},
	} {
		_, err := e.store.CreateRoute(ctx, store.Route{Name: name, Strategy: store.StrategyEscalate, Tiers: tiers, Settings: "{}"})
		must(t, err)
	}
	long := strings.Repeat("x", 20000)
	for _, name := range []string{"claude-fit", "claude-overflow"} {
		body := `{"model":"` + name + `","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"` + long + `"}]}`
		if resp, out := send(t, http.MethodPost, e.url+"/v1/messages", body, map[string]string{"X-Intelly-Key": e.key}); resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d: %s", name, resp.StatusCode, out)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if want := []string{"big-model", "overflowing-model", "big-model"}; !slices.Equal(models, want) {
		t.Fatalf("upstream models = %v, want %v", models, want)
	}
	legs := requestLegs(t, e.store)
	switch {
	case len(legs) != 2 || len(legs[0]) != 1 || !strings.Contains(legs[0][0].Note, "do not fit small-model"):
		t.Fatalf("estimated move: legs = %+v", legs)
	case len(legs[1]) != 2 || legs[1][0].Status == ledger.StatusOK || !strings.Contains(legs[1][0].Note, "prompt too long for overflowing-model") ||
		legs[1][1].Status != ledger.StatusOK || legs[1][1].Model != "big-model" || !strings.Contains(legs[1][1].Note, "after a context overflow"):
		t.Fatalf("overflow retry: legs = %+v", legs[1])
	}
}
