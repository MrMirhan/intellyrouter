package translate

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func jsonEqual(t *testing.T, got []byte, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, got)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("expected value is not JSON: %v", err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("JSON mismatch\n got: %s\nwant: %s", got, want)
	}
}

// Codex gibi OpenAI backend'leri 100+ tool ile boş cevap veriyor.
// Dedupe + cap uygulamalıyız; tool adları korunmalı.
func TestRequestDeduplicatesTools(t *testing.T) {
	tools := make([]map[string]any, 200)
	for i := range tools {
		tools[i] = map[string]any{
			"name":         "Bash",
			"description":  "dup",
			"input_schema": map[string]any{"type": "object"},
		}
	}
	raw := map[string]any{"model": "m", "max_tokens": 16, "messages": []any{}, "tools": tools}
	b, _ := json.Marshal(raw)
	out, err := Request(b, Options{Model: "m", MaxTokensField: "max_tokens"})
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Tools []struct {
			Type     string `json:"type"`
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Tools) != 1 || parsed.Tools[0].Function.Name != "Bash" {
		t.Fatalf("expected 1 deduplicated Bash tool, got %d", len(parsed.Tools))
	}
}

func TestRequestCapsAtLimit(t *testing.T) {
	tools := make([]map[string]any, 100)
	for i := range tools {
		tools[i] = map[string]any{
			"name":        "tool_" + strings.Repeat("x", i%5) + "_" + strings.Repeat("y", i/5),
			"description": "d",
		}
	}
	raw := map[string]any{"model": "m", "max_tokens": 16, "messages": []any{}, "tools": tools}
	b, _ := json.Marshal(raw)
	out, err := Request(b, Options{Model: "m", MaxTokensField: "max_tokens"})
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Tools []any `json:"tools"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatal(err)
	}
	if got := len(parsed.Tools); got > openAIToolCap {
		t.Fatalf("tool count %d exceeds cap %d", got, openAIToolCap)
	}
}
