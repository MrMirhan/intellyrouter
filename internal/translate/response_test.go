package translate

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestResponseFromGeminiMessage(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/openai/gemini_message.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Response(raw, "gemini-2.5-flash")
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, got, `{"id":"msg_HCWpavSjNMK7vdIPmsSF6Ac","type":"message","role":"assistant","model":"gemini-2.5-flash",
	  "content":[{"type":"text","text":"hello there"}],"stop_reason":"end_turn","stop_sequence":null,
	  "usage":{"input_tokens":7,"output_tokens":2,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}`)
}

func TestResponseToolCallsAndCachedUsage(t *testing.T) {
	raw := `{"id":"x.y","choices":[{"finish_reason":"stop","message":{"content":null,"tool_calls":[
	  {"id":"call:1","type":"function","function":{"name":"Bash","arguments":"{\"command\":\"ls\"}"}},
	  {"id":"","type":"function","function":{"name":"Bad","arguments":"not json"}}]}}],
	  "usage":{"prompt_tokens":100,"completion_tokens":5,"prompt_cache_hit_tokens":80}}`
	out, err := Response([]byte(raw), "deepseek-chat")
	if err != nil {
		t.Fatal(err)
	}
	var msg struct {
		ID      string `json:"id"`
		Content []struct {
			Type  string          `json:"type"`
			ID    string          `json:"id"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		StopReason string         `json:"stop_reason"`
		Usage      map[string]int `json:"usage"`
	}
	if err := json.Unmarshal(out, &msg); err != nil {
		t.Fatal(err)
	}
	switch {
	case msg.ID != "msg_x_y":
		t.Errorf("id = %q", msg.ID)
	case len(msg.Content) != 2:
		t.Fatalf("content = %s", out)
	case msg.Content[0].ID != "call_1" || string(msg.Content[0].Input) != `{"command":"ls"}`:
		t.Errorf("first tool_use = %+v", msg.Content[0])
	case !strings.HasPrefix(msg.Content[1].ID, "toolu_") || string(msg.Content[1].Input) != `{}`:
		t.Errorf("malformed tool_use = %+v", msg.Content[1])
	case msg.StopReason != "tool_use":
		t.Errorf("stop_reason = %q", msg.StopReason)
	case msg.Usage["input_tokens"] != 20 || msg.Usage["cache_read_input_tokens"] != 80 || msg.Usage["output_tokens"] != 5:
		t.Errorf("usage = %v", msg.Usage)
	}
}

func TestErrorTranslation(t *testing.T) {
	gemini, err := os.ReadFile("../../testdata/openai/gemini_error.json")
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, Error(404, gemini), `{"type":"error","error":{"type":"not_found_error","message":"models/no-such-model-xyz is not found for API version v1main, or is not supported for generateContent. Call ModelService.ListModels to see the list of available models and their supported methods."}}`)
	jsonEqual(t, Error(429, []byte(`{"error":{"message":"Rate limit reached","type":"requests"}}`)),
		`{"type":"error","error":{"type":"rate_limit_error","message":"Rate limit reached"}}`)
	jsonEqual(t, Error(502, []byte(`<html>bad gateway</html>`)),
		`{"type":"error","error":{"type":"api_error","message":"<html>bad gateway</html>"}}`)
}
