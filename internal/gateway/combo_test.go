package gateway_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/MrMirhan/intellyrouter/internal/gateway"
	"github.com/MrMirhan/intellyrouter/internal/ledger"
	"github.com/MrMirhan/intellyrouter/internal/provider"
	"github.com/MrMirhan/intellyrouter/internal/store"
)

type comboEnv struct {
	url    string
	key    string
	store  *store.Store
	combo  store.Combo
	calls  func() []string
	failA  *atomic.Bool
	models map[string]int64
}

// setupCombo builds a MiniMax provider with the slug "mm", two models, a
// fallback combo "stack" of both, and a route "claude-combo" on the combo.
// Model minimax-a answers 429 while failA is set.
func setupCombo(t *testing.T) comboEnv {
	t.Helper()
	fixture, err := os.ReadFile("../../testdata/anthropic/stream_tool_use.sse")
	must(t, err)
	var mu sync.Mutex
	var calls []string
	failA := &atomic.Bool{}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		calls = append(calls, body.Model)
		mu.Unlock()
		if body.Model == "minimax-a" && failA.Load() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(fixture)
	}))
	t.Cleanup(up.Close)

	st, err := store.Open(filepath.Join(t.TempDir(), "combo.db"), bytes.Repeat([]byte{4}, 32))
	must(t, err)
	t.Cleanup(func() { st.Close() })
	ctx := t.Context()
	p, err := st.CreateProvider(ctx, store.Provider{Type: string(provider.AnthropicCompatible), Name: "MiniMax", Slug: "mm", BaseURL: up.URL, APIKey: "sk-mm", Enabled: true})
	must(t, err)
	models := map[string]int64{}
	for _, id := range []string{"minimax-a", "minimax-b"} {
		m, err := st.CreateModel(ctx, store.Model{ProviderID: p.ID, ModelID: id, PriceIn: 0.3, PriceOut: 1.2, Enabled: true})
		must(t, err)
		models[id] = m.ID
	}
	combo, err := st.CreateCombo(ctx, store.Combo{Name: "stack", Strategy: store.ComboFallback, Enabled: true, Members: []store.ComboMember{{ModelID: models["minimax-a"], Weight: 1}, {ModelID: models["minimax-b"], Weight: 1}}})
	must(t, err)
	_, err = st.CreateRoute(ctx, store.Route{Name: "claude-combo", Strategy: store.StrategyDirect, Tiers: []store.Tier{{ModelID: combo.ID, Label: "stack"}}})
	must(t, err)
	key, _, err := st.CreateGatewayKey(ctx, "test")
	must(t, err)
	log := slog.New(slog.DiscardHandler)
	mux := http.NewServeMux()
	gateway.New(st, ledger.NewRecorder(st, log), up.Client(), log).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return comboEnv{url: srv.URL, key: key, store: st, combo: combo, failA: failA, models: models, calls: func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := slices.Clone(calls)
		calls = nil
		return out
	}}
}

func (e comboEnv) post(t *testing.T, model string) (int, string) {
	t.Helper()
	body := `{"model":"` + model + `","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"hi"}]}`
	resp, out := send(t, http.MethodPost, e.url+"/v1/messages", body, map[string]string{"X-Intelly-Key": e.key})
	return resp.StatusCode, string(out)
}

func (e comboEnv) postImage(t *testing.T, model string) (int, string) {
	t.Helper()
	body := `{"model":"` + model + `","max_tokens":64,"stream":true,"messages":[{"role":"user","content":[` +
		`{"type":"image","source":{"type":"base64","media_type":"image/png","data":"iVBORw0KGgo="}},` +
		`{"type":"text","text":"what is this"}]}]}`
	resp, out := send(t, http.MethodPost, e.url+"/v1/messages", body, map[string]string{"X-Intelly-Key": e.key})
	return resp.StatusCode, string(out)
}

// A combo takes images when any member does, so an image request must reach
// only the members that read one.
func TestComboSkipsMembersThatDoNotReadImages(t *testing.T) {
	e := setupCombo(t)
	ctx := t.Context()
	seeing, err := e.store.GetModel(ctx, e.models["minimax-b"])
	must(t, err)
	seeing.Vision = true
	must(t, e.store.UpdateModel(ctx, seeing))
	must(t, e.store.UpdateCombo(ctx, store.Combo{ID: e.combo.ID, Name: "stack", Strategy: store.ComboRoundRobin, Enabled: true, Members: e.combo.Members}))

	// minimax-a comes first in the rotation and cannot read images.
	for range 4 {
		if status, out := e.postImage(t, "claude-combo"); status != http.StatusOK {
			t.Fatalf("status %d: %s", status, out)
		}
	}
	for _, model := range e.calls() {
		if model != "minimax-b" {
			t.Fatalf("an image request reached %s, which does not read images", model)
		}
	}
	leg := requestLegs(t, e.store)[0][0]
	if !strings.Contains(leg.Note, "skipped 1 that do not read one") {
		t.Fatalf("leg note = %q", leg.Note)
	}

	// A request without an image still uses every member.
	for range 4 {
		if status, out := e.post(t, "claude-combo"); status != http.StatusOK {
			t.Fatalf("status %d: %s", status, out)
		}
	}
	if got := e.calls(); !slices.Contains(got, "minimax-a") {
		t.Fatalf("calls without an image = %v, want both members", got)
	}
}

