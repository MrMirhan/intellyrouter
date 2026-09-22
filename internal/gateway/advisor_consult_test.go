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
	"strings"
	"sync"
	"testing"

	"github.com/MrMirhan/intellyrouter/internal/gateway"
	"github.com/MrMirhan/intellyrouter/internal/ledger"
	"github.com/MrMirhan/intellyrouter/internal/provider"
	"github.com/MrMirhan/intellyrouter/internal/store"
)

type advisorCall struct {
	model    string
	effort   string
	advisor  bool // the request asks the advisor
	askTool  bool // the executor request offers ask_advisor
	answered bool // the executor request continues after the advisor's answer
	advice   bool // the executor request carries an answer from earlier in the turn
}

func readAdvisorCall(r *http.Request) (advisorCall, []byte) {
	raw, _ := io.ReadAll(r.Body)
	var body struct {
		Model        string                  `json:"model"`
		OutputConfig struct{ Effort string } `json:"output_config"`
	}
	_ = json.Unmarshal(raw, &body)
	return advisorCall{
		model: body.Model, effort: body.OutputConfig.Effort,
		advisor:  bytes.Contains(raw, []byte("You advise a coding agent")),
		askTool:  bytes.Contains(raw, []byte(`"name":"ask_advisor"`)),
		answered: bytes.Contains(raw, []byte("You asked the advisor: Which file has the bug?")),
		advice:   bytes.Contains(raw, []byte("Earlier in this task you asked an advisor model")),
	}, raw
}

func writeAdvisorAnswer(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"type":"message","role":"assistant","content":[{"type":"text","text":"Look at Add in calc.go."}],"stop_reason":"end_turn","usage":{"input_tokens":800,"output_tokens":40}}`)
}

// setupAdvisorRoute builds a direct route on DeepSeek whose advisor is GLM on
// another provider, and returns the gateway URL and key.
func setupAdvisorRoute(t *testing.T, upstream http.HandlerFunc, settings func(advisorID int64) string) (string, string, *store.Store) {
	t.Helper()
	up := httptest.NewServer(upstream)
	t.Cleanup(up.Close)
	st, err := store.Open(filepath.Join(t.TempDir(), "advisor.db"), bytes.Repeat([]byte{9}, 32))
	must(t, err)
	t.Cleanup(func() { st.Close() })
	ctx := t.Context()
	deepseek, err := st.CreateProvider(ctx, store.Provider{Type: string(provider.AnthropicCompatible), Name: "deepseek", BaseURL: up.URL, APIKey: "sk-deepseek", Enabled: true})
	must(t, err)
	zai, err := st.CreateProvider(ctx, store.Provider{Type: string(provider.AnthropicCompatible), Name: "z.ai", BaseURL: up.URL, APIKey: "sk-zai", Enabled: true})
	must(t, err)
	flash, err := st.CreateModel(ctx, store.Model{ProviderID: deepseek.ID, ModelID: "deepseek-v4.1-flash", PriceIn: 0.1, PriceOut: 0.4, Enabled: true})
	must(t, err)
	glm, err := st.CreateModel(ctx, store.Model{ProviderID: zai.ID, ModelID: "glm-5.3", PriceIn: 0.6, PriceOut: 2.2, Enabled: true})
	must(t, err)
	_, err = st.CreateRoute(ctx, store.Route{
		Name: "claude-direct", Strategy: store.StrategyDirect, Tiers: []store.Tier{{ModelID: flash.ID, Label: "flash"}},
		Settings: settings(glm.ID),
	})
	must(t, err)
	key, _, err := st.CreateGatewayKey(ctx, "test")
	must(t, err)
	log := slog.New(slog.DiscardHandler)
	mux := http.NewServeMux()
	gateway.New(st, ledger.NewRecorder(st, log), up.Client(), log).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL, key, st
}

func postDirect(t *testing.T, url, key, messages string) string {
	t.Helper()
	body := `{"model":"claude-direct","max_tokens":64,"stream":true,"tools":[{"name":"Bash","input_schema":{"type":"object"}},` +
		`{"type":"advisor_20260301","name":"advisor","model":"claude-opus-5"}],"messages":[` + messages + `]}`
	resp, out := send(t, http.MethodPost, url+"/v1/messages", body, map[string]string{"X-Intelly-Key": key, "X-Claude-Code-Session-Id": "s1"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, out)
	}
	return string(out)
}

func advisorFixtures(t *testing.T) (ask, after []byte) {
	t.Helper()
	ask, err := os.ReadFile("../../testdata/anthropic/stream_ask_director.sse")
	must(t, err)
	after, err = os.ReadFile("../../testdata/anthropic/stream_after_answer.sse")
	must(t, err)
	return bytes.ReplaceAll(ask, []byte("ask_director"), []byte("ask_advisor")), after
}

// A direct route on DeepSeek asks an advisor on another provider through the
// gateway's own ask_advisor tool, and keeps the answer for the rest of the turn.
func TestDirectRouteAdvisorOnAnotherProvider(t *testing.T) {
	ask, after := advisorFixtures(t)
	var mu sync.Mutex
	var calls []advisorCall
	url, key, st := setupAdvisorRoute(t, func(w http.ResponseWriter, r *http.Request) {
		c, raw := readAdvisorCall(r)
		mu.Lock()
		calls = append(calls, c)
		mu.Unlock()
		switch {
		case c.advisor:
			writeAdvisorAnswer(w)
		case bytes.Contains(raw, []byte("advisor_20260301")):
			w.WriteHeader(http.StatusBadRequest)
		case c.answered || c.advice:
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write(after)
		default:
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write(ask)
		}
	}, func(id int64) string { return fmt.Sprintf(`{"advisor":{"model_id":%d,"effort":"high"}}`, id) })

	const prompt = `{"role":"user","content":"fix calc"}`
	out := postDirect(t, url, key, prompt)
	postDirect(t, url, key, prompt+`,{"role":"assistant","content":[{"type":"tool_use","id":"b1","name":"Bash","input":{"command":"go test ./..."}}]},`+
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"b1","content":"FAIL calc"}]}`)

	mu.Lock()
	got := append([]advisorCall(nil), calls...)
	mu.Unlock()
	want := []advisorCall{
		{model: "deepseek-v4.1-flash", askTool: true},
		{model: "glm-5.3", effort: "high", advisor: true},
		{model: "deepseek-v4.1-flash", askTool: true, answered: true},
		{model: "deepseek-v4.1-flash", askTool: true, advice: true},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("upstream calls:\n got %+v\nwant %+v", got, want)
	}
	switch {
	case strings.Contains(out, "ask_advisor"):
		t.Fatalf("the hidden question reached Claude Code:\n%s", out)
	case strings.Count(out, "event: message_start") != 1 || !strings.Contains(out, "calc.go has the bug."):
		t.Fatalf("client stream is not one continued message:\n%s", out)
	}
	legs := requestLegs(t, st)
	first := legs[0]
	if len(first) != 3 || first[0].Role != ledger.RoleDirect || first[1].Role != ledger.RoleAdvisor || first[1].Model != "glm-5.3" ||
		!strings.Contains(first[1].Note, "question: Which file has the bug?") || !strings.Contains(first[2].Note, "continued after the advisor's answer") {
		t.Fatalf("legs = %+v", first)
	}
	if !strings.Contains(legs[1][0].Note, "advisor answer from earlier in the turn") {
		t.Fatalf("second request legs = %+v", legs[1])
	}
}

