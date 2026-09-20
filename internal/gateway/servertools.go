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
// reject the request. It reports whether it changed the body.
//
// The tool definition is not the whole of it. Once an Anthropic tier has used
// the advisor, every later request carries the advisor_tool_use and
// advisor_tool_result blocks it produced, and a provider that never knew the
// tool rejects those blocks as an unsupported content type. So the blocks go
// with the definition.
func dropAdvisorTools(t target, body []byte) ([]byte, bool, error) {
	if t.config.Type == provider.Anthropic || t.config.Type == provider.AnthropicSubscription {
		return body, false, nil
	}
	out, tools, err := removeAdvisorTools(body, "")
	if err != nil {
		return nil, false, err
	}
	out, blocks, err := removeContentBlocks(out, func(kind string) bool {
		return strings.HasPrefix(kind, "advisor_")
	})
	return out, tools || blocks, err
}

// removeContentBlocks drops the content blocks that match from every message,
// and the message itself when nothing is left of it.
func removeContentBlocks(body []byte, match func(kind string) bool) ([]byte, bool, error) {
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
	kept, changed := make([][]byte, 0, len(msgs)), false
	for _, m := range msgs {
		var msg struct {
			Content json.RawMessage `json:"content"`
		}
		var content []json.RawMessage
		// A message whose content is a plain string carries no blocks.
		if json.Unmarshal(m, &msg) != nil || json.Unmarshal(msg.Content, &content) != nil {
			kept = append(kept, m)
			continue
		}
		blocks := make([][]byte, 0, len(content))
		for _, b := range content {
			var head struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(b, &head) == nil && match(head.Type) {
				changed = true
				continue
			}
			blocks = append(blocks, b)
		}
		if len(blocks) == len(content) {
			kept = append(kept, m)
			continue
		}
		// An empty message is itself invalid, so it goes with its blocks.
		if len(blocks) == 0 {
			continue
		}
		out, err := jsonbytes.SetField(m, "content", append(append([]byte{'['}, bytes.Join(blocks, []byte{','})...), ']'))
		if err != nil {
			return nil, false, err
		}
		kept = append(kept, out)
	}
	if !changed {
		return body, false, nil
	}
	out, err := jsonbytes.SetField(body, "messages", append(append([]byte{'['}, bytes.Join(kept, []byte{','})...), ']'))
	return out, err == nil, err
}

// removeTool removes one tool from the request's tool list by name. A provider
// that validates tool schemas against its own meta-schema can reject a tool
// Anthropic accepts; the request cannot proceed until that tool is gone.
func removeTool(body []byte, name string) ([]byte, bool, error) {
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
			Name string `json:"name"`
		}
		if json.Unmarshal(tool, &head) == nil && head.Name == name {
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

// removeToolType removes every tool whose server-tool tag is type, such as
// advisor_20260120. A provider deployment that predates a newer Claude Code
// tag rejects the request naming that exact tag; the tag, not the tool name,
// identifies what to drop.
func removeToolType(body []byte, typ string) ([]byte, bool, error) {
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
			Type string `json:"type"`
		}
		if json.Unmarshal(tool, &head) == nil && head.Type == typ {
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

// removeToolWithSchema removes the tool whose input schema contains the
// fragment a provider rejected. Some providers report an invalid schema
// without naming the tool, so the fragment itself is the only handle on it.
// fragment is the provider's JSON with all whitespace removed.
func removeToolWithSchema(body []byte, fragment string) ([]byte, bool, error) {
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
	// The provider prints the schema with its own key order, so compare the
	// set of "key":value pairs rather than the whole object.
	parts := strings.FieldsFunc(strings.Trim(fragment, "{}"), func(r rune) bool { return r == ',' })
	if len(parts) == 0 {
		return body, false, nil
	}
	kept := make([][]byte, 0, len(tools))
	dropped := false
	for _, tool := range tools {
		compact := string(compactNoSpace(tool))
		hit := !dropped
		for _, p := range parts {
			if !strings.Contains(compact, p) {
				hit = false
				break
			}
		}
		if hit {
			dropped = true
			continue
		}
		kept = append(kept, []byte(tool))
	}
	if !dropped {
		return body, false, nil
	}
	if len(kept) == 0 {
		out, _, err := jsonbytes.RemoveField(body, "tools")
		return out, true, err
	}
	out, err := jsonbytes.SetField(body, "tools", append(append([]byte{'['}, bytes.Join(kept, []byte{','})...), ']'))
	return out, true, err
}

// compactNoSpace returns raw as compact JSON with no whitespace at all, so a
// provider's reformatted schema fragment can be matched against it.
func compactNoSpace(raw json.RawMessage) []byte {
	var buf bytes.Buffer
	if json.Compact(&buf, raw) != nil {
		return raw
	}
	return buf.Bytes()
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
