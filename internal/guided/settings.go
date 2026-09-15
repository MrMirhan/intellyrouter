// Package guided implements the guided strategy: a director model steers
// cheaper executor models at checkpoints in each turn, and the executors do
// the work.
package guided

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

type Settings struct {
	Director    DirectorSettings `json:"director"`
	Checkpoints Checkpoints      `json:"checkpoints"`
	// EscalateAfter is the number of failure checkpoints in one turn after
	// which the executor moves up one tier. Zero keeps the base tier.
	EscalateAfter int `json:"escalate_after"`
	// Consult gives the executor the ask_director tool, which the gateway answers.
	Consult bool `json:"consult"`
}

type DirectorSettings struct {
	ModelID         int64  `json:"model_id"`
	Effort          string `json:"effort"`
	MaxCallsPerTurn int    `json:"max_calls_per_turn"`
}

// Checkpoints select the moments when the director looks at the session.
type Checkpoints struct {
	TurnStart bool `json:"turn_start"`
	// FailedResults is the number of new failed tool results that triggers a check.
	FailedResults int `json:"failed_results"`
	// Steps is the number of executor steps without a check that triggers one.
	Steps           int  `json:"steps"`
	Unsure          bool `json:"unsure"`
	ReviewOnSuccess bool `json:"review_on_success"`
}

const (
	ReasonTurnStart = "turn start"
	ReasonFailures  = "failed tool results"
	ReasonUnsure    = "executor is unsure"
	ReasonReview    = "review after passing tests"
	ReasonSteps     = "step budget"
	ReasonQuestion  = "executor question"
)

var efforts = []string{"", "low", "medium", "high", "xhigh", "max"}

func DefaultSettings() Settings {
	return Settings{
		Director:      DirectorSettings{Effort: "medium", MaxCallsPerTurn: 6},
		Checkpoints:   Checkpoints{TurnStart: true, FailedResults: 2, Steps: 15, Unsure: true, ReviewOnSuccess: true},
		Consult:       true,
		EscalateAfter: 2,
	}
}

// ParseSettings reads route settings; fields that are not set keep their defaults.
func ParseSettings(raw string) (Settings, error) {
	s := DefaultSettings()
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			return Settings{}, fmt.Errorf("route settings: %w", err)
		}
	}
	switch {
	case s.Director.ModelID == 0:
		return Settings{}, errors.New("director.model_id is required")
	case !slices.Contains(efforts, s.Director.Effort):
		return Settings{}, fmt.Errorf("director.effort must be one of low, medium, high, xhigh, max, or empty")
	case s.Director.MaxCallsPerTurn < 1:
		return Settings{}, errors.New("director.max_calls_per_turn must be at least 1")
	case s.Checkpoints.FailedResults < 0 || s.Checkpoints.Steps < 0 || s.EscalateAfter < 0:
		return Settings{}, errors.New("checkpoint counts and escalate_after cannot be negative")
	}
	return s, nil
}

// Decision says what to do before one request of a turn.
type Decision struct {
	// Reason is the checkpoint that needs the director, or empty.
	Reason    string
	Detail    string
	Tier      int
	Escalated bool
}

// Plan updates the turn state for one request and decides whether the
// director looks at the session first. Only one checkpoint fires per step.
func (s Settings) Plan(t Turn, st *State, tiers int) Decision {
	var d Decision
	if st.DirectorCalls < s.Director.MaxCallsPerTurn && t.Steps != st.LastCheckpointStep {
		c := s.Checkpoints
		sinceCheck := t.Steps - max(st.LastCheckpointStep, 0)
		newFailures := t.FailedResults - st.LastFailedResults
		switch {
		case c.TurnStart && t.Steps == 0 && st.DirectorCalls == 0:
			d.Reason = ReasonTurnStart
		case c.FailedResults > 0 && newFailures >= c.FailedResults:
			d.Reason, d.Detail = ReasonFailures, fmt.Sprintf("%d new failed tool results", newFailures)
			st.LastFailedResults = t.FailedResults
			st.FailureCheckpoints++
			if s.EscalateAfter > 0 && st.FailureCheckpoints >= s.EscalateAfter && st.Tier < tiers-1 {
				st.Tier++
				st.FailureCheckpoints = 0
				d.Escalated = true
			}
		case c.Unsure && t.Unsure:
			d.Reason = ReasonUnsure
		case c.ReviewOnSuccess && !st.Reviewed && t.EditedFiles && t.LastResultsPassed:
			d.Reason = ReasonReview
		case c.Steps > 0 && sinceCheck >= c.Steps:
			d.Reason, d.Detail = ReasonSteps, fmt.Sprintf("%d steps since the last check", sinceCheck)
		}
		if d.Reason != "" {
			st.DirectorCalls++
			st.LastCheckpointStep = t.Steps
		}
	}
	d.Tier = st.Tier
	return d
}
