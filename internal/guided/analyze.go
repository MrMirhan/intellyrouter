package guided

import (
	"encoding/json"
	"regexp"
	"strings"

	"intellyrouter/internal/escalate"
)

// Turn adds the signals of the current turn's tool loop to escalate.Turn.
type Turn struct {
	escalate.Turn
	// Steps counts executor (assistant) messages after the prompt.
	Steps             int
	FailedResults     int
	LastResultsFailed bool
	LastResultsPassed bool
	EditedFiles       bool
	// Unsure is set when the newest assistant text says it is stuck or unsure.
	Unsure bool
	// HasTools is false for Claude Code side queries such as title generation.
	HasTools bool
}

type rawMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
	ToolUseID string          `json:"tool_use_id"`
}

var editTools = map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true}

var (
	failurePattern = regexp.MustCompile(`(?im)(exit code [1-9]\d*|^\s*--- FAIL|^FAIL\b|Traceback \(most recent call last\)|^panic:|AssertionError|^FAILED\b|npm ERR!|error\[E\d+\])`)
	passPattern    = regexp.MustCompile(`(?im)(^ok\s+\S+|^PASS$|\b\d+ passed\b|^OK$|^OK \(|all tests pass)`)
	unsurePattern  = regexp.MustCompile(`(?i)(I'?m not sure|I am not sure|not certain (?:why|how|what)|I'?m stuck|I am stuck|I can(?:'|no)t (?:figure out|find|determine|tell)|unable to (?:fix|find|determine|resolve|figure out))`)
)

// Analyze reads the messages of an Anthropic Messages request body.
func Analyze(body []byte) (Turn, error) {
	base, err := escalate.Analyze(body)
	if err != nil {
		return Turn{}, err
	}
	var req struct {
		Tools    []json.RawMessage `json:"tools"`
		Messages []rawMessage      `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return Turn{}, err
	}
	t := Turn{Turn: base, HasTools: len(req.Tools) > 0}
	after := req.Messages[base.PromptIndex+1:]
	lastAssistant := -1
	for i, m := range after {
		blocks := parseBlocks(m.Content)
		switch m.Role {
		case "assistant":
			t.Steps++
			lastAssistant = i
			for _, b := range blocks {
				if b.Type == "tool_use" && editTools[b.Name] {
					t.EditedFiles = true
				}
			}
		case "user":
			for _, b := range blocks {
				if b.Type == "tool_result" && resultFailed(b) {
					t.FailedResults++
				}
			}
		}
	}
	if n := len(after); n > 0 && after[n-1].Role == "user" {
		passed := false
		for _, b := range parseBlocks(after[n-1].Content) {
			if b.Type != "tool_result" {
				continue
			}
			if resultFailed(b) {
				t.LastResultsFailed = true
			} else if passPattern.MatchString(ResultText(b.Content)) {
				passed = true
			}
		}
		t.LastResultsPassed = passed && !t.LastResultsFailed
	}
	if lastAssistant >= 0 {
		var text []string
		for _, b := range parseBlocks(after[lastAssistant].Content) {
			if b.Type == "text" {
				text = append(text, b.Text)
			}
		}
		t.Unsure = unsurePattern.MatchString(strings.Join(text, "\n"))
	}
	return t, nil
}

func resultFailed(b block) bool {
	return b.IsError || failurePattern.MatchString(ResultText(b.Content))
}

func parseBlocks(raw json.RawMessage) []block {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []block{{Type: "text", Text: s}}
	}
	var blocks []block
	_ = json.Unmarshal(raw, &blocks)
	return blocks
}

// ResultText returns the text of a tool_result content, string or blocks.
func ResultText(raw json.RawMessage) string {
	var parts []string
	for _, b := range parseBlocks(raw) {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}
