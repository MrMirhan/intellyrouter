package guided

import (
	"encoding/json"
	"errors"
	"strings"
)

// DirectorSystemPrompt is the director's instruction. A director that runs
// through Claude Code gets it after Claude Code's own system prompt.
func DirectorSystemPrompt() string { return directorSystem }

// checkpointText is the last message the director reads.
func checkpointText(reason, detail, previous string) string {
	checkpoint := "Checkpoint: " + reason
	if detail != "" {
		checkpoint += " (" + detail + ")"
	}
	if reason == ReasonQuestion {
		checkpoint = "The executor asks you:\n" + detail + "\n\nAnswer the question directly, then add the guidance the executor needs."
	}
	if previous != "" {
		checkpoint += "\n\nYour previous guidance in this turn:\n" + previous
	}
	return checkpoint + "\n\nGive your guidance now."
}

// Transcript is an executor session as text for a director that runs through
// Claude Code. The session only grows, so a director conversation can read
// each block once and get the new blocks at the next checkpoint.
type Transcript struct {
	// Setup is the session's system text: the environment and instructions
	// the executor works with.
	Setup  string
	Blocks []TranscriptBlock
}

type TranscriptBlock struct {
	Role string
	Text string
}

func ReadTranscript(body []byte) (Transcript, error) {
	var req struct {
		System   json.RawMessage `json:"system"`
		Messages []rawMessage    `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return Transcript{}, err
	}
	t := Transcript{Setup: shorten(sessionSystemText(req.System), 20000, 4000)}
	for _, m := range req.Messages {
		role := m.Role
		if role != "assistant" {
			role = "user"
		}
		for _, b := range parseBlocks(m.Content) {
			if text := blockText(b); text != "" {
				t.Blocks = append(t.Blocks, TranscriptBlock{Role: role, Text: text})
			}
		}
	}
	return t, nil
}

// Prompt renders the blocks from index from, then the checkpoint. Only the
// prompt that starts a director conversation holds the session setup.
func (t Transcript) Prompt(from int, reason, detail, previous string) string {
	var b strings.Builder
	if from == 0 {
		if t.Setup != "" {
			b.WriteString("# Executor session setup\n\n" + t.Setup + "\n\n")
		}
		b.WriteString("# Executor session\n")
	} else {
		b.WriteString("# Executor session, continued\n")
	}
	role := ""
	for _, block := range t.Blocks[from:] {
		if block.Role != role {
			role = block.Role
			title := "User and tool results"
			if role == "assistant" {
				title = "Executor"
			}
			b.WriteString("\n## " + title + "\n")
		}
		b.WriteString("\n" + block.Text + "\n")
	}
	b.WriteString("\n# " + checkpointText(reason, detail, previous) + "\n")
	return b.String()
}

func sessionSystemText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var blocks []block
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		// Claude Code sends its billing header as a system block.
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" && !strings.HasPrefix(b.Text, "x-anthropic-billing-header:") {
			parts = append(parts, strings.TrimSpace(b.Text))
		}
	}
	return strings.Join(parts, "\n\n")
}

// GuidanceText reads a director's plain-text answer.
func GuidanceText(text string) (guidance string, approved bool, err error) {
	guidance = strings.TrimSpace(text)
	if guidance == "" {
		return "", false, errors.New("the director returned no text")
	}
	return guidance, strings.HasPrefix(strings.ToUpper(guidance), "APPROVED"), nil
}
