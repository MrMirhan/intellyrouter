package translate

import (
	"encoding/json"
	"reflect"
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

func TestRequestConvertsClaudeCodeConversation(t *testing.T) {
	in := `{
	  "model": "intelly-claude-auto", "max_tokens": 32000, "stream": true, "temperature": 1,
	  "system": [{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1"},{"type":"text","text":"You are Claude Code.","cache_control":{"type":"ephemeral"}}],
	  "messages": [
	    {"role":"user","content":[{"type":"text","text":"<system-reminder>ctx</system-reminder>"},{"type":"text","text":"Fix the bug","cache_control":{"type":"ephemeral"}}]},
	    {"role":"assistant","content":[{"type":"thinking","thinking":"hmm","signature":"sig"},{"type":"text","text":"Reading."},{"type":"tool_use","id":"toolu_1","name":"Read","input":{"file_path":"/a.go"}},{"type":"tool_use","id":"toolu_2","name":"Grep","input":{"pattern":"x"}}]},
	    {"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"package a"},{"type":"tool_result","tool_use_id":"toolu_2","is_error":true,"content":[{"type":"text","text":"no matches"}]},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"iVBOR"}},{"type":"text","text":"see screenshot"}]},
	    {"role":"assistant","content":"Done."}
	  ],
	  "tools": [
	    {"name":"Read","description":"Read a file","input_schema":{"type":"object","properties":{"file_path":{"type":"string"}}}},
	    {"type":"web_search_20250305","name":"web_search","max_uses":5}
	  ],
	  "tool_choice": {"type":"any","disable_parallel_tool_use":true},
	  "stop_sequences": ["END"],
	  "thinking": {"type":"adaptive"},
	  "context_management": {"edits":[]}
	}`
	got, err := Request([]byte(in), Options{Model: "deepseek-chat", MaxTokensField: "max_tokens"})
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, got, `{
	  "model": "deepseek-chat", "max_tokens": 32000, "stream": true, "stream_options": {"include_usage": true},
	  "temperature": 1, "stop": ["END"],
	  "messages": [
	    {"role":"system","content":"x-anthropic-billing-header: cc_version=2.1\n\nYou are Claude Code."},
	    {"role":"user","content":"<system-reminder>ctx</system-reminder>\n\nFix the bug"},
	    {"role":"assistant","content":"Reading.","tool_calls":[
	      {"id":"toolu_1","type":"function","function":{"name":"Read","arguments":"{\"file_path\":\"/a.go\"}"}},
	      {"id":"toolu_2","type":"function","function":{"name":"Grep","arguments":"{\"pattern\":\"x\"}"}}]},
	    {"role":"tool","tool_call_id":"toolu_1","content":"package a"},
	    {"role":"tool","tool_call_id":"toolu_2","content":"Error: no matches"},
	    {"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,iVBOR"}},{"type":"text","text":"see screenshot"}]},
	    {"role":"assistant","content":"Done."}
	  ],
	  "tools": [{"type":"function","function":{"name":"Read","description":"Read a file","parameters":{"type":"object","properties":{"file_path":{"type":"string"}}}}}],
	  "tool_choice": "required",
	  "parallel_tool_calls": false
	}`)
}

func TestRequestUsesMaxCompletionTokensForOpenAI(t *testing.T) {
	got, err := Request([]byte(`{"model":"m","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`),
		Options{Model: "gpt-5.5", MaxTokensField: "max_completion_tokens"})
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, got, `{"model":"gpt-5.5","max_completion_tokens":100,"messages":[{"role":"user","content":"hi"}]}`)
}

func TestRequestToolOnlyAssistantHasNullContent(t *testing.T) {
	in := `{"model":"m","messages":[
	  {"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{}}]},
	  {"role":"user","content":[{"type":"tool_result","tool_use_id":"t1"}]}
	]}`
	got, err := Request([]byte(in), Options{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, got, `{"model":"m","messages":[
	  {"role":"assistant","content":null,"tool_calls":[{"id":"t1","type":"function","function":{"name":"Bash","arguments":"{}"}}]},
	  {"role":"tool","tool_call_id":"t1","content":""}
	]}`)
}
