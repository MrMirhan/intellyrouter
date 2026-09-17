package guided

import "testing"

// plan runs one request of a turn: steps so far and failed results so far.
func plan(t *testing.T, s Settings, st *State, steps, failures, tiers int) Decision {
	t.Helper()
	return s.Plan(Turn{Steps: steps, FailedResults: failures}, st, tiers)
}

func deEscalateSettings(after int) Settings {
	s := DefaultSettings()
	s.Director.ModelID = 1
	s.Checkpoints = Checkpoints{FailedResults: 2}
	s.EscalateAfter, s.DeEscalateAfter = 1, after
	return s
}

// A turn that recovers returns to the cheap executor instead of holding the
// expensive one for every remaining step.
func TestTurnDropsBackAfterCleanSteps(t *testing.T) {
	s, st := deEscalateSettings(3), &State{}
	if d := plan(t, s, st, 1, 2, 2); !d.Escalated || d.Tier != 1 {
		t.Fatalf("two failures should escalate: %+v", d)
	}
	for step, wantTier := range map[int]int{2: 1, 3: 1} {
		if d := plan(t, s, st, step, 2, 2); d.Tier != wantTier {
			t.Fatalf("step %d: tier = %d, want %d", step, d.Tier, wantTier)
		}
	}
	d := plan(t, s, st, 4, 2, 2)
	if !d.DeEscalated || d.Tier != 0 {
		t.Fatalf("three clean steps should drop a tier: %+v", d)
	}
}

// A new failure restarts the count, so a turn that keeps failing stays up.
func TestNewFailureRestartsTheCount(t *testing.T) {
	s, st := deEscalateSettings(3), &State{}
	plan(t, s, st, 1, 2, 2)
	plan(t, s, st, 2, 2, 2)
	// Step 3 brings a failure: the two clean steps no longer count.
	if d := plan(t, s, st, 3, 3, 2); d.DeEscalated {
		t.Fatalf("a failing step should not drop a tier: %+v", d)
	}
	for _, step := range []int{4, 5} {
		if d := plan(t, s, st, step, 3, 2); d.DeEscalated {
			t.Fatalf("step %d dropped too early: %+v", step, d)
		}
	}
	if d := plan(t, s, st, 6, 3, 2); !d.DeEscalated || d.Tier != 0 {
		t.Fatalf("three clean steps after the failure should drop: %+v", d)
	}
}

// Zero keeps the old behaviour: the turn holds the tier it reached.
func TestZeroKeepsTheEscalatedTier(t *testing.T) {
	s, st := deEscalateSettings(0), &State{}
	plan(t, s, st, 1, 2, 2)
	for _, step := range []int{2, 3, 4, 5, 6, 7, 8} {
		if d := plan(t, s, st, step, 2, 2); d.Tier != 1 || d.DeEscalated {
			t.Fatalf("step %d: tier = %d, deEscalated = %v", step, d.Tier, d.DeEscalated)
		}
	}
}

// The base tier has nowhere to drop to.
func TestBaseTierStays(t *testing.T) {
	s, st := deEscalateSettings(1), &State{}
	for _, step := range []int{1, 2, 3} {
		if d := plan(t, s, st, step, 0, 2); d.Tier != 0 || d.DeEscalated {
			t.Fatalf("step %d: %+v", step, d)
		}
	}
}

func TestDeEscalateAfterRejectsNegative(t *testing.T) {
	if _, err := ParseSettings(`{"director":{"model_id":1,"effort":"medium","max_calls_per_turn":2},"de_escalate_after":-1}`); err == nil {
		t.Fatal("a negative de_escalate_after should be refused")
	}
}

func TestDefaultSettingsDropBackDown(t *testing.T) {
	if got := DefaultSettings().DeEscalateAfter; got <= 0 {
		t.Fatalf("DeEscalateAfter default = %d, want a turn that can recover", got)
	}
}
