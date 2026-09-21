package gateway

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/MrMirhan/intellyrouter/internal/jsonbytes"
)

// shortenToolNames rewrites every tool name longer than limit so the request
// fits a provider that caps names below Anthropic's own limit. Claude Code's
// MCP tools are named "mcp__<server>__<tool>" and pass 64 characters often
// enough that dropping them would cost the executor real tools.
//
// A shortened name keeps its head and ends in a hash of the original, so two
// names that share a long prefix stay apart. The same map applies to the
// tool_use and tool_result blocks already in the conversation: a request whose
// history still carries the long name would fail the same way.
//
// The executor answers with the short name, so callers must map it back before
// the response reaches the client, which does not know the short name.
func shortenToolNames(body []byte, limit int) ([]byte, map[string]string, error) {
	if limit <= 0 {
		return body, nil, nil
	}
	spans, err := jsonbytes.TopLevel(body)
	if err != nil {
		return nil, nil, err
	}
	sp, ok := spans["tools"]
	if !ok {
		return body, nil, nil
	}
	var tools []json.RawMessage
	if err := json.Unmarshal(body[sp.Start:sp.End], &tools); err != nil {
		return nil, nil, err
	}
	short := make(map[string]string)
	for _, tool := range tools {
		var head struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(tool, &head) != nil || len(head.Name) <= limit {
			continue
		}
		short[head.Name] = shortToolName(head.Name, limit)
	}
	if len(short) == 0 {
		return body, nil, nil
	}
	out, err := renameToolsField(body, tools, short)
	if err != nil {
		return nil, nil, err
	}
	if out, err = renameToolBlocks(out, short); err != nil {
		return nil, nil, err
	}
	// The response carries the short name, so the caller needs the way back.
	long := make(map[string]string, len(short))
	for from, to := range short {
		long[to] = from
	}
	return out, long, nil
}

// shortToolName cuts name to limit and ends it with a hash of the whole name,
// so names that share a prefix do not collide.
func shortToolName(name string, limit int) string {
	sum := sha256.Sum256([]byte(name))
	suffix := "_" + hex.EncodeToString(sum[:3])
	if limit <= len(suffix) {
		return hex.EncodeToString(sum[:])[:limit]
	}
	return name[:limit-len(suffix)] + suffix
}

func renameToolsField(body []byte, tools []json.RawMessage, short map[string]string) ([]byte, error) {
	kept := make([][]byte, 0, len(tools))
	for _, tool := range tools {
		var head struct {
			Name string `json:"name"`
		}
		to, ok := "", false
		if json.Unmarshal(tool, &head) == nil {
			to, ok = short[head.Name]
		}
		if !ok {
			kept = append(kept, tool)
			continue
		}
		v, err := json.Marshal(to)
		if err != nil {
			return nil, err
		}
		renamed, err := jsonbytes.SetField(tool, "name", v)
		if err != nil {
			return nil, err
		}
		kept = append(kept, renamed)
	}
	return jsonbytes.SetField(body, "tools", append(append([]byte{'['}, bytes.Join(kept, []byte{','})...), ']'))
}

// renameToolBlocks rewrites the tool_use names already in the conversation.
func renameToolBlocks(body []byte, short map[string]string) ([]byte, error) {
	spans, err := jsonbytes.TopLevel(body)
	if err != nil {
		return nil, err
	}
	sp, ok := spans["messages"]
	if !ok {
		return body, nil
	}
	var msgs []json.RawMessage
	if err := json.Unmarshal(body[sp.Start:sp.End], &msgs); err != nil {
		return nil, err
	}
	changed := false
	for i, m := range msgs {
		blocks, ok := messageBlocks(m)
		if !ok || blocks == nil {
			continue
		}
		touched := false
		for j, b := range blocks {
			var head struct {
				Type string `json:"type"`
				Name string `json:"name"`
			}
			if json.Unmarshal(b, &head) != nil || head.Type != "tool_use" {
				continue
			}
			to, ok := short[head.Name]
			if !ok {
				continue
			}
			v, err := json.Marshal(to)
			if err != nil {
				return nil, err
			}
			renamed, err := jsonbytes.SetField(b, "name", v)
			if err != nil {
				return nil, err
			}
			blocks[j], touched = renamed, true
		}
		if !touched {
			continue
		}
		raw := make([][]byte, len(blocks))
		for j, b := range blocks {
			raw[j] = b
		}
		updated, err := jsonbytes.SetField(m, "content", append(append([]byte{'['}, bytes.Join(raw, []byte{','})...), ']'))
		if err != nil {
			return nil, err
		}
		msgs[i], changed = updated, true
	}
	if !changed {
		return body, nil
	}
	raw := make([][]byte, len(msgs))
	for i, m := range msgs {
		raw[i] = m
	}
	return jsonbytes.SetField(body, "messages", append(append([]byte{'['}, bytes.Join(raw, []byte{','})...), ']'))
}
