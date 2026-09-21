package gateway

import (
	"encoding/json"
	"strings"
	"testing"
)

// The real name that a 64-character provider limit rejects.
const longToolName = "mcp__plugin_personal_cloudflare-bindings__complete_authentication"

func TestShortenToolNamesRewritesDefinitionAndHistory(t *testing.T) {
	body := `{"model":"m","tools":[` +
		`{"name":"Bash","input_schema":{}},` +
		`{"name":"` + longToolName + `","input_schema":{}}],` +
		`"messages":[{"role":"assistant","content":[` +
		`{"type":"tool_use","id":"t1","name":"` + longToolName + `","input":{}}]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}]}`

	out, back, err := shortenToolNames([]byte(body), 64)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 {
		t.Fatalf("reverse map = %v, want one entry", back)
	}
	if strings.Contains(string(out), longToolName) {
		t.Fatalf("the long name survived: %s", out)
	}
	if !strings.Contains(string(out), `"name":"Bash"`) {
		t.Fatal("a name within the limit was rewritten")
	}
	var short string
	for to, from := range back {
		if from != longToolName {
			t.Fatalf("reverse map points at %q", from)
		}
		short = to
	}
	if len(short) > 64 {
		t.Fatalf("short name is %d characters", len(short))
	}
	// Both the definition and the recorded call carry the new name, or the
	// provider rejects the history for the same reason.
	if strings.Count(string(out), `"`+short+`"`) != 2 {
		t.Fatalf("expected the short name in the tool and its tool_use: %s", out)
	}
	if !json.Valid(out) {
		t.Fatal("result is not valid JSON")
	}
}

// A request whose names all fit keeps its bytes, so the prompt cache holds.
func TestShortenToolNamesLeavesShortNamesAlone(t *testing.T) {
	body := `{"model":"m","tools":[{"name":"Bash","input_schema":{}}]}`
	out, back, err := shortenToolNames([]byte(body), 64)
	if err != nil || back != nil || string(out) != body {
		t.Fatalf("short names changed: out=%s back=%v err=%v", out, back, err)
	}
}

// Two names that share the first 64 characters must not collide.
func TestShortToolNameKeepsSimilarNamesApart(t *testing.T) {
	a := strings.Repeat("a", 70) + "_one"
	b := strings.Repeat("a", 70) + "_two"
	if shortToolName(a, 64) == shortToolName(b, 64) {
		t.Fatal("two names with the same prefix produced the same short name")
	}
	if got := len(shortToolName(a, 64)); got != 64 {
		t.Fatalf("short name length = %d, want 64", got)
	}
}
