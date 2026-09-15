package ledger

import "encoding/json"

// AnthropicTracker collects usage, stop reason, and errors from an Anthropic
// Messages response, streamed or not.
type AnthropicTracker struct {
	Usage      Usage
	StopReason string
	Error      string
}

type anthropicUsage struct {
	InputTokens              *int64 `json:"input_tokens"`
	OutputTokens             *int64 `json:"output_tokens"`
	CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
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
	var m struct {
		StopReason string         `json:"stop_reason"`
		Usage      anthropicUsage `json:"usage"`
	}
	if json.Unmarshal(body, &m) == nil {
		m.Usage.applyTo(&t.Usage)
		t.StopReason = m.StopReason
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
