package claudecli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	out := `{"type":"result","subtype":"success","is_error":false,"result":"  Return an error.  ",
"usage":{"input_tokens":10,"output_tokens":140,"cache_read_input_tokens":14720,"cache_creation_input_tokens":736,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":736}}}`
	res, err := Parse([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "Return an error." || res.Usage != (Usage{Input: 10, Output: 140, CacheRead: 14720, CacheWrite: 736, CacheWrite1h: 736}) {
		t.Fatalf("result = %+v", res)
	}
	if _, err := Parse([]byte(`{"is_error":true,"subtype":"error_during_execution","result":"login expired"}`)); err == nil || !strings.Contains(err.Error(), "login expired") {
		t.Fatalf("error result: %v", err)
	}
	if _, err := Parse([]byte(`{"is_error":false,"result":"  "}`)); err == nil {
		t.Fatal("empty answer accepted")
	}
}

func TestArgs(t *testing.T) {
	flags := func(o Options) string { return strings.Join(Args(o)[9:], " ") }
	o := Options{Model: "claude-opus-5", AppendSystemPrompt: "x"}
	if got := flags(o); got != "--setting-sources project --strict-mcp-config --no-session-persistence" {
		t.Fatalf("one-shot flags = %q", got)
	}
	o.Effort, o.SessionID = "high", "abc"
	if got := flags(o); got != "--setting-sources project --strict-mcp-config --effort high --session-id abc" {
		t.Fatalf("new session flags = %q", got)
	}
	o.Resume = true
	if got := flags(o); !strings.HasSuffix(got, "--resume abc") {
		t.Fatalf("resume flags = %q", got)
	}
}

func TestRun(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	script := "#!/bin/sh\n" +
		"input=$(cat)\n" +
		"case \"$*\" in *\"--tools  --append-system-prompt\"*) tools=off ;; *) tools=on ;; esac\n" +
		"printf '{\"is_error\":false,\"result\":\"%s bytes, tools %s, base url [%s], quiet %s, model %s, dir %s\"}' " +
		"\"${#input}\" \"$tools\" \"$ANTHROPIC_BASE_URL\" \"$CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC\" \"$3\" \"$(basename \"$PWD\")\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTHROPIC_BASE_URL", "http://127.0.0.1:7117")
	res, err := Run(t.Context(), Options{Binary: bin, Model: "claude-opus-5", AppendSystemPrompt: "Direct the agent.", Input: "hello", Dir: filepath.Join(dir, "director")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "5 bytes, tools off, base url [], quiet 1, model claude-opus-5, dir director" {
		t.Fatalf("result = %q", res.Text)
	}
}

func TestRemoveSession(t *testing.T) {
	config := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", config)
	project := filepath.Join(config, "projects", "-data-claude-director")
	keep := filepath.Join(project, "other.jsonl")
	for _, p := range []string{filepath.Join(project, "s1.jsonl"), filepath.Join(project, "s1", "tool-results", "r.txt"), keep} {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := RemoveSession("s1"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(project)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, []string{"other.jsonl"}) {
		t.Fatalf("left = %v", names)
	}
}
