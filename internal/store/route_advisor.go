package store

import (
	"encoding/json"
	"strings"
)

// DefaultAdvisorCalls is the number of questions an advisor answers in one
// turn when the route sets no limit.
const DefaultAdvisorCalls = 6

// RouteAdvisor is a route's advisor. With ModelID, the gateway answers the
// executor's questions with that model, on any provider, and Claude Code's own
// advisor tool uses it on steps that Anthropic runs. Off removes Claude Code's
// advisor tool. Without either, requests keep Claude Code's own advisor.
type RouteAdvisor struct {
	ModelID int64  `json:"model_id,omitempty"`
	Off     bool   `json:"off,omitempty"`
	Effort  string `json:"effort,omitempty"`
	// MaxCallsPerTurn limits the questions the advisor answers in one turn.
	MaxCallsPerTurn int `json:"max_calls_per_turn,omitempty"`
}

// CallLimit is the number of questions the advisor answers in one turn.
func (a RouteAdvisor) CallLimit() int {
	if a.MaxCallsPerTurn > 0 {
		return a.MaxCallsPerTurn
	}
	return DefaultAdvisorCalls
}

// ParseRouteAdvisor reads the advisor key of route settings.
func ParseRouteAdvisor(settings string) (RouteAdvisor, error) {
	var s struct {
		Advisor RouteAdvisor `json:"advisor"`
	}
	if strings.TrimSpace(settings) == "" {
		return RouteAdvisor{}, nil
	}
	err := json.Unmarshal([]byte(settings), &s)
	return s.Advisor, err
}
