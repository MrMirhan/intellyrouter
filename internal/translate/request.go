// Package translate converts between the Anthropic Messages API and the
// OpenAI Chat Completions API.
package translate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type Options struct {
	// Model is the upstream model ID.
	Model string
	// MaxTokensField is "max_tokens" or "max_completion_tokens".
	MaxTokensField string
	// ThoughtSignature returns the recorded Gemini thought signature of a tool
	// call. When it is set, every replayed tool call carries a signature.
	ThoughtSignature func(toolUseID string) string
}

// openAIToolCap keeps Codex-friendly tool lists small. Some upstreams (notably
// 9router's Codex backend) answer an OpenAI Responses call with an empty body
// when the tool count is huge, which is what Claude Code sees as a 36-second
// wait and then no response.
const openAIToolCap = 64

// dedupeOpenAITools drops tools with the same name. HEADROOM between Claude
// Code and the gateway has been seen duplicating the full Claude tool set on
// every turn, blowing a normal 20-tool list up past a thousand. The OpenAI
// endpoint then refuses to answer.
func dedupeOpenAITools(in []anthropicTool) []openAITool {
	seen := make(map[string]bool, len(in))
	out := make([]openAITool, 0, len(in))
	for _, t := range in {
		if t.Type != "" && t.Type != "custom" {
			continue
		}
		if seen[t.Name] {
			continue
		}
		seen[t.Name] = true
		out = append(out, openAITool{
			Type:     "function",
			Function: openAIFunction{Name: t.Name, Description: t.Description, Parameters: t.InputSchema},
		})
	}
	return out
}

type anthropicRequest struct {
	MaxTokens     *int                 `json:"max_tokens"`
	System        json.RawMessage      `json:"system"`
	Messages      []anthropicMessage   `json:"messages"`
	Tools         []anthropicTool      `json:"tools"`
	ToolChoice    *anthropicToolChoice `json:"tool_choice"`
	StopSequences []string             `json:"stop_sequences"`
	Temperature   *float64             `json:"temperature"`
	TopP          *float64             `json:"top_p"`
	Stream        bool                 `json:"stream"`
}

type anthropicMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
	Source    *imageSource    `json:"source"`
}

type imageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
	URL       string `json:"url"`
}

type anthropicTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicToolChoice struct {
	Type                   string `json:"type"`
	Name                   string `json:"name"`
	DisableParallelToolUse bool   `json:"disable_parallel_tool_use"`
}

type openAIRequest struct {
	Model               string          `json:"model"`
	Messages            []openAIMessage `json:"messages"`
	MaxTokens           *int            `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int            `json:"max_completion_tokens,omitempty"`
	Tools               []openAITool    `json:"tools,omitempty"`
	ToolChoice          any             `json:"tool_choice,omitempty"`
	ParallelToolCalls   *bool           `json:"parallel_tool_calls,omitempty"`
	Stop                []string        `json:"stop,omitempty"`
	Temperature         *float64        `json:"temperature,omitempty"`
	TopP                *float64        `json:"top_p,omitempty"`
	Stream              bool            `json:"stream,omitempty"`
	StreamOptions       *streamOptions  `json:"stream_options,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

type openAIFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type openAIToolCall struct {
	ID           string             `json:"id"`
	Type         string             `json:"type"`
	Function     openAICallFunction `json:"function"`
	ExtraContent *extraContent      `json:"extra_content,omitempty"`
}

type openAICallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// extraContent carries provider fields; Gemini puts thought signatures here.
type extraContent struct {
	Google struct {
		ThoughtSignature string `json:"thought_signature,omitempty"`
	} `json:"google"`
}

func (e *extraContent) signature() string {
	if e == nil {
		return ""
	}
	return e.Google.ThoughtSignature
}

// SkipThoughtSignature is Gemini's value for a function call without a
// recorded signature, such as a call that another model wrote.
const SkipThoughtSignature = "skip_thought_signature_validator"

// Request converts an Anthropic Messages request body into a Chat Completions
// request body. Thinking blocks, cache_control, and server tools have no
// Chat Completions equivalent and are dropped.
func Request(body []byte, opts Options) ([]byte, error) {
	var in anthropicRequest
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, err
	}
	out := openAIRequest{Model: opts.Model, Stop: in.StopSequences, Temperature: in.Temperature, TopP: in.TopP}
	if opts.MaxTokensField == "max_completion_tokens" {
		out.MaxCompletionTokens = in.MaxTokens
	} else {
		out.MaxTokens = in.MaxTokens
	}
	if in.Stream {
		out.Stream = true
		out.StreamOptions = &streamOptions{IncludeUsage: true}
	}

	system, err := systemText(in.System)
	if err != nil {
		return nil, err
	}
	if system != "" {
		out.Messages = append(out.Messages, openAIMessage{Role: "system", Content: system})
	}
	for i, m := range in.Messages {
		msgs, err := convertMessage(m, opts.ThoughtSignature)
		if err != nil {
			return nil, fmt.Errorf("messages[%d]: %w", i, err)
		}
		out.Messages = append(out.Messages, msgs...)
	}

	out.Tools = dedupeOpenAITools(in.Tools)
	if len(out.Tools) > openAIToolCap {
		out.Tools = out.Tools[:openAIToolCap]
	}
	if tc := in.ToolChoice; tc != nil && len(out.Tools) > 0 {
		switch tc.Type {
		case "auto":
			out.ToolChoice = "auto"
		case "any":
			out.ToolChoice = "required"
		case "none":
			out.ToolChoice = "none"
		case "tool":
			out.ToolChoice = map[string]any{"type": "function", "function": map[string]string{"name": tc.Name}}
		}
		if tc.DisableParallelToolUse {
			no := false
			out.ParallelToolCalls = &no
		}
	}
	return marshal(out)
}