// With no member that reads images the combo tries them all, so the upstream
// reports the problem instead of the gateway refusing the request.
func TestComboWithoutVisionStillTries(t *testing.T) {
	e := setupCombo(t)
	if status, out := e.postImage(t, "claude-combo"); status != http.StatusOK {
		t.Fatalf("status %d: %s", status, out)
	}
	if got := e.calls(); len(got) == 0 {
		t.Fatal("no upstream call for an image request on a combo with no vision member")
	}
}

func TestComboFallsBackToTheNextModel(t *testing.T) {
	e := setupCombo(t)
	e.failA.Store(true)
	if status, out := e.post(t, "claude-combo"); status != http.StatusOK || strings.Contains(out, "rate_limit_error") {
		t.Fatalf("status %d: %s", status, out)
	}
	if got := e.calls(); !slices.Equal(got, []string{"minimax-a", "minimax-b"}) {
		t.Fatalf("upstream calls = %v", got)
	}
	leg := requestLegs(t, e.store)[0][0]
	if leg.Model != "minimax-b" || leg.Status != ledger.StatusOK || !strings.Contains(leg.Note, "combo stack after minimax-a returned 429") {
		t.Fatalf("leg = %+v", leg)
	}

	// When every model fails, the client sees the last model's error.
	must(t, e.store.UpdateCombo(t.Context(), store.Combo{ID: e.combo.ID, Name: "stack", Strategy: store.ComboFallback, Enabled: true, Members: []store.ComboMember{{ModelID: e.models["minimax-a"], Weight: 1}}}))
	if status, out := e.post(t, "claude-combo"); status != http.StatusTooManyRequests || !strings.Contains(out, "rate_limit_error") {
		t.Fatalf("status %d: %s", status, out)
	}
}

func TestComboRoundRobin(t *testing.T) {
	e := setupCombo(t)
	must(t, e.store.UpdateCombo(t.Context(), store.Combo{ID: e.combo.ID, Name: "stack", Strategy: store.ComboRoundRobin, Enabled: true, Members: e.combo.Members}))
	for range 4 {
		if status, out := e.post(t, "claude-combo"); status != http.StatusOK {
			t.Fatalf("status %d: %s", status, out)
		}
	}
	if got := e.calls(); !slices.Equal(got, []string{"minimax-a", "minimax-b", "minimax-a", "minimax-b"}) {
		t.Fatalf("upstream calls = %v", got)
	}
}

// Models and combos work as "<provider slug>/<model id>" without a route.
func TestDirectModelNames(t *testing.T) {
	e := setupCombo(t)
	if status, out := e.post(t, "mm/minimax-b"); status != http.StatusOK {
		t.Fatalf("mm/minimax-b: status %d: %s", status, out)
	}
	e.failA.Store(true)
	if status, out := e.post(t, "combo/stack"); status != http.StatusOK {
		t.Fatalf("combo/stack: status %d: %s", status, out)
	}
	if got := e.calls(); !slices.Equal(got, []string{"minimax-b", "minimax-a", "minimax-b"}) {
		t.Fatalf("upstream calls = %v", got)
	}
	if status, _ := e.post(t, "mm/unknown"); status != http.StatusNotFound {
		t.Fatalf("unknown model: status %d", status)
	}
	requests, _, err := e.store.ListRequests(t.Context(), store.RequestFilter{})
	must(t, err)
	var routes []string
	for _, r := range requests {
		routes = append(routes, r.Route)
	}
	if !slices.Contains(routes, "mm/minimax-b") || !slices.Contains(routes, "combo/stack") {
		t.Fatalf("ledger routes = %v", routes)
	}

	resp, out := send(t, http.MethodGet, e.url+"/v1/models", "", map[string]string{"X-Intelly-Key": e.key})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("models: status %d", resp.StatusCode)
	}
	for _, want := range []string{`"id":"claude-combo"`, `"id":"mm/minimax-a"`, `"id":"combo/stack"`} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("model list misses %s: %s", want, out)
		}
	}
}
