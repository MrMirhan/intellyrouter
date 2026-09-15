package translate

import (
	"strings"
	"testing"
)

func TestThoughtSignaturesRoundTrip(t *testing.T) {
	var starts []string
	s := NewStream("gemini-3.8-flash", func(name string, data []byte) error {
		if name == "content_block_start" {
			starts = append(starts, string(data))
		}
		return nil
	})
	for _, c := range []string{
		`{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"Read","arguments":"{}"},"extra_content":{"google":{"thought_signature":"sig-1"}}}]}}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
	} {
		if err := s.Chunk([]byte(c)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Finish(); err != nil {
		t.Fatal(err)
	}
	id := toolID("call_1")
	if got := s.ThoughtSignatures(); len(got) != 1 || got[id] != "sig-1" || len(starts) != 1 || !strings.Contains(starts[0], `"id":"`+id+`"`) {
		t.Fatalf("signatures = %v, starts = %v", got, starts)
	}

	resp := `{"id":"r1","choices":[{"message":{"tool_calls":[{"id":"call_2","type":"function","function":{"name":"Read","arguments":"{}"},"extra_content":{"google":{"thought_signature":"sig-2"}}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`
	out, err := Response([]byte(resp), "gemini-3.8-flash")
	if err != nil {
		t.Fatal(err)
	}
	if sigs := ThoughtSignatures([]byte(resp)); sigs[toolID("call_2")] != "sig-2" || !strings.Contains(string(out), `"id":"`+toolID("call_2")+`"`) {
		t.Fatalf("response signatures = %v for %s", sigs, out)
	}

	body := `{"model":"x","max_tokens":10,"messages":[{"role":"user","content":"hi"},` +
		`{"role":"assistant","content":[{"type":"tool_use","id":"` + id + `","name":"Read","input":{}},{"type":"tool_use","id":"toolu_other","name":"Read","input":{}}]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"` + id + `","content":"a"},{"type":"tool_result","tool_use_id":"toolu_other","content":"b"}]}]}`
	recorded := map[string]string{id: "sig-1"}
	req, err := Request([]byte(body), Options{Model: "gemini-3.8-flash", MaxTokensField: "max_tokens", ThoughtSignature: func(toolUseID string) string { return recorded[toolUseID] }})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(req), `"extra_content":{"google":{"thought_signature":"sig-1"}}`) ||
		!strings.Contains(string(req), `"thought_signature":"`+SkipThoughtSignature+`"`) {
		t.Fatalf("request = %s", req)
	}
	plain, err := Request([]byte(body), Options{Model: "gpt-5", MaxTokensField: "max_tokens"})
	if err != nil || strings.Contains(string(plain), "extra_content") {
		t.Fatalf("non-Gemini request = %s, %v", plain, err)
	}
}