// An executor that keeps asking after the advisor's limit loses the tool and
// finishes the response.
func TestAdvisorLimitEndsTheQuestions(t *testing.T) {
	ask, after := advisorFixtures(t)
	var mu sync.Mutex
	var calls []advisorCall
	url, key, _ := setupAdvisorRoute(t, func(w http.ResponseWriter, r *http.Request) {
		c, _ := readAdvisorCall(r)
		c.answered, c.advice = false, false
		mu.Lock()
		calls = append(calls, c)
		mu.Unlock()
		if c.advisor {
			writeAdvisorAnswer(w)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if c.askTool {
			_, _ = w.Write(ask)
			return
		}
		_, _ = w.Write(after)
	}, func(id int64) string { return fmt.Sprintf(`{"advisor":{"model_id":%d,"max_calls_per_turn":1}}`, id) })

	out := postDirect(t, url, key, `{"role":"user","content":"fix calc"}`)
	mu.Lock()
	got := append([]advisorCall(nil), calls...)
	mu.Unlock()
	want := []advisorCall{
		{model: "deepseek-v4.1-flash", askTool: true},
		{model: "glm-5.3", advisor: true},
		{model: "deepseek-v4.1-flash", askTool: true},
		{model: "deepseek-v4.1-flash"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("upstream calls:\n got %+v\nwant %+v", got, want)
	}
	if strings.Count(out, "event: message_start") != 1 || !strings.Contains(out, "calc.go has the bug.") {
		t.Fatalf("client stream:\n%s", out)
	}
}

// On a guided route with an advisor, the executor asks the advisor instead of
// the director, and the director keeps the checkpoints.
func TestGuidedExecutorAsksAdvisor(t *testing.T) {
	e, calls := setupGuided(t, provider.Anthropic)
	ctx := t.Context()
	providers, err := e.store.ListProviders(ctx)
	must(t, err)
	var deepseek int64
	for _, p := range providers {
		if p.Name == "deepseek" {
			deepseek = p.ID
		}
	}
	glm, err := e.store.CreateModel(ctx, store.Model{ProviderID: deepseek, ModelID: "glm-5.3", PriceIn: 0.6, PriceOut: 2.2, Enabled: true})
	must(t, err)
	rt, err := e.store.RouteByName(ctx, "claude-guided")
	must(t, err)
	rt.Settings = strings.Replace(rt.Settings, `"checkpoints"`, fmt.Sprintf(`"advisor":{"model_id":%d},"checkpoints"`, glm.ID), 1)
	must(t, e.store.UpdateRoute(ctx, rt))

	out := string(sendGuided(t, e, `{"role":"user","content":"ASK_TEST fix calc"}`))
	got := calls()
	if len(got) != 4 || !got[0].director || got[1].model != "deepseek-v4-flash" || !got[1].consult ||
		!got[2].advisor || got[2].model != "glm-5.3" || !got[3].answered {
		t.Fatalf("upstream calls = %+v", got)
	}
	if strings.Contains(out, "ask_advisor") || !strings.Contains(out, "calc.go has the bug.") {
		t.Fatalf("client stream:\n%s", out)
	}
	legs := requestLegs(t, e.store)
	if len(legs[0]) != 4 || legs[0][2].Role != ledger.RoleAdvisor || legs[0][2].Model != "glm-5.3" ||
		!strings.Contains(legs[0][3].Note, "continued after the advisor's answer") {
		t.Fatalf("legs = %+v", legs[0])
	}
}
