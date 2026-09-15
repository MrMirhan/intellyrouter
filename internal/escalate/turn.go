// Package escalate decides which tier of an escalate route serves a request.
package escalate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// Turn describes the latest human turn of a conversation.
type Turn struct {
	// Prompt is the latest human message with system reminders removed.
	Prompt string
	// Key identifies the turn across the requests of one tool loop.
	Key string
	// PreviousReply is the assistant text before Prompt, truncated.
	PreviousReply string
	// ErrorStreak counts consecutive failed tool results at the end of the conversation.
	ErrorStreak int
}

type message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type block struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	IsError bool   `json:"is_error"`
}

var systemReminder = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`)

// Analyze reads the messages of an Anthropic Messages request body.
func Analyze(body []byte) (Turn, error) {
	var req struct {
		Messages []message `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return Turn{}, err
	}
	var t Turn
	promptAt := -1
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if m := req.Messages[i]; m.Role == "user" {
			if text := humanText(m.Content); text != "" {
				t.Prompt, promptAt = text, i
				break
			}
		}
	}
	// The position keeps two identical prompts ("continue") apart.
	sum := sha256.Sum256([]byte(strconv.Itoa(promptAt) + "\x00" + t.Prompt))
	t.Key = hex.EncodeToString(sum[:8])
	for i := promptAt - 1; i >= 0; i-- {
		if req.Messages[i].Role == "assistant" {
			t.PreviousReply = truncate(assistantText(req.Messages[i].Content), 1500)
			break
		}
	}
	t.ErrorStreak = errorStreak(req.Messages[promptAt+1:])
	return t, nil
}

// humanText returns the text a person typed; tool loops only add tool results
// and system reminders, which yield an empty string.
func humanText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(systemReminder.ReplaceAllString(s, ""))
	}
	var blocks []block
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if b.Type != "text" {
			continue
		}
		if text := strings.TrimSpace(systemReminder.ReplaceAllString(b.Text, "")); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func assistantText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []block
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func errorStreak(msgs []message) int {
	streak := 0
	for _, m := range msgs {
		if m.Role != "user" {
			continue
		}
		var blocks []block
		if json.Unmarshal(m.Content, &blocks) != nil {
			continue
		}
		for _, b := range blocks {
			if b.Type != "tool_result" {
				continue
			}
			if b.IsError {
				streak++
			} else {
				streak = 0
			}
		}
	}
	return streak
}

var markerPattern = regexp.MustCompile(`(?:^|\s)#([a-z0-9][a-z0-9._-]*)`)

// MarkerTier returns the tier picked by the last recognized marker in the
// prompt: #<label> selects that tier, #up the first escalation tier, and #base
// the base tier.
func MarkerTier(prompt string, labels []string) (tier int, marker string, ok bool) {
	for _, m := range markerPattern.FindAllStringSubmatch(strings.ToLower(prompt), -1) {
		for _, name := range []string{m[1], strings.TrimRight(m[1], "._-")} {
			if i, found := markerIndex(name, labels); found {
				tier, marker, ok = i, "#"+name, true
				break
			}
		}
	}
	return tier, marker, ok
}

func markerIndex(name string, labels []string) (int, bool) {
	switch name {
	case "base":
		return 0, true
	case "up":
		return min(1, len(labels)-1), true
	}
	for i, l := range labels {
		if l == name {
			return i, true
		}
	}
	return 0, false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
