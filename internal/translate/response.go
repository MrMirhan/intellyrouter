package translate

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
)

type openAIUsage struct {
	PromptTokens         int64 `json:"prompt_tokens"`
	CompletionTokens     int64 `json:"completion_tokens"`
	PromptCacheHitTokens int64 `json:"prompt_cache_hit_tokens"`
	PromptTokensDetails  *struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

type anthropicUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

// anthropic splits cached prompt tokens out of the prompt total, as Anthropic
// reports input_tokens without cache reads. DeepSeek names the field differently.
func (u *openAIUsage) anthropic() anthropicUsage {
	if u == nil {
		return anthropicUsage{}
	}
	cached := u.PromptCacheHitTokens
	if d := u.PromptTokensDetails; d != nil && d.CachedTokens > cached {
		cached = d.CachedTokens
	}
	return anthropicUsage{
		InputTokens:          max(u.PromptTokens-cached, 0),
		OutputTokens:         u.CompletionTokens,
		CacheReadInputTokens: cached,
	}
}

type outBlock struct {
	Type  string          `json:"type"`
	Text  *string         `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type messageOut struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Role         string         `json:"role"`
	Model        string         `json:"model"`
	Content      []outBlock     `json:"content"`
	StopReason   *string        `json:"stop_reason"`
	StopSequence *string        `json:"stop_sequence"`
	Usage        anthropicUsage `json:"usage"`
}

type openAIResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Message struct {
			Content   *string          `json:"content"`
			ToolCalls []openAIToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *openAIUsage `json:"usage"`
}

// Response converts a Chat Completions response body into an Anthropic message.
func Response(body []byte, model string) ([]byte, error) {
	var in openAIResponse
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, err
	}
	if len(in.Choices) == 0 {
		return nil, errors.New("response has no choices")
	}
	c := in.Choices[0]
	out := messageOut{
		ID: messageID(in.ID), Type: "message", Role: "assistant", Model: model,
		Content: []outBlock{}, Usage: in.Usage.anthropic(),
	}
	if t := c.Message.Content; t != nil && *t != "" {
		out.Content = append(out.Content, outBlock{Type: "text", Text: t})
	}
	for _, tc := range c.Message.ToolCalls {
		out.Content = append(out.Content, outBlock{
			Type: "tool_use", ID: toolID(tc.ID), Name: tc.Function.Name, Input: toolInput(tc.Function.Arguments),
		})
	}
	stop := stopReason(c.FinishReason, len(c.Message.ToolCalls) > 0)
	out.StopReason = &stop
	return marshal(out)
}

func stopReason(finish string, hasToolCalls bool) string {
	switch finish {
	case "length":
		return "max_tokens"
	case "content_filter":
		return "refusal"
	case "tool_calls", "function_call":
		return "tool_use"
	}
	if hasToolCalls {
		return "tool_use"
	}
	return "end_turn"
}

func messageID(upstream string) string {
	if upstream == "" {
		return "msg_" + rand.Text()
	}
	return "msg_" + sanitizeID(upstream)
}

func toolID(upstream string) string {
	if upstream == "" {
		return "toolu_" + rand.Text()
	}
	return sanitizeID(upstream)
}

// sanitizeID keeps IDs within Anthropic's [a-zA-Z0-9_-] pattern. The mapping
// is stable, so tool results sent back later still match their calls.
func sanitizeID(id string) string {
	return strings.Map(func(r rune) rune {
		if r == '_' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, id)
}

// toolInput returns the arguments when they form a JSON object; anything else
// becomes an empty object so the client still receives a valid tool_use block.
func toolInput(arguments string) json.RawMessage {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(arguments), &obj); err != nil || obj == nil {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(arguments)
}

type errorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type errorEnvelope struct {
	Type  string      `json:"type"`
	Error errorDetail `json:"error"`
}

// Error converts an upstream error response into an Anthropic error body.
func Error(status int, body []byte) []byte {
	b, _ := marshal(errorEnvelope{Type: "error", Error: errorDetail{Type: errorType(status), Message: upstreamMessage(body)}})
	return b
}

// upstreamMessage extracts the message from OpenAI-style error bodies, including
// Google's array-wrapped variant, and falls back to the raw body.
func upstreamMessage(body []byte) string {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var arr []json.RawMessage
		if json.Unmarshal(trimmed, &arr) == nil && len(arr) > 0 {
			trimmed = arr[0]
		}
	}
	fallback := string(trimmed)
	if len(fallback) > 2000 {
		fallback = fallback[:2000]
	}
	var e struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
	}
	if json.Unmarshal(trimmed, &e) != nil {
		return fallback
	}
	var detail struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(e.Error, &detail) == nil && detail.Message != "" {
		return detail.Message
	}
	var s string
	if json.Unmarshal(e.Error, &s) == nil && s != "" {
		return s
	}
	if e.Message != "" {
		return e.Message
	}
	return fallback
}

func errorType(status int) string {
	switch {
	case status == 401:
		return "authentication_error"
	case status == 403:
		return "permission_error"
	case status == 404:
		return "not_found_error"
	case status == 413:
		return "request_too_large"
	case status == 429:
		return "rate_limit_error"
	case status == 503 || status == 529:
		return "overloaded_error"
	case status >= 500:
		return "api_error"
	}
	return "invalid_request_error"
}

// ThoughtSignatures returns the Gemini thought signatures of a Chat Completions
// response by the tool_use IDs that Response gives the tool calls.
func ThoughtSignatures(body []byte) map[string]string {
	var in openAIResponse
	out := make(map[string]string)
	if json.Unmarshal(body, &in) != nil || len(in.Choices) == 0 {
		return out
	}
	for _, tc := range in.Choices[0].Message.ToolCalls {
		if sig := tc.ExtraContent.signature(); sig != "" && tc.ID != "" {
			out[toolID(tc.ID)] = sig
		}
	}
	return out
}
