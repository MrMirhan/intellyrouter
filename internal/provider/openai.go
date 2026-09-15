package provider

import (
	"bytes"
	"context"
	"net/http"
)

// NewOpenAIRequest builds a Chat Completions POST. Client headers are not
// forwarded to OpenAI-format providers.
func NewOpenAIRequest(ctx context.Context, c Config, body []byte, stream bool) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL()+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	return req, nil
}

// MaxTokensField is the request field that carries the output token limit.
func (t Type) MaxTokensField() string {
	if t == OpenAI {
		return "max_completion_tokens"
	}
	return "max_tokens"
}
