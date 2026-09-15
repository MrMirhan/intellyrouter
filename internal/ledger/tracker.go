package ledger

import (
	"encoding/json"
	"strings"

	"intellyrouter/internal/jsonbytes"
)

// AnthropicTracker collects usage, stop reason, and errors from an Anthropic
// Messages response, streamed or not. With Capture set it also keeps the
// response message for Message.
type AnthropicTracker struct {
	Usage      Usage
	StopReason string
	Error      string
	Capture    bool
	// Advisors lists advisor model calls, which the top-level usage leaves out.
	Advisors []AdvisorUsage

	stream *messageBuilder
	body   []byte
}

type anthropicUsage struct {
	InputTokens              *int64           `json:"input_tokens"`
	OutputTokens             *int64           `json:"output_tokens"`
	CacheCreationInputTokens *int64           `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     *int64           `json:"cache_read_input_tokens"`
	Iterations               []usageIteration `json:"iterations"`
}

// applyTo overwrites the counters present in u; message_delta usage is cumulative.
func (u anthropicUsage) applyTo(dst *Usage) {
	if u.InputTokens != nil {
		dst.Input = *u.InputTokens
	}
	if u.OutputTokens != nil {
		dst.Output = *u.OutputTokens
	}
	if u.CacheCreationInputTokens != nil {
		dst.CacheWrite = *u.CacheCreationInputTokens
	}
	if u.CacheReadInputTokens != nil {
		dst.CacheRead = *u.CacheReadInputTokens
	}
}

type anthropicError struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (e anthropicError) String() string {
	if e.Error.Message == "" {
		return ""
	}
	return e.Error.Type + ": " + e.Error.Message
}

// Event records one stream event.
func (t *AnthropicTracker) Event(name string, data []byte) {
	if t.Capture {
		if t.stream == nil {
			t.stream = &messageBuilder{usage: make(map[string]json.RawMessage)}
		}
		t.stream.event(name, data)
	}
	switch name {
	case "message_start":
		var ev struct {
			Message struct {
				Usage anthropicUsage `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(data, &ev) == nil {
			ev.Message.Usage.applyTo(&t.Usage)
		}
	case "message_delta":
		var ev struct {
			Delta struct {
				StopReason string `json:"stop_reason"`
			} `json:"delta"`
			Usage anthropicUsage `json:"usage"`
		}
		if json.Unmarshal(data, &ev) == nil {
			ev.Usage.applyTo(&t.Usage)
			t.applyIterations(ev.Usage.Iterations)
			if ev.Delta.StopReason != "" {
				t.StopReason = ev.Delta.StopReason
			}
		}
	case "error":
		var ev anthropicError
		if json.Unmarshal(data, &ev) == nil {
			t.Error = ev.String()
		}
	}
}

// Response records a complete, non-streamed response body.
func (t *AnthropicTracker) Response(status int, body []byte) {
	if status >= 300 {
		var ev anthropicError
		if json.Unmarshal(body, &ev) == nil && ev.String() != "" {
			t.Error = ev.String()
		} else {
			t.Error = truncate(string(body), 500)
		}
		return
	}
	if t.Capture {
		t.body = body
	}
	var m struct {
		StopReason string         `json:"stop_reason"`
		Usage      anthropicUsage `json:"usage"`
	}
	if json.Unmarshal(body, &m) == nil {
		m.Usage.applyTo(&t.Usage)
		t.applyIterations(m.Usage.Iterations)
		t.StopReason = m.StopReason
	}
}

// Message returns the response as an Anthropic message JSON, or nil when
// Capture is off or no message arrived.
func (t *AnthropicTracker) Message() []byte {
	if t.body != nil {
		return t.body
	}
	b := t.stream
	if b == nil || (b.id == "" && len(b.blocks) == 0) {
		return nil
	}
	content := make([]json.RawMessage, 0, len(b.blocks))
	for _, blk := range b.blocks {
		content = append(content, blk.json())
	}
	var stopReason *string
	if t.StopReason != "" {
		stopReason = &t.StopReason
	}
	out, err := json.Marshal(struct {
		ID         string                     `json:"id"`
		Type       string                     `json:"type"`
		Role       string                     `json:"role"`
		Model      string                     `json:"model"`
		Content    []json.RawMessage          `json:"content"`
		StopReason *string                    `json:"stop_reason"`
		Usage      map[string]json.RawMessage `json:"usage"`
	}{b.id, "message", "assistant", b.model, content, stopReason, b.usage})
	if err != nil {
		return nil
	}
	return out
}

