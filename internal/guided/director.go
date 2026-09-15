package guided

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"intellyrouter/internal/jsonbytes"
)

// directorSystem starts with a phrase the tests use to recognize director calls.
const directorSystem = `You direct a coding agent. A cheaper executor model does the work: it reads files, runs commands, and writes the code. You do not call tools and you do not write the final code. You see the executor's session so far as text; long tool output is shortened.

At each checkpoint, give the executor guidance for its next steps:
1. What the user needs and what "done" means, in one or two sentences.
2. What is wrong, risky, or missing so far, with exact files, functions, commands, or test names.
3. A short numbered plan for the next steps.

At a review checkpoint, start your reply with APPROVED when the work is complete and correct and nothing else is needed. Otherwise list what must change.

Be direct and concise: at most 250 words. Do not paste large code blocks.`

const (
	maxToolInput = 6000
	maxToolHead  = 2500
	maxToolTail  = 1500
)

type textBlock struct {
	Type         string         `json:"type"`
	Text         string         `json:"text"`
	CacheControl map[string]any `json:"cache_control,omitempty"`
}

type textMessage struct {
	Role    string      `json:"role"`
	Content []textBlock `json:"content"`
}

// DirectorRequest builds a non-streaming Anthropic Messages body that shows the
// director the executor's session as plain text, so it needs no tools or betas.
func DirectorRequest(body []byte, model string, ds DirectorSettings, reason, detail, previous string) ([]byte, error) {
	var req struct {
		Messages []rawMessage `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	msgs := transcript(req.Messages)
	// Later checkpoints in the same conversation read this prefix from cache.
	if n := len(msgs); n > 0 {
		blocks := msgs[n-1].Content
		blocks[len(blocks)-1].CacheControl = map[string]any{"type": "ephemeral"}
	}
	checkpoint := "Checkpoint: " + reason
	if detail != "" {
		checkpoint += " (" + detail + ")"
	}
	if previous != "" {
		checkpoint += "\n\nYour previous guidance in this turn:\n" + previous
	}
	msgs = appendText(msgs, "user", checkpoint+"\n\nGive your guidance now.")
	out := map[string]any{
		"model":      model,
		"max_tokens": 8192,
		"system":     directorSystem,
		"messages":   msgs,
	}
	if ds.Effort != "" {
		out["output_config"] = map[string]string{"effort": ds.Effort}
	}
	return json.Marshal(out)
}

func transcript(messages []rawMessage) []textMessage {
	var out []textMessage
	for _, m := range messages {
		role := m.Role
		if role != "assistant" {
			role = "user"
		}
		for _, b := range parseBlocks(m.Content) {
			if text := blockText(b); text != "" {
				out = appendText(out, role, text)
			}
		}
	}
	if len(out) > 0 && out[0].Role != "user" {
		out = append([]textMessage{{Role: "user", Content: []textBlock{{Type: "text", Text: "(session start)"}}}}, out...)
	}
	return out
}

func blockText(b block) string {
	switch b.Type {
	case "text":
		return strings.TrimSpace(b.Text)
	case "tool_use":
		return fmt.Sprintf("[tool call %s] %s", b.Name, shorten(compactJSON(b.Input), maxToolInput, 0))
	case "tool_result":
		label := "[tool result]"
		if b.IsError {
			label = "[tool error]"
		}
		return label + " " + shorten(ResultText(b.Content), maxToolHead, maxToolTail)
	case "thinking", "redacted_thinking", "":
		return ""
	}
	return "[" + b.Type + " omitted]"
}

func appendText(msgs []textMessage, role, text string) []textMessage {
	if n := len(msgs); n > 0 && msgs[n-1].Role == role {
		msgs[n-1].Content = append(msgs[n-1].Content, textBlock{Type: "text", Text: text})
		return msgs
	}
	return append(msgs, textMessage{Role: role, Content: []textBlock{{Type: "text", Text: text}}})
}

// shorten keeps the head and tail of long text; the cut depends only on the
// text, so repeated transcripts stay identical for prompt caching.
func shorten(s string, head, tail int) string {
	if len(s) <= head+tail {
		return s
	}
	return s[:head] + fmt.Sprintf("\n…[%d characters omitted]…\n", len(s)-head-tail) + s[len(s)-tail:]
}

func compactJSON(raw json.RawMessage) string {
	var buf bytes.Buffer
	if json.Compact(&buf, raw) != nil {
		return string(raw)
	}
	return buf.String()
}

// ParseGuidance reads the director's reply from an Anthropic message body.
func ParseGuidance(message []byte) (guidance string, approved bool, err error) {
	var m struct {
		Content []block `json:"content"`
	}
	if err := json.Unmarshal(message, &m); err != nil {
		return "", false, err
	}
	var parts []string
	for _, b := range m.Content {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			parts = append(parts, strings.TrimSpace(b.Text))
		}
	}
	guidance = strings.Join(parts, "\n\n")
	if guidance == "" {
		return "", false, errors.New("the director returned no text")
	}
	return guidance, strings.HasPrefix(strings.ToUpper(guidance), "APPROVED"), nil
}

// InjectGuidance appends the director's guidance to the last user message of
// an Anthropic Messages body. Earlier messages stay byte-identical, so the
// executor's prompt cache still covers them.
func InjectGuidance(body []byte, guidance, reason string) ([]byte, error) {
	spans, err := jsonbytes.TopLevel(body)
	if err != nil {
		return nil, err
	}
	sp, ok := spans["messages"]
	if !ok {
		return nil, errors.New("request has no messages")
	}
	var msgs []json.RawMessage
	if err := json.Unmarshal(body[sp.Start:sp.End], &msgs); err != nil {
		return nil, err
	}
	text := fmt.Sprintf("<director-guidance checkpoint=%q>\n%s\n</director-guidance>\n"+
		"A senior director reviewed your session and wrote the guidance above. Follow it unless the code or tool results clearly contradict it. Do not quote it to the user.",
		reason, guidance)
	guidanceBlock, err := json.Marshal(textBlock{Type: "text", Text: text})
	if err != nil {
		return nil, err
	}

	var last rawMessage
	if n := len(msgs); n > 0 && json.Unmarshal(msgs[n-1], &last) == nil && last.Role == "user" {
		var content []json.RawMessage
		var s string
		if json.Unmarshal(last.Content, &s) == nil {
			first, err := json.Marshal(textBlock{Type: "text", Text: s})
			if err != nil {
				return nil, err
			}
			content = []json.RawMessage{first}
		} else if err := json.Unmarshal(last.Content, &content); err != nil {
			return nil, err
		}
		content = append(content, guidanceBlock)
		updated, err := jsonbytes.SetField(msgs[n-1], "content", joinArray(content))
		if err != nil {
			return nil, err
		}
		msgs[n-1] = updated
	} else {
		msgs = append(msgs, json.RawMessage(`{"role":"user","content":[`+string(guidanceBlock)+`]}`))
	}
	return jsonbytes.SetField(body, "messages", joinArray(msgs))
}

func joinArray(items []json.RawMessage) []byte {
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, item := range items {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(item)
	}
	buf.WriteByte(']')
	return buf.Bytes()
}

// State is what the gateway remembers about one turn.
type State struct {
	Guidance           string
	GuidanceReason     string
	DirectorCalls      int
	LastCheckpointStep int
	LastFailedResults  int
	FailureCheckpoints int
	Reviewed           bool
	Tier               int
}

// Tracker keeps turn states in memory for a limited time.
type Tracker struct {
	mu    sync.Mutex
	turns map[string]trackedState
	ttl   time.Duration
}

type trackedState struct {
	state   State
	expires time.Time
}

const maxTrackedTurns = 10000

func NewTracker(ttl time.Duration) *Tracker {
	return &Tracker{turns: make(map[string]trackedState), ttl: ttl}
}

// Get returns the state of a turn, or a fresh state for an unknown turn.
func (t *Tracker) Get(key string) State {
	t.mu.Lock()
	defer t.mu.Unlock()
	if ts, ok := t.turns[key]; ok && time.Now().Before(ts.expires) {
		return ts.state
	}
	return State{LastCheckpointStep: -1}
}

func (t *Tracker) Put(key string, st State) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	if len(t.turns) >= maxTrackedTurns {
		for k, v := range t.turns {
			if now.After(v.expires) {
				delete(t.turns, k)
			}
		}
	}
	t.turns[key] = trackedState{state: st, expires: now.Add(t.ttl)}
}
