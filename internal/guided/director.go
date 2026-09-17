package guided

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/MrMirhan/intellyrouter/internal/jsonbytes"
)

// directorSystem starts with a phrase the tests use to recognize director calls.
const directorSystem = `You direct a coding agent. A cheaper executor model does the work: it reads files, runs commands, and writes the code. You do not call tools and you do not write the final code. You see the executor's session so far as text; long tool output is shortened.

At each checkpoint, give the executor guidance for its next steps:
1. What the user needs and what "done" means, in one or two sentences.
2. What is wrong, risky, or missing so far, with exact files, functions, commands, or test names.
3. A short numbered plan for the next steps.

At a review checkpoint, start your reply with APPROVED when the work is complete and correct and nothing else is needed. Otherwise list what must change.

The executor reads your guidance only at checkpoints and cannot wait for you. Do not tell it to wait for your approval. When it must check a decision with you, tell it to call the ask_director tool if it has one.

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
	return consultRequest(body, model, directorSystem, ds.Effort, checkpointText(reason, detail, previous))
}

func consultRequest(body []byte, model, system, effort, last string) ([]byte, error) {
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
	msgs = appendText(msgs, "user", last)
	out := map[string]any{
		"model":      model,
		"max_tokens": 8192,
		"system":     system,
		"messages":   msgs,
	}
	if effort != "" {
		out["output_config"] = map[string]string{"effort": effort}
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

// Markers around the injected guidance. Everything the executor must not
// repeat sits between them, so one closing marker ends the whole block.
const (
	guidanceOpen  = "<director-guidance"
	guidanceClose = "</director-guidance>"
	adviceOpen    = "<advisor-answer"
	adviceClose   = "</advisor-answer>"
)

// injectedBlocks are the wrappers the gateway adds to an executor's request.
// The executor must act on them and never repeat them: a copy reaches the
// user, and it stays in the client's history, where it teaches the executor to
// repeat the block on every later request. Add a wrapper here when you add one
// to a request, or its copies stay in the conversation for good.
var injectedBlocks = [][2]string{
	{guidanceOpen, guidanceClose},
	{adviceOpen, adviceClose},
}

// InjectGuidance appends the director's guidance to the last user message of
// an Anthropic Messages body. Earlier messages stay byte-identical, so the
// executor's prompt cache still covers them.
func InjectGuidance(body []byte, guidance, reason string) ([]byte, error) {
	text := fmt.Sprintf("%s checkpoint=%q>\nA senior director reviewed your session and wrote this. "+
		"Follow it unless the code or tool results clearly contradict it.\n\n%s\n\n"+
		"Never copy these lines into your reply: the user must not see them. Start your reply with the work.\n%s",
		guidanceOpen, reason, guidance, guidanceClose)
	return appendUserText(body, text)
}

// StripEchoedBlocks removes the injected blocks that the executor copied into
// its own reply. A weak executor sometimes repeats a block instead of acting
// on it, and the copy stays in the client's history: from then on the executor
// reads its own replies as a house style, repeats the block again, and the
// turn stops making progress. The second return value reports whether the body
// changed, so a clean request keeps its bytes and its prompt cache.
func StripEchoedBlocks(body []byte) ([]byte, bool, error) {
	if !hasInjectedBlock(body) {
		return body, false, nil
	}
	spans, err := jsonbytes.TopLevel(body)
	if err != nil {
		return nil, false, err
	}
	sp, ok := spans["messages"]
	if !ok {
		return body, false, nil
	}
	var msgs []json.RawMessage
	if err := json.Unmarshal(body[sp.Start:sp.End], &msgs); err != nil {
		return nil, false, err
	}
	changed := false
	for i, raw := range msgs {
		var m rawMessage
		if json.Unmarshal(raw, &m) != nil || m.Role != "assistant" {
			continue
		}
		if !hasInjectedBlock(raw) {
			continue
		}
		cleaned, ok := cutMessageBlocks(m)
		if !ok {
			continue
		}
		msgs[i] = cleaned
		changed = true
	}
	if !changed {
		return body, false, nil
	}
	replaced, err := json.Marshal(msgs)
	if err != nil {
		return nil, false, err
	}
	out := make([]byte, 0, len(body)+len(replaced)-(sp.End-sp.Start))
	out = append(out, body[:sp.Start]...)
	out = append(out, replaced...)
	out = append(out, body[sp.End:]...)
	return out, true, nil
}

// cutMessageBlocks rewrites one assistant message without its injected
// blocks. It reports false when nothing in the message changed.
func cutMessageBlocks(m rawMessage) (json.RawMessage, bool) {
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		cut := cutBlocks(s)
		if cut == s {
			return nil, false
		}
		out, err := json.Marshal(struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{m.Role, cut})
		return out, err == nil
	}
	var blocks []json.RawMessage
	if json.Unmarshal(m.Content, &blocks) != nil {
		return nil, false
	}
	changed := false
	kept := blocks[:0]
	for _, raw := range blocks {
		var b block
		if json.Unmarshal(raw, &b) != nil || b.Type != "text" {
			kept = append(kept, raw)
			continue
		}
		cut := cutBlocks(b.Text)
		if cut == b.Text {
			kept = append(kept, raw)
			continue
		}
		changed = true
		if strings.TrimSpace(cut) == "" {
			continue
		}
		replaced, err := json.Marshal(textBlock{Type: "text", Text: cut})
		if err != nil {
			return nil, false
		}
		kept = append(kept, replaced)
	}
	if !changed {
		return nil, false
	}
	// An assistant message needs at least one block to stay valid.
	if len(kept) == 0 {
		empty, err := json.Marshal(textBlock{Type: "text", Text: "(continuing)"})
		if err != nil {
			return nil, false
		}
		kept = append(kept, empty)
	}
	out, err := json.Marshal(struct {
		Role    string            `json:"role"`
		Content []json.RawMessage `json:"content"`
	}{m.Role, kept})
	return out, err == nil
}

// hasInjectedBlock reports whether b mentions the opening marker of any block
// the gateway injects.
func hasInjectedBlock(b []byte) bool {
	for _, mark := range injectedBlocks {
		if bytes.Contains(b, []byte(mark[0])) {
			return true
		}
	}
	return false
}

// cutBlocks removes every injected block from s. A block that lost its closing
// marker on the way through the model runs to the end of the text.
func cutBlocks(s string) string {
	for {
		at, mark := -1, [2]string{}
		for _, m := range injectedBlocks {
			if i := strings.Index(s, m[0]); i >= 0 && (at < 0 || i < at) {
				at, mark = i, m
			}
		}
		if at < 0 {
			return s
		}
		rest := s[at+len(mark[0]):]
		j := strings.Index(rest, mark[1])
		if j < 0 {
			return strings.TrimRight(s[:at], " \t\n")
		}
		s = s[:at] + rest[j+len(mark[1]):]
	}
}

// appendUserText appends a text block to the last user message, or a user
// message when the last message is not the user's. Earlier messages stay
// byte-identical, so the executor's prompt cache still covers them.
func appendUserText(body []byte, text string) ([]byte, error) {
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
	newBlock, err := json.Marshal(textBlock{Type: "text", Text: text})
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
		content = append(content, newBlock)
		updated, err := jsonbytes.SetField(msgs[n-1], "content", joinArray(content))
		if err != nil {
			return nil, err
		}
		msgs[n-1] = updated
	} else {
		msgs = append(msgs, json.RawMessage(`{"role":"user","content":[`+string(newBlock)+`]}`))
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
	Guidance       string
	GuidanceReason string
	// GuidanceSent is set once the executor has read the current guidance, so
	// the next request marks it as a reminder of work already under way.
	GuidanceSent       bool
	DirectorCalls      int
	LastCheckpointStep int
	LastFailedResults  int
	LastRepeatCount    int
	FailureCheckpoints int
	Reviewed           bool
	Tier               int
	// SeenStep and SeenFailures are the step and failure counts of the previous
	// request, which is how a turn above the base tier measures clean progress.
	SeenStep     int
	SeenFailures int
	// CleanSteps counts the steps since the last new failure while the turn is
	// above the base tier.
	CleanSteps   int
	AdvisorCalls int
	// Advice is the advisor's latest answer in the turn, to AdviceQuestion.
	Advice         string
	AdviceQuestion string
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