// messageBuilder rebuilds a streamed message from its events.
type messageBuilder struct {
	id     string
	model  string
	usage  map[string]json.RawMessage
	blocks []*streamedBlock
}

type streamedBlock struct {
	index     int
	typ       string
	start     json.RawMessage
	text      strings.Builder
	thinking  strings.Builder
	signature strings.Builder
	input     []byte
}

func (m *messageBuilder) event(name string, data []byte) {
	switch name {
	case "message_start":
		var ev struct {
			Message struct {
				ID    string                     `json:"id"`
				Model string                     `json:"model"`
				Usage map[string]json.RawMessage `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(data, &ev) == nil {
			m.id, m.model = ev.Message.ID, ev.Message.Model
			m.mergeUsage(ev.Message.Usage)
		}
	case "content_block_start":
		var ev struct {
			Index        int             `json:"index"`
			ContentBlock json.RawMessage `json:"content_block"`
		}
		if json.Unmarshal(data, &ev) != nil || len(ev.ContentBlock) == 0 {
			return
		}
		var head struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(ev.ContentBlock, &head) == nil {
			m.blocks = append(m.blocks, &streamedBlock{index: ev.Index, typ: head.Type, start: ev.ContentBlock})
		}
	case "content_block_delta":
		var ev struct {
			Index int `json:"index"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				Thinking    string `json:"thinking"`
				Signature   string `json:"signature"`
				PartialJSON string `json:"partial_json"`
			} `json:"delta"`
		}
		if json.Unmarshal(data, &ev) != nil {
			return
		}
		blk := m.block(ev.Index)
		if blk == nil {
			return
		}
		switch ev.Delta.Type {
		case "text_delta":
			blk.text.WriteString(ev.Delta.Text)
		case "thinking_delta":
			blk.thinking.WriteString(ev.Delta.Thinking)
		case "signature_delta":
			blk.signature.WriteString(ev.Delta.Signature)
		case "input_json_delta":
			blk.input = append(blk.input, ev.Delta.PartialJSON...)
		}
	case "message_delta":
		var ev struct {
			Usage map[string]json.RawMessage `json:"usage"`
		}
		if json.Unmarshal(data, &ev) == nil {
			m.mergeUsage(ev.Usage)
		}
	}
}

// mergeUsage overwrites counters that are present; message_delta usage is cumulative.
func (m *messageBuilder) mergeUsage(u map[string]json.RawMessage) {
	for k, v := range u {
		if string(v) != "null" {
			m.usage[k] = v
		}
	}
}

func (m *messageBuilder) block(index int) *streamedBlock {
	for i := len(m.blocks) - 1; i >= 0; i-- {
		if m.blocks[i].index == index {
			return m.blocks[i]
		}
	}
	return nil
}

// json returns the start block with the streamed deltas filled in. Block types
// without deltas stay as they started.
func (b *streamedBlock) json() json.RawMessage {
	out := b.start
	set := func(key string, value any) {
		v, err := json.Marshal(value)
		if err != nil {
			return
		}
		if next, err := jsonbytes.SetField(out, key, v); err == nil {
			out = next
		}
	}
	if b.text.Len() > 0 {
		set("text", b.text.String())
	}
	if b.thinking.Len() > 0 {
		set("thinking", b.thinking.String())
	}
	if b.signature.Len() > 0 {
		set("signature", b.signature.String())
	}
	if len(b.input) > 0 {
		if json.Valid(b.input) {
			set("input", json.RawMessage(b.input))
		} else {
			set("input", string(b.input))
		}
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// usageIteration is one model call inside a response. With the advisor tool,
// Anthropic also calls the advisor model and reports it only here.
type usageIteration struct {
	Type                     string `json:"type"`
	Model                    string `json:"model"`
	InputTokens              int64  `json:"input_tokens"`
	OutputTokens             int64  `json:"output_tokens"`
	CacheCreationInputTokens int64  `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64  `json:"cache_read_input_tokens"`
}

// AdvisorUsage is the usage of one advisor model call.
type AdvisorUsage struct {
	Model string
	Usage Usage
}

// applyIterations keeps the advisor calls of the latest usage, which is cumulative.
func (t *AnthropicTracker) applyIterations(iterations []usageIteration) {
	if len(iterations) == 0 {
		return
	}
	t.Advisors = t.Advisors[:0]
	for _, it := range iterations {
		if it.Type != "advisor_message" {
			continue
		}
		t.Advisors = append(t.Advisors, AdvisorUsage{Model: it.Model, Usage: Usage{
			Input: it.InputTokens, Output: it.OutputTokens, CacheRead: it.CacheReadInputTokens, CacheWrite: it.CacheCreationInputTokens,
		}})
	}
}
