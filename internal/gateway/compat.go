package gateway

import (
	"bytes"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/MrMirhan/intellyrouter/internal/jsonbytes"
)

// Claude Code picks request features from the model name it sends, and a
// route name looks like a current Claude model. An older or third-party model
// behind the route can reject some of those features with a 400. The gateway
// then removes the rejected feature, retries, and remembers the change for
// that model.

const maxAdaptations = 4

// A request cannot work without these fields, so they are never dropped.
var essentialFields = map[string]bool{
	"model": true, "messages": true, "max_tokens": true, "system": true,
	"tools": true, "tool_choice": true, "stream": true,
}

// Claude Code cannot check an advisor pairing for a route name, so a model
// behind the route can reject the advisor that another model accepts.
var advisorPairing = regexp.MustCompile(`'([^']+)' cannot be used as an advisor`)

var extraInputs = regexp.MustCompile(`^([a-z_]+)(?:\.[^:]*)?: Extra inputs are not permitted`)

// unsupportedBlock matches a provider naming a content block type it does not
// know, such as "messages.12.content.1: unsupported content type 'thinking'".
var unsupportedBlock = regexp.MustCompile(`(?i)(?:unsupported|unknown|invalid) content (?:block )?type:? '?"?([a-z][a-z0-9_]*)`)

// adaptationFor maps a 400 error message to the change that avoids it.
func adaptationFor(message string) (string, bool) {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "clear_thinking"):
		return "clear_thinking", true
	case strings.Contains(lower, "effort parameter"):
		return "effort", true
	case strings.Contains(lower, "thinking") && strings.Contains(lower, "not supported"):
		return "thinking", true
	case strings.Contains(lower, "role 'system'") || strings.Contains(lower, `role "system"`):
		return "system_messages", true
	}
	if m := advisorPairing.FindStringSubmatch(message); m != nil {
		return "advisor:" + m[1], true
	}
	if strings.Contains(strings.ToLower(message), "cannot be used as an advisor") {
		return "advisor", true
	}
	if m := unsupportedBlock.FindStringSubmatch(message); m != nil && !essentialBlocks[m[1]] {
		return "block:" + m[1], true
	}
	if m := extraInputs.FindStringSubmatch(message); m != nil && !essentialFields[m[1]] {
		return "field:" + m[1], true
	}
	return "", false
}

// essentialBlocks carry the conversation itself. Dropping one loses what the
// request is asking about, so a provider that rejects it is a provider the
// route should not be using.
var essentialBlocks = map[string]bool{
	"text": true, "image": true, "tool_use": true, "tool_result": true, "document": true,
}

type compat struct {
	mu      sync.Mutex
	byModel map[int64][]string
}

func newCompat() *compat {
	return &compat{byModel: make(map[int64][]string)}
}

// apply makes the changes already learned for the model.
func (c *compat) apply(modelID int64, body []byte) ([]byte, []string) {
	c.mu.Lock()
	known := slices.Clone(c.byModel[modelID])
	c.mu.Unlock()
	var applied []string
	for _, a := range known {
		if out, changed, err := adapt(body, a); err == nil && changed {
			body, applied = out, append(applied, a)
		}
	}
	return body, applied
}

// learn reads a 400 response body and returns the request body without the
// rejected feature. It returns false when the error is not a known feature
// rejection or the request does not use that feature.
func (c *compat) learn(modelID int64, errBody, body []byte) ([]byte, string, bool) {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(errBody, &e) != nil {
		return nil, "", false
	}
	a, ok := adaptationFor(e.Error.Message)
	if !ok {
		return nil, "", false
	}
	out, changed, err := adapt(body, a)
	if err != nil || !changed {
		return nil, "", false
	}
	c.mu.Lock()
	if !slices.Contains(c.byModel[modelID], a) {
		c.byModel[modelID] = append(c.byModel[modelID], a)
	}
	c.mu.Unlock()
	return out, a, true
}

