package gateway

import (
	"bytes"
	"encoding/json"
	"regexp"

	"github.com/MrMirhan/intellyrouter/internal/jsonbytes"
)

// toolNamePattern is what the Messages API accepts as a tool name. Tool names
// outside this charset cause a hard 400, and once the corrupted block is in
// the conversation history every later request in the session keeps failing.
var toolNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// dropBrokenToolCalls removes tool_use blocks whose name the API will reject,
// and the tool_result blocks that answer them. A weak executor occasionally
// emits a tool call with a corrupted name; once the client records it the whole
// session dies. Removing the broken call and its result loses one turn of
// context but lets the next request through.
//
// The function is safe on requests where the issue does not occur: changed
// stays false and the bytes are returned untouched.
func dropBrokenToolCalls(body []byte) ([]byte, int, error) {
	spans, err := jsonbytes.TopLevel(body)
	if err != nil {
		return nil, 0, err
	}
	sp, ok := spans["messages"]
	if !ok {
		return body, 0, nil
	}
	var msgs []json.RawMessage
	if err := json.Unmarshal(body[sp.Start:sp.End], &msgs); err != nil {
		return nil, 0, err
	}
	// One walk: collect ids of broken tool_use blocks, and drop the matching
	// tool_results as we reach them. A tool_result always follows the tool_use
	// it answers, so order is enough.
	broken := map[string]bool{}
	for _, m := range msgs {
		blocks, ok := messageBlocks(m)
		if !ok {
			continue
		}
		for _, b := range blocks {
			var head struct {
				Type string `json:"type"`
				Name string `json:"name"`
				ID   string `json:"id"`
			}
			if json.Unmarshal(b, &head) != nil {
				continue
			}
			if head.Type == "tool_use" && head.Name != "" && !toolNamePattern.MatchString(head.Name) {
				broken[head.ID] = true
			}
		}
	}
	if len(broken) == 0 {
		return body, 0, nil
	}

	kept := make([][]byte, 0, len(msgs))
	dropped := 0
	for _, m := range msgs {
		blocks, ok := messageBlocks(m)
		if !ok {
			kept = append(kept, m)
			continue
		}
		if blocks == nil {
			kept = append(kept, m)
			continue
		}
		out := make([][]byte, 0, len(blocks))
		for _, b := range blocks {
			var head struct {
				Type      string `json:"type"`
				ID        string `json:"id"`
				ToolUseID string `json:"tool_use_id"`
			}
			if json.Unmarshal(b, &head) != nil {
				out = append(out, b)
				continue
			}
			if (head.Type == "tool_use" && broken[head.ID]) || (head.Type == "tool_result" && broken[head.ToolUseID]) {
				dropped++
				continue
			}
			out = append(out, b)
		}
		if len(out) == len(blocks) {
			kept = append(kept, m)
			continue
		}
		if len(out) == 0 {
			continue
		}
		rebuilt, err := jsonbytes.SetField(m, "content", append(append([]byte{'['}, bytes.Join(out, []byte{','})...), ']'))
		if err != nil {
			return nil, dropped, err
		}
		kept = append(kept, rebuilt)
	}
	out, err := jsonbytes.SetField(body, "messages", append(append([]byte{'['}, bytes.Join(kept, []byte{','})...), ']'))
	return out, dropped, err
}

// messageBlocks returns a message's content blocks. ok is false when the
// message has no content field, and blocks is nil for plain-text content.
func messageBlocks(m json.RawMessage) ([]json.RawMessage, bool) {
	var msg struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(m, &msg) != nil {
		return nil, false
	}
	var blocks []json.RawMessage
	if json.Unmarshal(msg.Content, &blocks) != nil {
		return nil, false
	}
	return blocks, true
}
