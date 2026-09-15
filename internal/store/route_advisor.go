package store

import (
	"encoding/json"
	"strings"
)

// RouteAdvisor is a route's advisor choice for Claude Code's advisor tool.
// ModelID replaces the advisor model that Claude Code asks for, and Off removes
// the tool. Without either, requests keep Claude Code's own advisor.
type RouteAdvisor struct {
	ModelID int64 `json:"model_id,omitempty"`
	Off     bool  `json:"off,omitempty"`
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
