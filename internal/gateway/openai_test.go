package gateway_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"intellyrouter/internal/ledger"
	"intellyrouter/internal/provider"
	"intellyrouter/internal/sse"
)

func TestOpenAICompatibleStreamIsTranslated(t *testing.T) {
	fixture, err := os.ReadFile("../../testdata/openai/stream_text_tools.sse")
	must(t, err)
	var seen struct {
		path, auth, apiKey string
		body               map[string]any
	}
	e := setup(t, provider.OpenAICompatible, func(w http.ResponseWriter, r *http.Request) {
		seen.path, seen.auth, seen.apiKey = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-Api-Key")
		_ = json.NewDecoder(r.Body).Decode(&seen.body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(fixture)
	})

	resp, out := send(t, http.MethodPost, e.url+"/v1/messages?beta=true", streamBody, map[string]string{
		"X-Intelly-Key": e.key,
		"Authorization": "Bearer client-oauth-token",
	})
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("got %d %q: %s", resp.StatusCode, resp.Header.Get("Content-Type"), out)
	}
	if seen.path != "/chat/completions" || seen.auth != "Bearer sk-upstream" || seen.apiKey != "" {
		t.Errorf("upstream request: path=%q auth=%q x-api-key=%q", seen.path, seen.auth, seen.apiKey)
	}
	if seen.body["model"] != "claude-haiku-4-5" || seen.body["stream"] != true {
		t.Errorf("upstream body model=%v stream=%v", seen.body["model"], seen.body["stream"])
	}

	var names []string
	events := sse.NewReader(bytes.NewReader(out))
	for {
		ev, err := events.Next()
		if err == io.EOF {
			break
		}
		must(t, err)
		names = append(names, ev.Name)
	}
	want := "message_start content_block_start content_block_delta content_block_delta content_block_stop " +
		"content_block_start content_block_delta content_block_stop content_block_start content_block_delta content_block_stop " +
		"message_delta message_stop"
	if strings.Join(names, " ") != want {
		t.Fatalf("client events = %v", names)
	}

	req := onlyRequest(t, e.store)
	leg := req.Legs[0]
	if req.Status != ledger.StatusOK || leg.InputTokens != 500 || leg.OutputTokens != 60 || leg.CacheReadTokens != 1000 || leg.StopReason != "tool_use" {
		t.Fatalf("ledger request = %+v", req)
	}
}

func TestOpenAICompatibleNonStream(t *testing.T) {
	msg, err := os.ReadFile("../../testdata/openai/gemini_message.json")
	must(t, err)
	e := setup(t, provider.OpenAICompatible, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(msg)
	})
	body := strings.Replace(streamBody, `"stream":true`, `"stream":false`, 1)
	resp, out := send(t, http.MethodPost, e.url+"/v1/messages", body, map[string]string{"X-Api-Key": e.key})
	var got struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
	}
	if err := json.Unmarshal(out, &got); err != nil || resp.StatusCode != 200 {
		t.Fatalf("got %d %s", resp.StatusCode, out)
	}
	if len(got.Content) != 1 || got.Content[0].Text != "hello there" || got.StopReason != "end_turn" {
		t.Fatalf("message = %s", out)
	}
	leg := onlyRequest(t, e.store).Legs[0]
	if leg.InputTokens != 7 || leg.OutputTokens != 2 {
		t.Fatalf("ledger leg = %+v", leg)
	}
}

func TestOpenAICompatibleErrorIsTranslated(t *testing.T) {
	e := setup(t, provider.OpenAICompatible, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"Rate limit reached","type":"requests"}}`)
	})
	resp, out := send(t, http.MethodPost, e.url+"/v1/messages", streamBody, map[string]string{"X-Api-Key": e.key})
	if resp.StatusCode != 429 || resp.Header.Get("Retry-After") != "3" ||
		string(out) != `{"type":"error","error":{"type":"rate_limit_error","message":"Rate limit reached"}}` {
		t.Fatalf("got %d retry-after=%q %s", resp.StatusCode, resp.Header.Get("Retry-After"), out)
	}
	req := onlyRequest(t, e.store)
	if req.Status != ledger.StatusUpstreamError || req.Error != "rate_limit_error: Rate limit reached" {
		t.Fatalf("ledger request = %+v", req)
	}
}

func TestCountTokensUnavailableForOpenAIProviders(t *testing.T) {
	e := setup(t, provider.OpenAICompatible, func(http.ResponseWriter, *http.Request) {
		t.Error("upstream must not be called")
	})
	resp, _ := send(t, http.MethodPost, e.url+"/v1/messages/count_tokens", `{"model":"intelly-claude-fast","messages":[]}`, map[string]string{"X-Api-Key": e.key})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("count_tokens status = %d, want 404", resp.StatusCode)
	}
}
