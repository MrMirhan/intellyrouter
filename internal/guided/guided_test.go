package guided

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const session = `{"model":"claude-guided","messages":[
  {"role":"user","content":"Fix the failing tests in calc"},
  {"role":"assistant","content":[{"type":"thinking","thinking":"secret","signature":"s"},{"type":"text","text":"Editing."},{"type":"tool_use","id":"t1","name":"Edit","input":{"file_path":"calc.go"}}]},
  {"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"--- FAIL: TestAdd (0.00s)"}]},
  {"role":"assistant","content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"go test ./..."}}]},
  {"role":"user","content":[{"type":"tool_result","tool_use_id":"t2","is_error":true,"content":[{"type":"text","text":"Exit code 1"}]}]},
  {"role":"assistant","content":[{"type":"text","text":"I'm not sure why this still fails."},{"type":"tool_use","id":"t3","name":"Bash","input":{"command":"go test ./..."}}]},
  {"role":"user","content":[{"type":"tool_result","tool_use_id":"t3","content":"ok  \tevaltask\t0.2s"}]}
]}`

func TestAnalyze(t *testing.T) {
	turn, err := Analyze([]byte(session))
	if err != nil {
		t.Fatal(err)
	}
	if turn.Prompt != "Fix the failing tests in calc" || turn.Steps != 3 || turn.FailedResults != 2 ||
		!turn.EditedFiles || !turn.LastResultsPassed || turn.LastResultsFailed || !turn.Unsure || turn.HasTools {
		t.Fatalf("turn = %+v", turn)
	}
	withTools, err := Analyze([]byte(`{"tools":[{"name":"Bash"}],"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil || !withTools.HasTools {
		t.Fatalf("tools not detected: %+v, %v", withTools, err)
	}
}

func TestPlan(t *testing.T) {
	s, err := ParseSettings(`{"director":{"model_id":7},"checkpoints":{"steps":4},"escalate_after":2}`)
	if err != nil {
		t.Fatal(err)
	}
	st := State{LastCheckpointStep: -1}
	step := func(turn Turn, wantReason string, wantTier int) {
		t.Helper()
		d := s.Plan(turn, &st, 3)
		if d.Reason != wantReason || d.Tier != wantTier {
			t.Fatalf("Plan(%+v) = %+v, want reason %q tier %d (state %+v)", turn, d, wantReason, wantTier, st)
		}
	}
	step(Turn{}, ReasonTurnStart, 0)
	step(Turn{}, "", 0) // same step: no second checkpoint
	step(Turn{Steps: 1}, "", 0)
	step(Turn{Steps: 2, FailedResults: 2}, ReasonFailures, 0)
	step(Turn{Steps: 3, FailedResults: 3}, "", 0)
	step(Turn{Steps: 4, FailedResults: 4}, ReasonFailures, 1) // second failure checkpoint moves the executor up
	step(Turn{Steps: 5, FailedResults: 4, Unsure: true}, ReasonUnsure, 1)
	step(Turn{Steps: 6, FailedResults: 4, EditedFiles: true, LastResultsPassed: true}, ReasonReview, 1)
	step(Turn{Steps: 10, FailedResults: 4}, ReasonSteps, 1)
	if st.DirectorCalls != 6 {
		t.Fatalf("director calls = %d", st.DirectorCalls)
	}

	capped := State{LastCheckpointStep: -1, DirectorCalls: s.Director.MaxCallsPerTurn}
	if d := s.Plan(Turn{}, &capped, 3); d.Reason != "" {
		t.Fatalf("checkpoint fired past the per-turn cap: %+v", d)
	}
	if _, err := ParseSettings(`{}`); err == nil {
		t.Fatal("settings without a director model were accepted")
	}
}

func TestDirectorRequest(t *testing.T) {
	long := strings.Repeat("x", 9000)
	body := strings.Replace(session, `"ok  \tevaltask\t0.2s"`, `"`+long+`"`, 1)
	out, err := DirectorRequest([]byte(body), "claude-fable-5-1", DirectorSettings{Effort: "medium"}, ReasonReview, "", "Check calc.go first.")
	if err != nil {
		t.Fatal(err)
	}
	var req struct {
		Model        string            `json:"model"`
		System       string            `json:"system"`
		Tools        json.RawMessage   `json:"tools"`
		OutputConfig map[string]string `json:"output_config"`
		Messages     []textMessage     `json:"messages"`
	}
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatal(err)
	}
	text, _ := json.Marshal(req.Messages)
	switch {
	case req.Model != "claude-fable-5-1" || req.Tools != nil || req.OutputConfig["effort"] != "medium":
		t.Fatalf("request header fields: %s", out)
	case !strings.HasPrefix(req.System, "You direct a coding agent"):
		t.Fatal("director system prompt missing")
	case strings.Contains(string(text), "secret"):
		t.Fatal("thinking content leaked into the director transcript")
	case !strings.Contains(string(text), `[tool call Edit] {\"file_path\":\"calc.go\"}`) || !strings.Contains(string(text), "[tool error] Exit code 1"):
		t.Fatalf("tool blocks not converted: %s", text)
	case !strings.Contains(string(text), "characters omitted"):
		t.Fatal("long tool output was not shortened")
	}
	for i := 1; i < len(req.Messages); i++ {
		if req.Messages[i].Role == req.Messages[i-1].Role {
			t.Fatalf("consecutive %s messages", req.Messages[i].Role)
		}
	}
	last := req.Messages[len(req.Messages)-1]
	final := last.Content[len(last.Content)-1].Text
	if last.Role != "user" || !strings.Contains(final, "Checkpoint: review after passing tests") || !strings.Contains(final, "Check calc.go first.") {
		t.Fatalf("checkpoint message = %+v", last)
	}
	if last.Content[len(last.Content)-2].CacheControl == nil {
		t.Fatal("transcript prefix is not marked for caching")
	}
}

func TestInjectGuidance(t *testing.T) {
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(session)); err != nil {
		t.Fatal(err)
	}
	body := compact.String()
	out, err := InjectGuidance([]byte(body), "1. Run go test -race.", ReasonUnsure)
	if err != nil {
		t.Fatal(err)
	}
	cut := strings.LastIndex(body, `{"role":"user"`)
	if !strings.HasPrefix(string(out), body[:cut]) {
		t.Fatal("messages before the last one changed")
	}
	var req struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	last := parseBlocks(req.Messages[len(req.Messages)-1].Content)
	if len(req.Messages) != 7 || len(last) != 2 || last[0].Type != "tool_result" ||
		!strings.Contains(last[1].Text, `<director-guidance checkpoint="executor is unsure">`) {
		t.Fatalf("last message = %+v", last)
	}

	plain, err := InjectGuidance([]byte(`{"messages":[{"role":"user","content":"hi"}]}`), "Plan.", ReasonTurnStart)
	if err != nil || !json.Valid(plain) || !strings.Contains(string(plain), `[{"type":"text","text":"hi"},{"type":"text","text":"\u003cdirector-guidance`) {
		t.Fatalf("string content: %s, %v", plain, err)
	}
}

func TestParseGuidanceAndTracker(t *testing.T) {
	g, approved, err := ParseGuidance([]byte(`{"content":[{"type":"thinking","thinking":"x"},{"type":"text","text":"Approved. The fix is complete."}]}`))
	if err != nil || !approved || g != "Approved. The fix is complete." {
		t.Fatalf("got %q %v %v", g, approved, err)
	}
	if _, _, err := ParseGuidance([]byte(`{"content":[]}`)); err == nil {
		t.Fatal("empty guidance accepted")
	}

	tr := NewTracker(time.Hour)
	if st := tr.Get("k"); st.LastCheckpointStep != -1 {
		t.Fatalf("fresh state = %+v", st)
	}
	tr.Put("k", State{Guidance: "g", DirectorCalls: 1})
	if st := tr.Get("k"); st.Guidance != "g" || st.DirectorCalls != 1 {
		t.Fatalf("stored state = %+v", st)
	}
}
