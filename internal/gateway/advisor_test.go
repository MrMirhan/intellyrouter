package gateway_test

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"intellyrouter/internal/ledger"
	"intellyrouter/internal/provider"
)

const advisorStream = `event: message_start
data: {"type":"message_start","message":{"id":"msg_a","type":"message","role":"assistant","model":"claude-sonnet-5","content":[],"usage":{"input_tokens":10,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"yes"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5,"iterations":[{"type":"message","input_tokens":10,"output_tokens":3},{"type":"advisor_message","model":"claude-fable-5-1","input_tokens":1000,"output_tokens":100,"cache_read_input_tokens":0,"cache_creation_input_tokens":0},{"type":"message","input_tokens":2,"output_tokens":2}]}}

event: message_stop
data: {"type":"message_stop"}

`

func advisorBody(model string) string {
	return `{"model":"` + model + `","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"Is 91 prime?"}],` +
		`"tools":[{"name":"Read","input_schema":{"type":"object"}},{"type":"advisor_20260301","name":"advisor","model":"claude-fable-5-1"}]}`
}

func TestAdvisorToolReachesOnlyAnthropic(t *testing.T) {
	for _, c := range []struct {
		typ         provider.Type
		wantAdvisor bool
	}{
		{provider.Anthropic, true},
		{provider.AnthropicCompatible, false},
	} {
		t.Run(string(c.typ), func(t *testing.T) {
			var mu sync.Mutex
			var upstream string
			e := setup(t, c.typ, func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				mu.Lock()
				upstream = string(raw)
				mu.Unlock()
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, advisorStream)
			})
			resp, out := send(t, http.MethodPost, e.url+"/v1/messages", advisorBody(route), map[string]string{"X-Intelly-Key": e.key})
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d: %s", resp.StatusCode, out)
			}
			mu.Lock()
			defer mu.Unlock()
			if strings.Contains(upstream, "advisor_20260301") != c.wantAdvisor || !strings.Contains(upstream, `"name":"Read"`) {
				t.Fatalf("upstream body = %s", upstream)
			}
		})
	}
}

func TestAdvisorUsageBecomesALeg(t *testing.T) {
	e := setup(t, provider.Anthropic, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, advisorStream)
	})
	resp, out := send(t, http.MethodPost, e.url+"/v1/messages", advisorBody(route), map[string]string{"X-Intelly-Key": e.key})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, out)
	}
	req := onlyRequest(t, e.store)
	if len(req.Legs) != 2 {
		t.Fatalf("legs = %+v", req.Legs)
	}
	adv := req.Legs[1]
	// Fable 5.1 costs $10 input and $50 output per million tokens.
	if adv.Role != ledger.RoleAdvisor || adv.Model != "claude-fable-5-1" || adv.InputTokens != 1000 || adv.OutputTokens != 100 ||
		adv.CostUSD < 0.0149 || adv.CostUSD > 0.0151 || req.CostUSD <= adv.CostUSD {
		t.Fatalf("advisor leg = %+v, request cost %v", adv, req.CostUSD)
	}
}
