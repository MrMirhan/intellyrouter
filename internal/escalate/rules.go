package escalate

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	TargetNext = "next"
	TargetTop  = "top"
)

// Rules are the escalation settings stored in an escalate route's settings JSON.
type Rules struct {
	Classifier    ClassifierRule    `json:"classifier"`
	FailureStreak FailureStreakRule `json:"failure_streak"`
}

// ClassifierRule runs a small model once per turn to spot dissatisfaction,
// review requests, or requests for a stronger model.
type ClassifierRule struct {
	Enabled bool   `json:"enabled"`
	ModelID int64  `json:"model_id"`
	Target  string `json:"target"`
}

// FailureStreakRule raises the tier after Threshold consecutive failed tool calls.
type FailureStreakRule struct {
	Enabled   bool   `json:"enabled"`
	Threshold int    `json:"threshold"`
	Target    string `json:"target"`
}

func DefaultRules() Rules {
	return Rules{
		Classifier:    ClassifierRule{Target: TargetTop},
		FailureStreak: FailureStreakRule{Enabled: true, Threshold: 3, Target: TargetNext},
	}
}

// ParseRules reads route settings; fields that are not set keep their defaults.
func ParseRules(settings string) (Rules, error) {
	r := DefaultRules()
	if strings.TrimSpace(settings) == "" {
		return r, nil
	}
	if err := json.Unmarshal([]byte(settings), &r); err != nil {
		return Rules{}, fmt.Errorf("route settings: %w", err)
	}
	return r, r.validate()
}

func (r Rules) validate() error {
	for name, target := range map[string]string{"classifier": r.Classifier.Target, "failure_streak": r.FailureStreak.Target} {
		if target != TargetNext && target != TargetTop {
			return fmt.Errorf("%s.target must be %q or %q", name, TargetNext, TargetTop)
		}
	}
	if r.Classifier.Enabled && r.Classifier.ModelID == 0 {
		return errors.New("classifier.model_id is required when the classifier is enabled")
	}
	if r.FailureStreak.Enabled && r.FailureStreak.Threshold < 1 {
		return errors.New("failure_streak.threshold must be at least 1")
	}
	return nil
}

func raise(target string, current, tiers int) int {
	if target == TargetTop {
		return tiers - 1
	}
	return min(current+1, tiers-1)
}
