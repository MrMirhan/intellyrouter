package escalate

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const conversation = `{"model":"intelly-claude-auto","messages":[
  {"role":"user","content":"Add a login page"},
  {"role":"assistant","content":[{"type":"text","text":"Done, I added login.tsx."}]},
  {"role":"user","content":[{"type":"text","text":"<system-reminder>todo list is empty</system-reminder>"},{"type":"text","text":"Bunu beğenmedim, #Opus ile tekrar yap"}]},
  {"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{}}]},
  {"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","is_error":true,"content":"exit 1"},{"type":"text","text":"<system-reminder>x</system-reminder>"}]},
  {"role":"assistant","content":[{"type":"tool_use","id":"t2","name":"Bash","input":{}},{"type":"tool_use","id":"t3","name":"Bash","input":{}}]},
  {"role":"user","content":[{"type":"tool_result","tool_use_id":"t2","is_error":true,"content":"exit 1"},{"type":"tool_result","tool_use_id":"t3","is_error":true,"content":"exit 2"}]}
]}`

func TestAnalyzeFindsHumanPromptAndErrorStreak(t *testing.T) {
	turn, err := Analyze([]byte(conversation))
	if err != nil {
		t.Fatal(err)
	}
	if turn.Prompt != "Bunu beğenmedim, #Opus ile tekrar yap" || turn.PreviousReply != "Done, I added login.tsx." || turn.ErrorStreak != 3 {
		t.Fatalf("turn = %+v", turn)
	}

	// More tool loop requests in the same turn keep the key.
	later := strings.TrimSuffix(conversation, "\n]}") +
		`,{"role":"assistant","content":[{"type":"tool_use","id":"t4","name":"Read","input":{}}]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"t4","content":"ok"}]}]}`
	next, err := Analyze([]byte(later))
	if err != nil {
		t.Fatal(err)
	}
	if next.Key != turn.Key || next.ErrorStreak != 0 {
		t.Fatalf("same turn: key %s vs %s, streak %d", next.Key, turn.Key, next.ErrorStreak)
	}

	// The same words typed again later are a new turn.
	repeat := strings.TrimSuffix(later, "]}") + `,{"role":"user","content":"Bunu beğenmedim, #Opus ile tekrar yap"}]}`
	third, _ := Analyze([]byte(repeat))
	if third.Key == turn.Key {
		t.Fatal("a repeated prompt later in the conversation reused the old turn key")
	}
}

func TestMarkerTier(t *testing.T) {
	labels := []string{"flash", "pro", "opus"}
	cases := []struct {
		prompt string
		tier   int
		ok     bool
	}{
		{"fix it #opus", 2, true},
		{"#PRO please", 1, true},
		{"try #up", 1, true},
		{"#opus then #base", 0, true},
		{"see issue #123 and # heading", 0, false},
		{"email me at a#opus", 0, false},
		{"use #opus.", 2, true},
	}
	for _, c := range cases {
		tier, _, ok := MarkerTier(c.prompt, labels)
		if tier != c.tier || ok != c.ok {
			t.Errorf("MarkerTier(%q) = %d, %v; want %d, %v", c.prompt, tier, ok, c.tier, c.ok)
		}
	}
}

func TestParseRules(t *testing.T) {
	r, err := ParseRules(`{"classifier":{"enabled":true,"model_id":7}}`)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Classifier.Enabled || r.Classifier.ModelID != 7 || r.Classifier.Target != TargetTop || !r.FailureStreak.Enabled || r.FailureStreak.Threshold != 3 {
		t.Fatalf("rules = %+v", r)
	}
	for _, bad := range []string{
		`{"classifier":{"enabled":true}}`,
		`{"failure_streak":{"target":"sideways"}}`,
		`{"failure_streak":{"enabled":true,"threshold":0}}`,
		`[1]`,
	} {
		if _, err := ParseRules(bad); err == nil {
			t.Errorf("ParseRules(%s) accepted invalid settings", bad)
		}
	}
}

func TestDecide(t *testing.T) {
	labels := []string{"flash", "pro", "opus"}
	rules, _ := ParseRules(`{"classifier":{"enabled":true,"model_id":1,"target":"top"},"failure_streak":{"threshold":2,"target":"next"}}`)
	d := NewDecider(time.Hour)
	ctx := context.Background()
	calls := 0
	classify := func(verdict bool, err error) Classify {
		return func(context.Context) (bool, string, error) {
			calls++
			return verdict, "user asked for a review", err
		}
	}

	if got := d.Decide(ctx, "k0", Turn{Prompt: "go #pro"}, labels, rules, classify(true, nil)); got.Tier != 1 || calls != 0 {
		t.Fatalf("marker: %+v, classifier calls %d", got, calls)
	}

	first := d.Decide(ctx, "k1", Turn{Prompt: "review this"}, labels, rules, classify(true, nil))
	again := d.Decide(ctx, "k1", Turn{Prompt: "review this"}, labels, rules, classify(false, nil))
	if first.Tier != 2 || again != first || calls != 1 {
		t.Fatalf("classifier: first %+v again %+v calls %d", first, again, calls)
	}

	if got := d.Decide(ctx, "k2", Turn{Prompt: "hi"}, labels, rules, classify(false, errors.New("timeout"))); got.Tier != 0 || !strings.Contains(got.Reason, "timeout") {
		t.Fatalf("classifier error: %+v", got)
	}

	base := classify(false, nil)
	steps := []struct{ streak, tier int }{{0, 0}, {1, 0}, {2, 1}, {3, 1}, {4, 2}, {0, 2}}
	for _, s := range steps {
		if got := d.Decide(ctx, "k3", Turn{Prompt: "build it", ErrorStreak: s.streak}, labels, rules, base); got.Tier != s.tier {
			t.Fatalf("streak %d: tier %d, want %d (%s)", s.streak, got.Tier, s.tier, got.Reason)
		}
	}
}

func TestParseVerdict(t *testing.T) {
	up, reason, err := ParseVerdict([]byte(`{"content":[{"type":"thinking","thinking":"..."},{"type":"text","text":"` + "```json\\n{\\\"escalate\\\": true, \\\"reason\\\": \\\"asks for review\\\"}\\n```" + `"}]}`))
	if err != nil || !up || reason != "asks for review" {
		t.Fatalf("got %v %q %v", up, reason, err)
	}
	if _, _, err := ParseVerdict([]byte(`{"content":[{"type":"text","text":"maybe"}]}`)); err == nil {
		t.Fatal("accepted a reply without JSON")
	}
}
