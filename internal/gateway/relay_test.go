package gateway_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/MrMirhan/intellyrouter/internal/provider"
)

// An Anthropic-shaped endpoint may answer a non-streamed request in the OpenAI
// Chat Completions shape. The relay must convert it, or the tracker reports an
// empty response and the client receives a body it cannot parse.
func TestNonStreamOpenAIShapedBodyIsConverted(t *testing.T) {
	e := setup(t, provider.Anthropic, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp_1","object":"chat.completion","model":"gpt-6-sol",`+
			`"choices":[{"index":0,"message":{"role":"assistant","content":"PONG"},"finish_reason":"stop"}],`+
			`"usage":{"prompt_tokens":786,"completion_tokens":6}}`)
	})

	resp, out := send(t, http.MethodPost, e.url+"/v1/messages", nonStreamBody, map[string]string{"X-Intelly-Key": e.key})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, out)
	}
	var got struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			Input  int `json:"input_tokens"`
			Output int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("client body is not an Anthropic message: %v: %s", err, out)
	}
	if got.Type != "message" || len(got.Content) != 1 || got.Content[0].Text != "PONG" {
		t.Fatalf("client body = %s", out)
	}
	if got.StopReason != "end_turn" || got.Usage.Input != 786 || got.Usage.Output != 6 {
		t.Fatalf("stop reason or usage lost: %s", out)
	}
	req := onlyRequest(t, e.store)
	if req.Status != "ok" {
		t.Fatalf("ledger status = %q, error %q", req.Status, req.Error)
	}
}

// An Anthropic body without choices must pass through untouched: a conversion
// attempt that failed would otherwise replace a valid answer.
func TestNonStreamAnthropicBodyIsPassedThrough(t *testing.T) {
	const body = `{"type":"message","role":"assistant","model":"claude-haiku-4-5",` +
		`"content":[{"type":"text","text":"PONG"}],"stop_reason":"end_turn",` +
		`"usage":{"input_tokens":11,"output_tokens":2}}`
	e := setup(t, provider.Anthropic, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	})

	resp, out := send(t, http.MethodPost, e.url+"/v1/messages", nonStreamBody, map[string]string{"X-Intelly-Key": e.key})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, out)
	}
	var got struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("client body is not JSON: %v: %s", err, out)
	}
	if got.Model != "claude-haiku-4-5" {
		t.Fatalf("Anthropic body was rewritten: %s", out)
	}
	if req := onlyRequest(t, e.store); req.Status != "ok" {
		t.Fatalf("ledger status = %q, error %q", req.Status, req.Error)
	}
}

const nonStreamBody = `{"model":"intelly-claude-fast","max_tokens":64,"messages":[{"role":"user","content":"say PONG"}]}`
