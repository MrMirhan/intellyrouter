package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MrMirhan/intellyrouter/internal/provider"
)

// withAdvisorHistory is a turn where an Anthropic tier used the advisor, so the
// transcript carries the blocks that produced.
const withAdvisorHistory = `{"model":"m","max_tokens":64,` +
	`"tools":[{"type":"advisor_20260101","name":"advisor","model":"claude-opus-5"},{"name":"Read","input_schema":{}}],` +
	`"messages":[` +
	`{"role":"user","content":"hi"},` +
	`{"role":"assistant","content":[{"type":"text","text":"asking"},{"type":"advisor_tool_use","id":"a1","input":{}}]},` +
	`{"role":"user","content":[{"type":"advisor_tool_result","tool_use_id":"a1","content":"answer"}]},` +
	`{"role":"assistant","content":[{"type":"text","text":"done"}]}]}`

func blockTypes(t *testing.T, body []byte) []string {
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

// A provider that never knew the advisor tool also never knew its blocks.
func TestDropAdvisorToolsClearsTheBlocksItLeftBehind(t *testing.T) {
	tgt := target{config: provider.Config{Type: provider.AnthropicCompatible}}
	out, changed, err := dropAdvisorTools(tgt, []byte(withAdvisorHistory))
	if err != nil || !changed {
		t.Fatalf("dropAdvisorTools = %v, %v", changed, err)
	}
	if s := string(out); strings.Contains(s, "advisor_") {
		t.Fatalf("an advisor block or tool survived: %s", s)
	}
	got := blockTypes(t, out)
	// The user message held only the advisor result, so it goes with it.
	want := []string{"string", "text", "text"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("blocks = %v, want %v", got, want)
	}
}

// Anthropic runs the tool itself, so its requests keep both.
func TestAnthropicKeepsAdvisorBlocks(t *testing.T) {
	for _, kind := range []provider.Type{provider.Anthropic, provider.AnthropicSubscription} {
		out, changed, err := dropAdvisorTools(target{config: provider.Config{Type: kind}}, []byte(withAdvisorHistory))
		if err != nil || changed {
			t.Fatalf("%s: changed = %v, %v", kind, changed, err)
		}
		if !strings.Contains(string(out), "advisor_tool_result") {
			t.Fatalf("%s: lost the advisor history", kind)
		}
	}
}

// The real 400 from MiniMax-M3, which the gateway did not recognise before.
func TestAdaptationForUnsupportedContentType(t *testing.T) {
	cases := map[string]string{
		"invalid params, messages.410.content.1: unsupported content type 'advisor_tool_result' (2013)": "block:advisor_tool_result",
		"messages.3.content.0: unknown content type \"thinking\"":                                       "block:thinking",
		"messages.1.content.2: Unsupported content block type: redacted_thinking":                       "block:redacted_thinking",
	}
	for message, want := range cases {
		got, ok := adaptationFor(message)
		if !ok || got != want {
			t.Fatalf("adaptationFor(%q) = %q, %v; want %q", message, got, ok, want)
		}
	}
}

// Dropping text or images would lose the question itself, so those are not
// treated as a removable feature.
func TestAdaptationForKeepsEssentialBlocks(t *testing.T) {
	for _, kind := range []string{"text", "image", "tool_use", "tool_result", "document"} {
		if a, ok := adaptationFor("messages.0.content.0: unsupported content type '" + kind + "'"); ok {
			t.Fatalf("%s would be dropped as %q", kind, a)
		}
	}
}

// Learning the rejection removes the blocks, and the request keeps its shape.
func TestCompatLearnsAnUnsupportedBlock(t *testing.T) {
	c := newCompat()
	errBody := []byte(`{"type":"error","error":{"type":"invalid_request_error","message":"invalid params, messages.410.content.1: unsupported content type 'advisor_tool_result' (2013)"}}`)
	out, a, _, ok := c.learn(7, errBody, []byte(withAdvisorHistory))
	if !ok || a != "block:advisor_tool_result" {
		t.Fatalf("learn = %q, %v", a, ok)
	}
	if strings.Contains(string(out), "advisor_tool_result") {
		t.Fatal("the rejected block survived")
	}
	// The advisor_tool_use block is a different type and stays until its own
	// rejection, so the body still parses and keeps the rest of the turn.
	if got := blockTypes(t, out); len(got) == 0 {
		t.Fatal("no messages left")
	}
	// A second request to the same model applies what was learned.
	again, applied, _ := c.apply(7, []byte(withAdvisorHistory))
	if len(applied) != 1 || strings.Contains(string(again), "advisor_tool_result") {
		t.Fatalf("apply = %v", applied)
	}
}

// A body whose messages are plain strings has no blocks to remove.
func TestRemoveContentBlocksLeavesStringContent(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	out, changed, err := removeContentBlocks(body, func(string) bool { return true })
	if err != nil || changed || string(out) != string(body) {
		t.Fatalf("removeContentBlocks = %q, %v, %v", out, changed, err)
	}
}
