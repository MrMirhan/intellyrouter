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
	url     string
	key     string
	store   *store.Store
	combo   store.Combo
	calls   func() []string
	failA   *atomic.Bool
	truncA  *atomic.Bool
	refuseA *atomic.Bool
	emptyA  *atomic.Bool
	downA   *atomic.Bool
	oopsA   *atomic.Bool
	thinkA  *atomic.Bool
	models  map[string]int64
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
	truncA := &atomic.Bool{}
	refuseA := &atomic.Bool{}
	emptyA := &atomic.Bool{}
	downA := &atomic.Bool{}
	oopsA := &atomic.Bool{}
	thinkA := &atomic.Bool{}
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
		if body.Model == "minimax-a" && downA.Load() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"type":"server_error","message":"Error from provider (Console): Upstream request failed: Model is unavailable."}}`)
			return
		}
		if body.Model == "minimax-a" && oopsA.Load() {
			// A generic provider-side failure with no useful message. The body
			// type is the only thing that says the request was fine.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"server_error","message":"An error occurred while processing your request."}}`)
			return
		}
		if body.Model == "minimax-a" && thinkA.Load() {
			// A model that thinks for a while then errors mid-stream. The
			// thinking deltas must not commit the held response, so the combo
			// can still fail over when the error event arrives.
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w,
				"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"think\",\"role\":\"assistant\",\"model\":\"minimax-a\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n"+
					"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"text\":\"\"}}\n\n"+
					"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"I need to think about this carefully.\"}}\n\n"+
					"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\" More thoughts follow.\"}}\n\n"+
					"event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"server_error\",\"message\":\"Error from provider (Console): Upstream request failed: An error occurred while processing your request. Please contact us through our help center at help.openai.com if the error persists.\"}}\n\n")
			return
		}
		if body.Model == "minimax-a" && truncA.Load() {
			// A stream cut off after message_start: no message_stop, no content.
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: message_start\n"+
				`data: {"type":"message_start","message":{"id":"msg_cut","type":"message","role":"assistant","model":"minimax-a","content":[],"stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}`+"\n\n")
			return
		}
		if body.Model == "minimax-a" && refuseA.Load() {
			// A refusal with no content: the client saw nothing.
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: message_start\n"+
				`data: {"type":"message_start","message":{"id":"msg_ref","type":"message","role":"assistant","model":"minimax-a","content":[],"stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}`+"\n\n"+
				"event: message_delta\n"+
				`data: {"type":"message_delta","delta":{"stop_reason":"refusal","stop_details":{"type":"refusal","category":"cyber","explanation":"declined"}},"usage":{"output_tokens":0}}`+"\n\n"+
				"event: message_stop\n"+
				`data: {"type":"message_stop"}`+"\n\n")
			return
		}
		if body.Model == "minimax-a" && emptyA.Load() {
			// A complete stream that carries no content block at all.
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: message_start\n"+
				`data: {"type":"message_start","message":{"id":"msg_empty","type":"message","role":"assistant","model":"minimax-a","content":[],"stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}`+"\n\n"+
				"event: message_delta\n"+
				`data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":0}}`+"\n\n"+
				"event: message_stop\n"+
				`data: {"type":"message_stop"}`+"\n\n")
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
	return comboEnv{url: srv.URL, key: key, store: st, combo: combo,
		failA: failA, truncA: truncA, refuseA: refuseA, emptyA: emptyA, downA: downA, oopsA: oopsA, thinkA: thinkA,
		models: models, calls: func() []string {
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

// A member whose stream was cut off before it produced any content is not a
// usable answer: the combo moves to the next member, and the client never sees
// the broken stream.
func TestComboRetriesAMemberThatTruncatedTheStream(t *testing.T) {
	e := setupCombo(t)
	e.truncA.Store(true)
	status, out := e.post(t, "claude-combo")
	if status != http.StatusOK || !strings.Contains(out, "message_stop") {
		t.Fatalf("status %d: %s", status, out)
	}
	if got := e.calls(); !slices.Equal(got, []string{"minimax-a", "minimax-b"}) {
		t.Fatalf("upstream calls = %v", got)
	}
	leg := requestLegs(t, e.store)[0][0]
	if leg.Model != "minimax-b" || leg.Status != ledger.StatusOK {
		t.Fatalf("leg = %+v", leg)
	}
}

// A refusal that produced no content is retryable on another member: Anthropic
// says re-sending to the same model usually refuses again, so the different
// provider behind the next member is the way out.
func TestComboRetriesARefusalWithNoContent(t *testing.T) {
	e := setupCombo(t)
	e.refuseA.Store(true)
	status, out := e.post(t, "claude-combo")
	if status != http.StatusOK || !strings.Contains(out, "message_stop") {
		t.Fatalf("status %d: %s", status, out)
	}
	if got := e.calls(); !slices.Equal(got, []string{"minimax-a", "minimax-b"}) {
		t.Fatalf("upstream calls = %v", got)
	}
}

// A complete stream that carries no content block is an empty answer, which
// Claude Code reports as a malformed response. The combo retries it.
func TestComboRetriesAnEmptyAnswer(t *testing.T) {
	e := setupCombo(t)
	e.emptyA.Store(true)
	status, out := e.post(t, "claude-combo")
	if status != http.StatusOK || !strings.Contains(out, "message_stop") {
		t.Fatalf("status %d: %s", status, out)
	}
	if got := e.calls(); !slices.Equal(got, []string{"minimax-a", "minimax-b"}) {
		t.Fatalf("upstream calls = %v", got)
	}
}

// "Model is unavailable" arrives as a 400 from a transiently-down provider.
// The combo treats it like any other upstream failure and tries the next
// member instead of giving the client a hard error.
func TestComboRetriesAProviderThatReportsModelUnavailable(t *testing.T) {
	e := setupCombo(t)
	e.downA.Store(true)
	status, out := e.post(t, "claude-combo")
	if status != http.StatusOK || !strings.Contains(out, "message_stop") {
		t.Fatalf("status %d: %s", status, out)
	}
	if got := e.calls(); !slices.Equal(got, []string{"minimax-a", "minimax-b"}) {
		t.Fatalf("upstream calls = %v", got)
	}
}

// A model that thinks for a while then errors mid-stream with an `error`
// event must still let the combo fail over. Thinking deltas carry no
// user-visible text, so they must not commit the held response before the
// error arrives.
func TestComboRetriesWhenThinkingIsFollowedByError(t *testing.T) {
	e := setupCombo(t)
	e.thinkA.Store(true)
	status, out := e.post(t, "claude-combo")
	if status != http.StatusOK || !strings.Contains(out, "message_stop") {
		t.Fatalf("status %d: %s", status, out)
	}
	if got := e.calls(); !slices.Equal(got, []string{"minimax-a", "minimax-b"}) {
		t.Fatalf("upstream calls = %v", got)
	}
}

// A provider whose own machinery failed answers 400 with a generic message
// and `error.type` of "server_error". Nothing in the text says the model is
// down, so the body type is what tells the combo to try the next member.
func TestComboRetriesAProviderServerError(t *testing.T) {
	e := setupCombo(t)
	e.oopsA.Store(true)
	status, out := e.post(t, "claude-combo")
	if status != http.StatusOK || !strings.Contains(out, "message_stop") {
		t.Fatalf("status %d: %s", status, out)
	}
	if got := e.calls(); !slices.Equal(got, []string{"minimax-a", "minimax-b"}) {
		t.Fatalf("upstream calls = %v", got)
	}
}

// The last member has nobody to fail over to, but a stream that ends without
// an answer still must not reach the client as a 200 it cannot parse.
func TestComboLastMemberTruncatedBecomesAnError(t *testing.T) {
	e := setupCombo(t)
	e.truncA.Store(true)
	must(t, e.store.UpdateCombo(t.Context(), store.Combo{ID: e.combo.ID, Name: "stack", Strategy: store.ComboFallback, Enabled: true,
		Members: []store.ComboMember{{ModelID: e.models["minimax-a"], Weight: 1}}}))
	status, out := e.post(t, "claude-combo")
	if status == http.StatusOK {
		t.Fatalf("a truncated stream reached the client as 200: %s", out)
	}
	if !strings.Contains(out, "message_stop") {
		t.Fatalf("status %d, error does not name the cause: %s", status, out)
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
