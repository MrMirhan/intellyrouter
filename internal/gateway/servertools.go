package gateway

import (
	"bytes"
	"encoding/json"
	"strings"

	"intellyrouter/internal/jsonbytes"
	"intellyrouter/internal/provider"
)

// dropAdvisorTools removes the advisor server tool when the provider is not
// Anthropic: Anthropic runs that tool on its own servers, and other providers
// reject the request.
func dropAdvisorTools(t target, body []byte) ([]byte, error) {
	if t.config.Type == provider.Anthropic || t.config.Type == provider.AnthropicSubscription {
		return body, nil
	}
	spans, err := jsonbytes.TopLevel(body)
	if err != nil {
		return nil, err
	}
	sp, ok := spans["tools"]
	if !ok {
		return body, nil
	}
	var tools []json.RawMessage
	if err := json.Unmarshal(body[sp.Start:sp.End], &tools); err != nil {
		return nil, err
	}
	kept := make([][]byte, 0, len(tools))
	for _, tool := range tools {
		var head struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(tool, &head) == nil && strings.HasPrefix(head.Type, "advisor_") {
			continue
		}
		kept = append(kept, []byte(tool))
	}
	if len(kept) == len(tools) {
		return body, nil
	}
	if len(kept) == 0 {
		out, _, err := jsonbytes.RemoveField(body, "tools")
		return out, err
	}
	return jsonbytes.SetField(body, "tools", append(append([]byte{'['}, bytes.Join(kept, []byte{','})...), ']'))
}