func systemText(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	blocks, text, err := parseContent(raw)
	if err != nil {
		return "", fmt.Errorf("system: %w", err)
	}
	if blocks == nil {
		return text, nil
	}
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

// parseContent returns the blocks of array content, or the text of string content.
func parseContent(raw json.RawMessage) ([]contentBlock, string, error) {
	if len(raw) == 0 {
		return nil, "", nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return nil, s, nil
	}
	blocks := []contentBlock{}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, "", fmt.Errorf("content: %w", err)
	}
	return blocks, "", nil
}

func convertMessage(m anthropicMessage, signature func(string) string) ([]openAIMessage, error) {
	blocks, text, err := parseContent(m.Content)
	if err != nil {
		return nil, err
	}
	switch m.Role {
	case "assistant":
		if blocks == nil {
			return []openAIMessage{{Role: "assistant", Content: text}}, nil
		}
		return []openAIMessage{assistantMessage(blocks, signature)}, nil
	case "user", "system":
		if blocks == nil {
			return []openAIMessage{{Role: m.Role, Content: text}}, nil
		}
		return userMessages(m.Role, blocks), nil
	}
	return nil, fmt.Errorf("unsupported role %q", m.Role)
}

func assistantMessage(blocks []contentBlock, signature func(string) string) openAIMessage {
	msg := openAIMessage{Role: "assistant"}
	var text []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			text = append(text, b.Text)
		case "tool_use":
			args := string(b.Input)
			if args == "" || args == "null" {
				args = "{}"
			}
			call := openAIToolCall{
				ID: b.ID, Type: "function", Function: openAICallFunction{Name: b.Name, Arguments: args},
			}
			if signature != nil {
				call.ExtraContent = &extraContent{}
				call.ExtraContent.Google.ThoughtSignature = signature(b.ID)
				if call.ExtraContent.Google.ThoughtSignature == "" {
					call.ExtraContent.Google.ThoughtSignature = SkipThoughtSignature
				}
			}
			msg.ToolCalls = append(msg.ToolCalls, call)
		}
	}
	if len(text) > 0 || len(msg.ToolCalls) == 0 {
		msg.Content = strings.Join(text, "")
	}
	return msg
}

// userMessages emits tool results first, because Chat Completions requires
// tool messages to follow the assistant message that made the calls.
func userMessages(role string, blocks []contentBlock) []openAIMessage {
	var out []openAIMessage
	var parts []contentPart
	for _, b := range blocks {
		switch b.Type {
		case "tool_result":
			out = append(out, openAIMessage{Role: "tool", ToolCallID: b.ToolUseID, Content: toolResultText(b)})
		case "text":
			parts = append(parts, contentPart{Type: "text", Text: b.Text})
		case "image":
			if u := imageDataURL(b.Source); u != "" {
				parts = append(parts, contentPart{Type: "image_url", ImageURL: &imageURL{URL: u}})
			}
		default:
			parts = append(parts, contentPart{Type: "text", Text: fmt.Sprintf("[%s block omitted: not supported by this model]", b.Type)})
		}
	}
	if len(parts) > 0 {
		out = append(out, openAIMessage{Role: role, Content: simplifyParts(parts)})
	}
	return out
}

func toolResultText(b contentBlock) string {
	blocks, text, err := parseContent(b.Content)
	if err != nil {
		text = string(b.Content)
	}
	if blocks != nil {
		parts := make([]string, 0, len(blocks))
		for _, c := range blocks {
			if c.Type == "text" {
				parts = append(parts, c.Text)
			} else {
				parts = append(parts, fmt.Sprintf("[%s omitted]", c.Type))
			}
		}
		text = strings.Join(parts, "\n")
	}
	if b.IsError {
		return "Error: " + text
	}
	return text
}

func imageDataURL(src *imageSource) string {
	if src == nil {
		return ""
	}
	switch src.Type {
	case "base64":
		return "data:" + src.MediaType + ";base64," + src.Data
	case "url":
		return src.URL
	}
	return ""
}

// simplifyParts sends text-only content as a plain string, which every
// compatible provider accepts.
func simplifyParts(parts []contentPart) any {
	texts := make([]string, 0, len(parts))
	for _, p := range parts {
		if p.Type != "text" {
			return parts
		}
		texts = append(texts, p.Text)
	}
	return strings.Join(texts, "\n\n")
}

func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