func adapt(body []byte, adaptation string) ([]byte, bool, error) {
	switch {
	case adaptation == "effort":
		return dropEffort(body)
	case adaptation == "thinking":
		out, removed, err := jsonbytes.RemoveField(body, "thinking")
		if err != nil {
			return nil, false, err
		}
		// Thinking-clearing context edits are invalid once thinking is off.
		out, cleared, err := dropClearThinking(out)
		return out, removed || cleared, err
	case adaptation == "clear_thinking":
		return dropClearThinking(body)
	case adaptation == "system_messages":
		return systemMessagesToUser(body)
	case adaptation == "advisor":
		return removeAdvisorTools(body, "")
	case strings.HasPrefix(adaptation, "advisor:"):
		return removeAdvisorTools(body, strings.TrimPrefix(adaptation, "advisor:"))
	case strings.HasPrefix(adaptation, "block:"):
		kind := strings.TrimPrefix(adaptation, "block:")
		return removeContentBlocks(body, func(t string) bool { return t == kind })
	case strings.HasPrefix(adaptation, "field:"):
		return jsonbytes.RemoveField(body, strings.TrimPrefix(adaptation, "field:"))
	}
	return body, false, nil
}

func dropEffort(body []byte) ([]byte, bool, error) {
	spans, err := jsonbytes.TopLevel(body)
	if err != nil {
		return nil, false, err
	}
	sp, ok := spans["output_config"]
	if !ok {
		return body, false, nil
	}
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(body[sp.Start:sp.End], &cfg); err != nil {
		return nil, false, err
	}
	if _, ok := cfg["effort"]; !ok {
		return body, false, nil
	}
	delete(cfg, "effort")
	if len(cfg) == 0 {
		return jsonbytes.RemoveField(body, "output_config")
	}
	v, err := json.Marshal(cfg)
	if err != nil {
		return nil, false, err
	}
	out, err := jsonbytes.SetField(body, "output_config", v)
	return out, err == nil, err
}

// dropClearThinking removes clear_thinking_* edits from context_management,
// and the whole field when no edit remains.
func dropClearThinking(body []byte) ([]byte, bool, error) {
	spans, err := jsonbytes.TopLevel(body)
	if err != nil {
		return nil, false, err
	}
	sp, ok := spans["context_management"]
	if !ok {
		return body, false, nil
	}
	var cm map[string]json.RawMessage
	if err := json.Unmarshal(body[sp.Start:sp.End], &cm); err != nil {
		return nil, false, err
	}
	var edits []json.RawMessage
	if raw, ok := cm["edits"]; !ok || json.Unmarshal(raw, &edits) != nil {
		return body, false, nil
	}
	kept := make([]json.RawMessage, 0, len(edits))
	for _, e := range edits {
		var head struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(e, &head) == nil && strings.HasPrefix(head.Type, "clear_thinking") {
			continue
		}
		kept = append(kept, e)
	}
	if len(kept) == len(edits) {
		return body, false, nil
	}
	if len(kept) == 0 {
		return jsonbytes.RemoveField(body, "context_management")
	}
	if cm["edits"], err = json.Marshal(kept); err != nil {
		return nil, false, err
	}
	v, err := json.Marshal(cm)
	if err != nil {
		return nil, false, err
	}
	out, err := jsonbytes.SetField(body, "context_management", v)
	return out, err == nil, err
}

// systemMessagesToUser turns mid-conversation system messages into user
// messages. The API merges consecutive user turns.
func systemMessagesToUser(body []byte) ([]byte, bool, error) {
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
	changed := false
	for i, m := range msgs {
		var head struct {
			Role string `json:"role"`
		}
		if json.Unmarshal(m, &head) != nil || head.Role != "system" {
			continue
		}
		out, err := jsonbytes.SetField(m, "role", []byte(`"user"`))
		if err != nil {
			return nil, false, err
		}
		msgs[i], changed = out, true
	}
	if !changed {
		return body, false, nil
	}
	var arr bytes.Buffer
	arr.WriteByte('[')
	for i, m := range msgs {
		if i > 0 {
			arr.WriteByte(',')
		}
		arr.Write(m)
	}
	arr.WriteByte(']')
	out, err := jsonbytes.SetField(body, "messages", arr.Bytes())
	return out, err == nil, err
}
