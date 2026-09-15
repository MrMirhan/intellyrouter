package provider

import (
	"bytes"
	"context"
	"net/http"
	"slices"
	"strings"
)

const anthropicVersion = "2023-06-01"

// NewAnthropicRequest builds a POST to an Anthropic-format upstream. The body
// and the anthropic-* headers pass through unchanged; client credentials are
// never copied and the provider's key is set instead.
func NewAnthropicRequest(ctx context.Context, c Config, path, rawQuery string, body []byte, in http.Header) (*http.Request, error) {
	url := c.baseURL() + path
	if rawQuery != "" {
		url += "?" + rawQuery
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for name, values := range in {
		if forwardHeader(name) {
			req.Header[name] = slices.Clone(values)
		}
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if req.Header.Get("Anthropic-Version") == "" {
		req.Header.Set("Anthropic-Version", anthropicVersion)
	}
	switch c.Type {
	case Anthropic:
		req.Header.Set("X-Api-Key", c.APIKey)
	case OpenRouter:
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	case AnthropicCompatible:
		// Compatible endpoints differ in which of the two headers they read.
		req.Header.Set("X-Api-Key", c.APIKey)
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	return req, nil
}

func forwardHeader(name string) bool {
	lower := strings.ToLower(name)
	switch lower {
	case "content-type", "accept", "user-agent", "x-app":
		return true
	}
	return strings.HasPrefix(lower, "anthropic-")
}
