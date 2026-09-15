package gateway_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"intellyrouter/internal/provider"
	"intellyrouter/internal/sse"
	"intellyrouter/internal/store"
)

const geminiToolStream = `data: {"id":"c1","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_sig","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"a.go\"}"},"extra_content":{"google":{"thought_signature":"sig-live"}}}]}}]}

data: {"id":"c1","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}

data: [DONE]

`

func TestGeminiThoughtSignaturesAreReplayed(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	e := setup(t, provider.OpenAICompatible, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if bytes.Contains(raw, []byte(`"role":"tool"`)) {
			_, _ = io.WriteString(w, "data: {\"id\":\"c2\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		_, _ = io.WriteString(w, geminiToolStream)
	})
	ctx := t.Context()
	providers, err := e.store.ListProviders(ctx)
	must(t, err)
	m, err := e.store.CreateModel(ctx, store.Model{ProviderID: providers[0].ID, ModelID: "gemini-3.8-flash", Enabled: true})
	must(t, err)
	_, err = e.store.CreateRoute(ctx, store.Route{Name: "claude-gemini", Strategy: store.StrategyDirect, Tiers: []store.Tier{{ModelID: m.ID, Label: "gemini"}}})
	must(t, err)
	headers := map[string]string{"X-Intelly-Key": e.key}

	resp, out := send(t, http.MethodPost, e.url+"/v1/messages", `{"model":"claude-gemini","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"read a.go"}]}`, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, out)
	}
	var toolUseID string
	events := sse.NewReader(bytes.NewReader(out))
	for {
		ev, err := events.Next()
		if err != nil {
			break
		}
		var start struct {
			ContentBlock struct {
				Type string `json:"type"`
				ID   string `json:"id"`
			} `json:"content_block"`
		}
		if ev.Name == "content_block_start" && json.Unmarshal(ev.Data, &start) == nil && start.ContentBlock.Type == "tool_use" {
			toolUseID = start.ContentBlock.ID
		}
	}
	if toolUseID == "" {
		t.Fatalf("no tool_use in %s", out)
	}

	// The history mixes a call from another model with Gemini's own call.
	history := fmt.Sprintf(`{"model":"claude-gemini","max_tokens":100,"stream":true,"messages":[
		{"role":"user","content":"read a.go"},
		{"role":"assistant","content":[{"type":"tool_use","id":"toolu_from_claude","name":"read_file","input":{}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_from_claude","content":"x"}]},
		{"role":"assistant","content":[{"type":"tool_use","id":%q,"name":"read_file","input":{"path":"a.go"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":%q,"content":"package a"}]}]}`, toolUseID, toolUseID)
	resp, out = send(t, http.MethodPost, e.url+"/v1/messages", history, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, out)
	}
	mu.Lock()
	replayed := bodies[len(bodies)-1]
	mu.Unlock()
	if !strings.Contains(replayed, `"thought_signature":"sig-live"`) || !strings.Contains(replayed, `"thought_signature":"skip_thought_signature_validator"`) {
		t.Fatalf("replayed request = %s", replayed)
	}
}
