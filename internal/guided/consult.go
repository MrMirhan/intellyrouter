package guided

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"intellyrouter/internal/jsonbytes"
)

// ConsultToolName is the tool that lets an executor ask the director a
// question. The gateway answers the call, so Claude Code never sees it.
const ConsultToolName = "ask_director"

const consultTool = `{"name":"ask_director","description":"Ask the director, a senior engineer who can see this whole session, for advice. Use it when you are stuck, when you are not sure which approach is correct, or before a large or risky change. Call it alone, without other tools in the same response. The result is the director's written answer.","input_schema":{"type":"object","properties":{"question":{"type":"string","description":"Your question, with what you tried, what failed, and the options you see."}},"required":["question"]}}`

// AddConsultTool appends the ask_director tool to the request's tools. Earlier
// bytes stay unchanged for prompt caching. It reports false when the request
// already has a tool with that name.
func AddConsultTool(body []byte) ([]byte, bool, error) {
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
		if t.Name == ConsultToolName {
			return body, false, nil
		}
	}
	return appendItems(body, sp, []byte(consultTool)), true, nil
}

// AppendAssistantText appends an assistant message with one text block.
func AppendAssistantText(body []byte, text string) ([]byte, error) {
	return appendMessages(body, textMessage{Role: "assistant", Content: []textBlock{{Type: "text", Text: text}}})
}

// AppendConsultAnswer lets the executor continue its response after the
// director's answer. The hidden ask_director call is not replayed, because
// some upstreams, such as Gemini, reject a replayed function call that has no
// thought signature. text is what the executor wrote before it asked.
func AppendConsultAnswer(body []byte, text, question, answer string) ([]byte, error) {
	if strings.TrimSpace(text) == "" {
		text = "I will ask the director before I continue."
	}
	note := fmt.Sprintf("<director-answer>\nYou asked the director: %s\n\n%s\n</director-answer>\n"+
		"Continue your response from where it stopped. Do not repeat what you already wrote, and do not ask the same question again.",
		question, answer)
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
