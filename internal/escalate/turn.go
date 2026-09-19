// Package escalate decides which tier of an escalate route serves a request.
package escalate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"slices"
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
	// PromptIndex is the position of the prompt in messages, or -1 without one.
	PromptIndex int
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
	// The whole prefix up to the prompt, not just its text, keeps two turns
	// apart: a short generic prompt ("devam et", "ok") repeats often, and after
	// /clear or a compaction the position resets too, so text and position
	// alone can collide with an unrelated earlier turn and hand it that turn's
	// director guidance or escalation state.
	prefix, err := json.Marshal(req.Messages[:promptAt+1])
	if err != nil {
		prefix = []byte(strconv.Itoa(promptAt) + "\x00" + t.Prompt)
	}
	sum := sha256.Sum256(prefix)
	t.Key = hex.EncodeToString(sum[:8])
	for i := promptAt - 1; i >= 0; i-- {
		if req.Messages[i].Role == "assistant" {
			t.PreviousReply = truncate(assistantText(req.Messages[i].Content), 1500)
			break
		}
	}
	t.ErrorStreak = errorStreak(req.Messages[promptAt+1:])
	t.PromptIndex = promptAt
	return t, nil
}

// Claude Code adds these user texts itself; a loaded skill, local command
// output, hook feedback or an interruption does not start a new turn.
var injectedPrefixes = []string{
	"Base directory for this skill:",
	"<local-command-caveat>",
	"<local-command-stdout>",
	"<local-command-stderr>",
	"Stop hook feedback:",
	"[Request interrupted by user",
}

// humanText returns the text a person typed; tool loops only add tool results,
// system reminders and injected texts, which yield an empty string.
func humanText(raw json.RawMessage) string {
	var texts []string
	var s string
	if json.Unmarshal(raw, &s) == nil {
		texts = []string{s}
	} else {
		var blocks []block
		if json.Unmarshal(raw, &blocks) != nil {
			return ""
		}
		for _, b := range blocks {
			if b.Type == "text" {
				texts = append(texts, b.Text)
			}
		}
	}
	localOutput := false
	for i, text := range texts {
		texts[i] = strings.TrimSpace(systemReminder.ReplaceAllString(text, ""))
		localOutput = localOutput || strings.HasPrefix(texts[i], "<local-command-stdout>") || strings.HasPrefix(texts[i], "<local-command-stderr>")
	}
	var parts []string
	for _, text := range texts {
		injected := slices.ContainsFunc(injectedPrefixes, func(p string) bool { return strings.HasPrefix(text, p) })
		// A built-in command such as /model echoes its name next to its output;
		// a custom slash command has no local output and is a real prompt.
		if text == "" || injected || (localOutput && strings.HasPrefix(text, "<command-name>")) {
			continue
		}
		parts = append(parts, text)
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
