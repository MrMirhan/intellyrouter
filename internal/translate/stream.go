package translate

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Stream converts Chat Completions stream chunks into Anthropic stream events.
// Text is emitted as it arrives. Tool calls are buffered and emitted whole when
// the upstream stream ends, because providers interleave, repeat, or delay
// tool call fragments.
type Stream struct {
	model    string
	emit     func(event string, data []byte) error
	started  bool
	index    int
	textOpen bool
	tools    []*pendingTool
	byIndex  map[int]*pendingTool
	finish   string
	usage    anthropicUsage
}

type pendingTool struct {
	id   string
	name string
	args strings.Builder
}

// UpstreamError reports an error chunk inside an otherwise successful stream.
type UpstreamError struct {
	Message string
}

func (e *UpstreamError) Error() string { return "upstream stream error: " + e.Message }

func NewStream(model string, emit func(event string, data []byte) error) *Stream {
	return &Stream{model: model, emit: emit, byIndex: make(map[int]*pendingTool)}
}

type chunk struct {
	ID      string `json:"id"`
	Choices []struct {
		Delta struct {
			Content   *string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *openAIUsage    `json:"usage"`
	Error json.RawMessage `json:"error"`
}

type blockStart struct {
	Type         string   `json:"type"`
	Index        int      `json:"index"`
	ContentBlock outBlock `json:"content_block"`
}

type blockDelta struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta any    `json:"delta"`
}

type textDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type jsonDelta struct {
	Type        string `json:"type"`
	PartialJSON string `json:"partial_json"`
}

type blockStop struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
}

type messageDelta struct {
	Type  string `json:"type"`
	Delta struct {
		StopReason   string  `json:"stop_reason"`
		StopSequence *string `json:"stop_sequence"`
	} `json:"delta"`
	Usage anthropicUsage `json:"usage"`
}

// Chunk handles the data of one upstream event. For an error chunk it emits an
// Anthropic error event and returns *UpstreamError.
func (s *Stream) Chunk(data []byte) error {
	var c chunk
	if err := json.Unmarshal(data, &c); err != nil {
		return fmt.Errorf("decode upstream chunk: %w", err)
	}
	if len(c.Error) > 0 && string(c.Error) != "null" {
		msg := upstreamMessage(data)
		if err := s.event("error", errorEnvelope{Type: "error", Error: errorDetail{Type: "api_error", Message: msg}}); err != nil {
			return err
		}
		return &UpstreamError{Message: msg}
	}
	if err := s.start(c.ID); err != nil {
		return err
	}
	if c.Usage != nil {
		s.usage = c.Usage.anthropic()
	}
	for _, ch := range c.Choices {
		if t := ch.Delta.Content; t != nil && *t != "" {
			if err := s.text(*t); err != nil {
				return err
			}
		}
		for _, tc := range ch.Delta.ToolCalls {
			p := s.byIndex[tc.Index]
			// Some providers reuse index 0 for every call and tell them apart by ID.
			if p == nil || (tc.ID != "" && p.id != "" && tc.ID != p.id) {
				p = &pendingTool{}
				s.byIndex[tc.Index] = p
				s.tools = append(s.tools, p)
			}
			if tc.ID != "" {
				p.id = tc.ID
			}
			if tc.Function.Name != "" {
				p.name = tc.Function.Name
			}
			p.args.WriteString(tc.Function.Arguments)
		}
		if ch.FinishReason != nil && *ch.FinishReason != "" {
			s.finish = *ch.FinishReason
		}
	}
	return nil
}

// Finish closes the text block, emits buffered tool calls, and ends the
// message. Call it once when the upstream stream ends.
func (s *Stream) Finish() error {
	if err := s.start(""); err != nil {
		return err
	}
	if err := s.closeText(); err != nil {
		return err
	}
	for _, p := range s.tools {
		start := blockStart{Type: "content_block_start", Index: s.index,
			ContentBlock: outBlock{Type: "tool_use", ID: toolID(p.id), Name: p.name, Input: json.RawMessage(`{}`)}}
		if err := s.event("content_block_start", start); err != nil {
			return err
		}
		if input := toolInput(p.args.String()); string(input) != "{}" {
			if err := s.event("content_block_delta", blockDelta{Type: "content_block_delta", Index: s.index,
				Delta: jsonDelta{Type: "input_json_delta", PartialJSON: string(input)}}); err != nil {
				return err
			}
		}
		if err := s.event("content_block_stop", blockStop{Type: "content_block_stop", Index: s.index}); err != nil {
			return err
		}
		s.index++
	}
	md := messageDelta{Type: "message_delta", Usage: s.usage}
	md.Delta.StopReason = stopReason(s.finish, len(s.tools) > 0)
	if err := s.event("message_delta", md); err != nil {
		return err
	}
	return s.event("message_stop", struct {
		Type string `json:"type"`
	}{"message_stop"})
}

func (s *Stream) start(upstreamID string) error {
	if s.started {
		return nil
	}
	s.started = true
	return s.event("message_start", struct {
		Type    string     `json:"type"`
		Message messageOut `json:"message"`
	}{"message_start", messageOut{ID: messageID(upstreamID), Type: "message", Role: "assistant", Model: s.model, Content: []outBlock{}}})
}

func (s *Stream) text(t string) error {
	if !s.textOpen {
		empty := ""
		if err := s.event("content_block_start", blockStart{Type: "content_block_start", Index: s.index,
			ContentBlock: outBlock{Type: "text", Text: &empty}}); err != nil {
			return err
		}
		s.textOpen = true
	}
	return s.event("content_block_delta", blockDelta{Type: "content_block_delta", Index: s.index,
		Delta: textDelta{Type: "text_delta", Text: t}})
}

func (s *Stream) closeText() error {
	if !s.textOpen {
		return nil
	}
	s.textOpen = false
	err := s.event("content_block_stop", blockStop{Type: "content_block_stop", Index: s.index})
	s.index++
	return err
}

func (s *Stream) event(name string, v any) error {
	b, err := marshal(v)
	if err != nil {
		return err
	}
	return s.emit(name, b)
}
