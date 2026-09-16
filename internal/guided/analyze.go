package guided

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/MrMirhan/intellyrouter/internal/escalate"
)

// Turn adds the signals of the current turn's tool loop to escalate.Turn.
type Turn struct {
	escalate.Turn
	// Steps counts executor (assistant) messages after the prompt.
	Steps             int
	FailedResults     int
	LastResultsFailed bool
	// LastResultsPassed is set when the newest results include a passed test,
	// lint or type check and no failure.
	LastResultsPassed bool
	EditedFiles       bool
	// Unsure is set when the newest assistant text says it is stuck or unsure.
	Unsure bool
	// RepeatCount is the highest number of identical tool calls with the same result.
	RepeatCount  int
	RepeatDetail string
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
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
	ToolUseID string          `json:"tool_use_id"`
}

type toolInput struct {
	Command  string `json:"command"`
	FilePath string `json:"file_path"`
	Pattern  string `json:"pattern"`
}

type callKey struct {
	name, input string
	result      [sha256.Size]byte
}

var editTools = map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true}

var (
	failurePattern = regexp.MustCompile(`(?im)(exit code [1-9]\d*|^\s*--- FAIL|^FAIL\b|Traceback \(most recent call last\)|^panic:|AssertionError|^FAILED\b|npm ERR!|error\[E\d+\])`)
	passPattern    = regexp.MustCompile(`(?im)(^ok\s+\S+|^PASS$|\b\d+ (?:tests? )?passed\b|^OK$|^OK \(|all tests pass|\ball checks passed\b|\btests? completed\b|✓|✔)`)
	unsurePattern  = regexp.MustCompile(`(?i)(I'?m not sure|I am not sure|not certain (?:why|how|what)|I'?m stuck|I am stuck|I can(?:'|no)t (?:figure out|find|determine|tell)|unable to (?:fix|find|determine|resolve|figure out)|I'?m unsure|I am unsure|not sure (?:whether|which|how|why|what)|I don'?t know (?:why|how|which|what)|going in circles|still (?:fails|failing|broken))`)
	// The command must start a shell command, so "grep pytest" or "cat test.go" do not count.
	verifyCommand = regexp.MustCompile(`(?im)(?:^|[;&|(])\s*` +
		`(?:(?:\w+=\S*|sudo|env|time|nice|npx|bunx|timeout\s+\S+|(?:uv|poetry|pipenv|pdm|hatch)\s+run|(?:pnpm|yarn|bundle)\s+exec)\s+)*` +
		`(?:(?:\$\([^)]*\)|[^\s;&|()])*/)?` +
		`(?:go\s+(?:test|vet)|golangci-lint|(?:python[\d.]*\s+-m\s+)?pytest|tox|nox|(?:npm|pnpm|yarn|bun)\s+(?:run\s+)?(?:test|lint|check|typecheck)|vitest|jest|` +
		`cargo\s+(?:test|check|clippy)|make\s+(?:test|check|lint|verify)|mvnw?\s+(?:test|verify)|gradlew?\s+(?:test|check)|dotnet\s+test|phpunit|rspec|mix\s+test|tsc|eslint|ruff|mypy|` +
		`(?:\.{1,2}/|[\w.-]+/)[\w./-]*\s+(?:test|check|lint|verify))` +
		`(?:[^\w.]|$)`)
	// Durations differ between runs that otherwise give the same result.
	durationPattern = regexp.MustCompile(`\b\d+(?:\.\d+)?\s?(?:ns|µs|ms|s)\b`)
)

// HasImage reports whether any message of an Anthropic Messages request
// contains an image or document content block. Tool result text that
// mentions the literal word "image" does not count.
func HasImage(body []byte) bool {
	var req struct {
		Messages []rawMessage `json:"messages"`
	}
	if json.Unmarshal(body, &req) != nil {
		return false
	}
	for _, m := range req.Messages {
		for _, b := range parseBlocks(m.Content) {
			if b.Type == "image" || b.Type == "document" {
				return true
			}
		}
	}
	return false
}

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
	var calls []block
	uses := map[string]block{}
	results := map[string]string{}
	lastAssistant := -1
	passed := false
	for i, m := range after {
		blocks := parseBlocks(m.Content)
		switch m.Role {
		case "assistant":
			t.Steps++
			lastAssistant = i
			for _, b := range blocks {
				if b.Type == "tool_use" {
					calls = append(calls, b)
					uses[b.ID] = b
					t.EditedFiles = t.EditedFiles || editTools[b.Name]
				}
			}
		case "user":
			newest := i == len(after)-1
			for _, b := range blocks {
				if b.Type != "tool_result" {
					continue
				}
				text := ResultText(b.Content)
				results[b.ToolUseID] = text
				switch {
				case b.IsError || failurePattern.MatchString(text):
					t.FailedResults++
					t.LastResultsFailed = t.LastResultsFailed || newest
				case newest && (verification(uses[b.ToolUseID]) || passPattern.MatchString(text)):
					passed = true
				}
			}
		}
	}
	t.LastResultsPassed = passed && !t.LastResultsFailed
	if lastAssistant >= 0 {
		var text []string
		for _, b := range parseBlocks(after[lastAssistant].Content) {
			if b.Type == "text" {
				text = append(text, b.Text)
			}
		}
		t.Unsure = unsurePattern.MatchString(strings.Join(text, "\n"))
	}
	t.RepeatCount, t.RepeatDetail = repeats(calls, results)
	return t, nil
}

// verification reports whether a tool call runs tests, a linter or a type check.
func verification(call block) bool {
	var in toolInput
	return call.Name == "Bash" && json.Unmarshal(call.Input, &in) == nil && verifyCommand.MatchString(in.Command)
}

// repeats returns the highest count of one tool call made with the same input
// and the same result, and describes that call.
func repeats(calls []block, results map[string]string) (int, string) {
	counts := map[callKey]int{}
	best, top := 0, block{}
	for _, c := range calls {
		text, ok := results[c.ID]
		if !ok {
			continue
		}
		var input bytes.Buffer
		_ = json.Compact(&input, c.Input)
		k := callKey{c.Name, input.String(), sha256.Sum256([]byte(durationPattern.ReplaceAllString(text, "")))}
		counts[k]++
		if counts[k] > best {
			best, top = counts[k], c
		}
	}
	if best < 2 {
		return best, ""
	}
	var in toolInput
	_ = json.Unmarshal(top.Input, &in)
	arg := []rune(cmp.Or(in.Command, in.FilePath, in.Pattern, string(top.Input)))
	if len(arg) > 80 {
		arg = append(arg[:80], '…')
	}
	return best, fmt.Sprintf("%s %q ran %d times with the same result", top.Name, string(arg), best)
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
