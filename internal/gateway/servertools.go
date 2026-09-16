package gateway

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/MrMirhan/intellyrouter/internal/jsonbytes"
	"github.com/MrMirhan/intellyrouter/internal/provider"
)

// dropAdvisorTools removes the advisor server tool when the provider is not
// Anthropic: Anthropic runs that tool on its own servers, and other providers
// reject the request. It reports whether it removed a tool.
func dropAdvisorTools(t target, body []byte) ([]byte, bool, error) {
	if t.config.Type == provider.Anthropic || t.config.Type == provider.AnthropicSubscription {
		return body, false, nil
	}
	return removeAdvisorTools(body, "")
}

// removeAdvisorTools removes advisor server tools. With a model it removes only
// the advisor that uses that model.
func removeAdvisorTools(body []byte, model string) ([]byte, bool, error) {
	spans, err := jsonbytes.TopLevel(body)
	if err != nil {
		return nil, false, err
	}
	sp, ok := spans["tools"]
	if !ok {
		return body, false, nil
	}
	var tools []json.RawMessage
	if err := json.Unmarshal(body[sp.Start:sp.End], &tools); err != nil {
		return nil, false, err
	}
	kept := make([][]byte, 0, len(tools))
	for _, tool := range tools {
		var head struct {
			Type  string `json:"type"`
			Model string `json:"model"`
		}
		if json.Unmarshal(tool, &head) == nil && strings.HasPrefix(head.Type, "advisor_") && (model == "" || head.Model == model) {
			continue
		}
		kept = append(kept, []byte(tool))
	}
	if len(kept) == len(tools) {
		return body, false, nil
	}
	if len(kept) == 0 {
		out, _, err := jsonbytes.RemoveField(body, "tools")
		return out, true, err
	}
	out, err := jsonbytes.SetField(body, "tools", append(append([]byte{'['}, bytes.Join(kept, []byte{','})...), ']'))
	return out, true, err
}
