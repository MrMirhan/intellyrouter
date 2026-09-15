package guided

import (
	"strings"
	"testing"
)

func TestTranscriptPrompt(t *testing.T) {
	body := strings.Replace(session, `{"model":"claude-guided",`, `{"model":"claude-guided","system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1; cc_entrypoint=cli;"},{"type":"text","text":"You are Claude Code. Working directory: /repo"}],`, 1)
	tr, err := ReadTranscript([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	prompt := tr.Prompt(0, ReasonUnsure, "", "Check calc.go first.")
	for _, want := range []string{
		"# Executor session setup\n\nYou are Claude Code. Working directory: /repo",
		"## User and tool results\n\nFix the failing tests in calc",
		"## Executor\n\nEditing.",
		`[tool call Edit] {"file_path":"calc.go"}`,
		"# Checkpoint: executor is unsure",
		"Your previous guidance in this turn:\nCheck calc.go first.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt misses %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "secret") || strings.Contains(prompt, "billing-header") {
		t.Fatalf("thinking or the billing header reached the director prompt:\n%s", prompt)
	}
}

func TestTranscriptPromptFrom(t *testing.T) {
	tr, err := ReadTranscript([]byte(`{"system":"Setup text","messages":[{"role":"user","content":"Fix it"},` +
		`{"role":"assistant","content":[{"type":"text","text":"Editing."},{"type":"tool_use","id":"t1","name":"Edit","input":{"file_path":"a.go"}}]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Blocks) != 4 || tr.Setup != "Setup text" {
		t.Fatalf("transcript = %+v", tr)
	}
	want := "# Executor session, continued\n\n## Executor\n\n[tool call Edit] {\"file_path\":\"a.go\"}\n\n## User and tool results\n\n[tool result] ok\n\n" +
		"# Checkpoint: executor is unsure\n\nGive your guidance now.\n"
	if got := tr.Prompt(2, ReasonUnsure, "", ""); got != want {
		t.Fatalf("prompt =\n%s", got)
	}
}

func TestGuidanceText(t *testing.T) {
	if g, approved, err := GuidanceText("  APPROVED. Ship it.\n"); err != nil || !approved || g != "APPROVED. Ship it." {
		t.Fatalf("GuidanceText = %q %v %v", g, approved, err)
	}
	if _, _, err := GuidanceText(" "); err == nil {
		t.Fatal("empty guidance accepted")
	}
}
