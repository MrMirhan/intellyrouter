package guided

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/MrMirhan/intellyrouter/internal/jsonbytes"
)

// Roles of the model that answers the executor's hidden questions.
const (
	RoleDirector = "director"
	RoleAdvisor  = "advisor"
)

// ConsultToolName lets an executor ask the director a question, and
// AdvisorToolName the route's advisor. The gateway answers the call, so Claude
// Code never sees it.
const (
	ConsultToolName = "ask_director"
	AdvisorToolName = "ask_advisor"
)

// ToolName is the consult tool of a role.
func ToolName(role string) string {
	if role == RoleAdvisor {
		return AdvisorToolName
	}
	return ConsultToolName
}

var consultUse = map[string]string{
	RoleDirector: "Use it when you are stuck, when you are not sure which approach is correct, or before a large or risky change.",
	RoleAdvisor:  "Use it when you are stuck, when an error keeps coming back, when you are not sure which approach is correct, before a large or risky change, and before you report that the task is done.",
}

func consultTool(role string) []byte {
	name, _ := json.Marshal(ToolName(role))
	description, _ := json.Marshal(fmt.Sprintf("Ask the %s, a senior engineer who can see this whole session, for advice. %s Call it alone, without other tools in the same response. The result is the %s's written answer.", role, consultUse[role], role))
	return fmt.Appendf(nil, `{"name":%s,"description":%s,"input_schema":{"type":"object","properties":{"question":{"type":"string","description":"Your question, with what you tried, what failed, and the options you see."}},"required":["question"]}}`, name, description)
}

// AddConsultTool appends the consult tool of a role to the request's tools.
// Earlier bytes stay unchanged for prompt caching. It reports false when the
// request already has a tool with that name.
func AddConsultTool(body []byte, role string) ([]byte, bool, error) {
	sp, err := arraySpan(body, "tools")
	if err != nil {
		return nil, false, err
	}
	var tools []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body[sp.Start:sp.End], &tools); err != nil {
		return nil, false, err
	}
	for _, t := range tools {
		if t.Name == ToolName(role) {
			return body, false, nil
		}
	}
	return appendItems(body, sp, consultTool(role)), true, nil
}

// RemoveConsultTool removes the consult tool of a role from the request's
// tools, so the executor cannot ask again.
func RemoveConsultTool(body []byte, role string) ([]byte, error) {
	sp, err := arraySpan(body, "tools")
	if err != nil {
		return nil, err
	}
	var tools []json.RawMessage
	if err := json.Unmarshal(body[sp.Start:sp.End], &tools); err != nil {
		return nil, err
	}
	kept := make([][]byte, 0, len(tools))
	for _, tool := range tools {
		var head struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(tool, &head) == nil && head.Name == ToolName(role) {
			continue
		}
		kept = append(kept, tool)
	}
	return jsonbytes.SetField(body, "tools", append(append([]byte{'['}, bytes.Join(kept, []byte{','})...), ']'))
}

// HasTools reports whether the request offers the model at least one tool.
func HasTools(body []byte) bool {
	sp, err := arraySpan(body, "tools")
	if err != nil {
		return false
	}
	var tools []json.RawMessage
	return json.Unmarshal(body[sp.Start:sp.End], &tools) == nil && len(tools) > 0
}

// AppendAssistantText appends an assistant message with one text block.
func AppendAssistantText(body []byte, text string) ([]byte, error) {
	return appendMessages(body, textMessage{Role: "assistant", Content: []textBlock{{Type: "text", Text: text}}})
}

// AppendConsultAnswer lets the executor continue its response after the
// answer of the director or the advisor. The hidden call is not replayed,
// because some upstreams, such as Gemini, reject a replayed function call that
// has no thought signature. text is what the executor wrote before it asked.
func AppendConsultAnswer(body []byte, role, text, question, answer string) ([]byte, error) {
	if strings.TrimSpace(text) == "" {
		text = fmt.Sprintf("I will ask the %s before I continue.", role)
	}
	note := fmt.Sprintf("<%[1]s-answer>\nYou asked the %[1]s: %[2]s\n\n%[3]s\n</%[1]s-answer>\n"+
		"Continue your response from where it stopped. Do not repeat what you already wrote, and do not ask the same question again.",
		role, question, answer)
	return appendMessages(body,
		textMessage{Role: "assistant", Content: []textBlock{{Type: "text", Text: text}}},
		textMessage{Role: "user", Content: []textBlock{{Type: "text", Text: note}}})
}

func appendMessages(body []byte, msgs ...textMessage) ([]byte, error) {
	sp, err := arraySpan(body, "messages")
	if err != nil {
		return nil, err
	}
	items := make([][]byte, 0, len(msgs))
	for _, m := range msgs {
		b, err := json.Marshal(m)
		if err != nil {
			return nil, err
		}
		items = append(items, b)
	}
	return appendItems(body, sp, items...), nil
}

func arraySpan(body []byte, key string) (jsonbytes.Span, error) {
	spans, err := jsonbytes.TopLevel(body)
	if err != nil {
		return jsonbytes.Span{}, err
	}
	sp, ok := spans[key]
	if !ok || body[sp.Start] != '[' {
		return jsonbytes.Span{}, errors.New("request has no " + key + " array")
	}
	return sp, nil
}

// appendItems inserts items at the end of the JSON array at sp.
func appendItems(body []byte, sp jsonbytes.Span, items ...[]byte) []byte {
	closing := sp.End - 1
	nonEmpty := len(bytes.TrimSpace(body[sp.Start+1:closing])) > 0
	var ins []byte
	for _, item := range items {
		if nonEmpty {
			ins = append(ins, ',')
		}
		ins = append(ins, item...)
		nonEmpty = true
	}
	out := make([]byte, 0, len(body)+len(ins))
	out = append(out, body[:closing]...)
	out = append(out, ins...)
	return append(out, body[closing:]...)
}
