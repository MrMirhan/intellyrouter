package gateway

import (
	"encoding/json"
	"strings"
	"testing"
)

// brokenToolHistory is what a session looks like after a weak executor emitted
// a corrupted tool name: the broken call, its result, and healthy turns around it.
const brokenToolHistory = `{"model":"m","max_tokens":64,"messages":[` +
	`{"role":"user","content":"hi"},` +
	`{"role":"assistant","content":[` +
	`{"type":"text","text":"working"},` +
	`{"type":"tool_use","id":"t_ok","name":"Read","input":{}},` +
	`{"type":"tool_use","id":"t_bad","name":"mcp__task-runner__list_projects]<]minimax[","input":{}}]},` +
	`{"role":"user","content":[{"type":"tool_result","tool_use_id":"t_ok","content":"file"},` +
	`{"type":"tool_result","tool_use_id":"t_bad","content":"error: no such tool"}]},` +
	`{"role":"assistant","content":[{"type":"text","text":"done"}]}]}`

func contentTypes(t *testing.T, body []byte) []string {
	t.Helper()
	var req struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	var out []string
	for _, m := range req.Messages {
		var blocks []struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(m.Content, &blocks) != nil {
			out = append(out, "string")
			continue
		}
		for _, b := range blocks {
			out = append(out, b.Type)
		}
	}
	return out
}

// The corrupted call and its result go; the healthy tool call and every other
// block stay, so the session continues with one turn of context lost.
func TestDropBrokenToolCallsRemovesTheCallAndItsResult(t *testing.T) {
	out, dropped, err := dropBrokenToolCalls([]byte(brokenToolHistory))
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 2 {
		t.Fatalf("dropped = %d, want the tool_use and its tool_result", dropped)
	}
	if s := string(out); strings.Contains(s, "minimax[") || strings.Contains(s, "t_bad") {
		t.Fatalf("the broken call survived: %s", s)
	}
	if s := string(out); !strings.Contains(s, `"name":"Read"`) || !strings.Contains(s, "t_ok") {
		t.Fatal("the healthy tool call was lost")
	}
	got := contentTypes(t, out)
	want := []string{"string", "text", "tool_use", "tool_result", "text"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("blocks = %v, want %v", got, want)
	}
}

// A clean history keeps its bytes, so the prompt cache holds.
func TestDropBrokenToolCallsLeavesCleanHistoryUntouched(t *testing.T) {
	clean := `{"model":"m","messages":[{"role":"user","content":"hi"},` +
		`{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{}}]}]}`
	out, dropped, err := dropBrokenToolCalls([]byte(clean))
	if err != nil || dropped != 0 || string(out) != clean {
		t.Fatalf("clean history changed: dropped=%d out=%q err=%v", dropped, out, err)
	}
}

// A name outside the API's charset is dropped regardless of where the bad
// character sits, but every valid name survives untouched.
func TestDropBrokenToolCallsOnlyDropsInvalidCharset(t *testing.T) {
	body := `{"messages":[{"role":"assistant","content":[` +
		`{"type":"tool_use","id":"a","name":"mcp__server__tool_2","input":{}},` +
		`{"type":"tool_use","id":"b","name":"Search_files-1","input":{}},` +
		`{"type":"tool_use","id":"c","name":"bad]name[here","input":{}}]}]}`
	out, dropped, err := dropBrokenToolCalls([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 1 {
		t.Fatalf("dropped = %d, want only the one invalid name", dropped)
	}
	if !strings.Contains(string(out), `"id":"a"`) || !strings.Contains(string(out), `"id":"b"`) {
		t.Fatal("a valid call was dropped")
	}
	if strings.Contains(string(out), `"id":"c"`) {
		t.Fatal("the invalid call survived")
	}
}

// A message left with no blocks is itself invalid, so it goes with its blocks.
func TestDropBrokenToolCallsRemovesEmptiedMessages(t *testing.T) {
	body := `{"messages":[{"role":"user","content":"hi"},` +
		`{"role":"assistant","content":[{"type":"tool_use","id":"t","name":"bad]name","input":{}}]}]}`
	out, dropped, err := dropBrokenToolCalls([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 1 {
		t.Fatalf("dropped = %d", dropped)
	}
	got := contentTypes(t, out)
	if len(got) != 1 || got[0] != "string" {
		t.Fatalf("blocks = %v, want only the plain user message", got)
	}
}
