package escalate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

type Decision struct {
	Tier   int
	Reason string
}

// Classify asks a small model whether the turn needs a stronger model.
type Classify func(ctx context.Context) (escalate bool, reason string, err error)

// Decider remembers each turn's decision so every request of a tool loop
// lands on the same tier and the classifier runs once per turn.
type Decider struct {
	mu    sync.Mutex
	turns map[string]turnState
	ttl   time.Duration
	now   func() time.Time
}

type turnState struct {
	decision Decision
	// streakMark is the error streak at the last failure-streak raise.
	streakMark int
	expires    time.Time
}

const maxTurns = 10000

func NewDecider(ttl time.Duration) *Decider {
	return &Decider{turns: make(map[string]turnState), ttl: ttl, now: time.Now}
}

// Decide picks the tier for one request of a turn. A marker in the prompt
// wins. Otherwise the turn's earlier decision applies, or the classifier runs
// once. A failure streak can raise the tier for the rest of the turn.
func (d *Decider) Decide(ctx context.Context, key string, turn Turn, labels []string, rules Rules, classify Classify) Decision {
	if tier, marker, ok := MarkerTier(turn.Prompt, labels); ok {
		return Decision{Tier: tier, Reason: "marker " + marker}
	}
	st, ok := d.get(key)
	if !ok {
		st.decision = Decision{Reason: "base"}
		if rules.Classifier.Enabled && classify != nil && turn.Prompt != "" {
			up, why, err := classify(ctx)
			switch {
			case err != nil:
				st.decision.Reason = "classifier failed: " + err.Error()
			case up:
				st.decision = Decision{Tier: raise(rules.Classifier.Target, 0, len(labels)), Reason: "classifier: " + why}
			}
		}
	}
	if turn.ErrorStreak < st.streakMark {
		st.streakMark = 0
	}
	if fs := rules.FailureStreak; fs.Enabled && st.decision.Tier < len(labels)-1 && turn.ErrorStreak-st.streakMark >= fs.Threshold {
		st.decision = Decision{
			Tier:   raise(fs.Target, st.decision.Tier, len(labels)),
			Reason: fmt.Sprintf("%d failed tool calls in a row", turn.ErrorStreak),
		}
		st.streakMark = turn.ErrorStreak
	}
	d.put(key, st)
	return st.decision
}

func (d *Decider) get(key string) (turnState, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	st, ok := d.turns[key]
	if !ok || d.now().After(st.expires) {
		return turnState{}, false
	}
	return st, true
}

func (d *Decider) put(key string, st turnState) {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	if len(d.turns) >= maxTurns {
		for k, v := range d.turns {
			if now.After(v.expires) {
				delete(d.turns, k)
			}
		}
	}
	st.expires = now.Add(d.ttl)
	d.turns[key] = st
}

const classifierSystem = `You route requests in a coding assistant. A cheap model does the work by default; a stronger model takes over when needed.
Decide from the user's latest message whether the stronger model should handle this turn. Answer escalate=true when the user:
- is unhappy with the previous result, rejects it, or says it is wrong or low quality
- asks for a review, audit, or second opinion of code or a plan
- asks for a better, smarter, or stronger model
Otherwise answer escalate=false.
Reply with only a JSON object: {"escalate": true or false, "reason": "at most 10 words"}`

// ClassifierRequest builds a non-streaming Anthropic Messages body for the
// classifier model.
func ClassifierRequest(model string, turn Turn) []byte {
	user := "Latest user message:\n" + truncate(turn.Prompt, 4000)
	if turn.PreviousReply != "" {
		user = "Previous assistant reply (truncated):\n" + turn.PreviousReply + "\n\n" + user
	}
	b, _ := json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": 400,
		"system":     classifierSystem,
		"messages":   []map[string]string{{"role": "user", "content": user}},
	})
	return b
}

// ParseVerdict reads the classifier's JSON answer from an Anthropic message body.
func ParseVerdict(message []byte) (escalate bool, reason string, err error) {
	var m struct {
		Content []block `json:"content"`
	}
	if err := json.Unmarshal(message, &m); err != nil {
		return false, "", err
	}
	var text strings.Builder
	for _, b := range m.Content {
		if b.Type == "text" {
			text.WriteString(b.Text)
		}
	}
	s := text.String()
	start, end := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if start < 0 || end < start {
		return false, "", fmt.Errorf("no JSON object in classifier reply %q", truncate(s, 200))
	}
	var v struct {
		Escalate bool   `json:"escalate"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(s[start:end+1]), &v); err != nil {
		return false, "", fmt.Errorf("classifier reply: %w", err)
	}
	return v.Escalate, v.Reason, nil
}
