package guided

import (
	"bytes"
	"encoding/json"
	"regexp"
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
		!turn.EditedFiles || !turn.LastResultsPassed || turn.LastResultsFailed || !turn.Unsure || turn.HasTools ||
		turn.RepeatCount != 1 || turn.RepeatDetail != "" {
		t.Fatalf("turn = %+v", turn)
	}
	withTools, err := Analyze([]byte(`{"tools":[{"name":"Bash"}],"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil || !withTools.HasTools {
		t.Fatalf("tools not detected: %+v, %v", withTools, err)
	}
}

func TestPlan(t *testing.T) {
	s, err := ParseSettings(`{"director":{"model_id":7,"max_calls_per_turn":8},"checkpoints":{"steps":4},"escalate_after":2}`)
	if err != nil {
		t.Fatal(err)
	}
	st := State{LastCheckpointStep: -1}
	step := func(turn Turn, wantReason string, wantTier int) Decision {
		t.Helper()
		d := s.Plan(turn, &st, 3)
		if d.Reason != wantReason || d.Tier != wantTier {
			t.Fatalf("Plan(%+v) = %+v, want reason %q tier %d (state %+v)", turn, d, wantReason, wantTier, st)
		}
		return d
	}
	const loop = `Bash "go test ./..." ran 3 times with the same result`
	step(Turn{}, ReasonTurnStart, 0)
	step(Turn{}, "", 0) // same step: no second checkpoint
	step(Turn{Steps: 1}, "", 0)
	step(Turn{Steps: 2, FailedResults: 2, RepeatCount: 3}, ReasonFailures, 0)
	if d := step(Turn{Steps: 3, FailedResults: 3, RepeatCount: 3, RepeatDetail: loop, Unsure: true}, ReasonRepeat, 0); d.Detail != loop {
		t.Fatalf("repeat detail = %q", d.Detail)
	}
	step(Turn{Steps: 4, FailedResults: 4}, ReasonFailures, 1) // second failure checkpoint moves the executor up
	step(Turn{Steps: 5, FailedResults: 4, RepeatCount: 5, Unsure: true}, ReasonUnsure, 1)
	step(Turn{Steps: 6, FailedResults: 4, EditedFiles: true, LastResultsPassed: true}, ReasonReview, 1)
	step(Turn{Steps: 7, FailedResults: 4, RepeatCount: 6}, ReasonRepeat, 1)
	step(Turn{Steps: 11, FailedResults: 4, RepeatCount: 6}, ReasonSteps, 1)
	if st.DirectorCalls != 8 || st.LastRepeatCount != 6 {
		t.Fatalf("state = %+v", st)
	}

	capped := State{LastCheckpointStep: -1, DirectorCalls: s.Director.MaxCallsPerTurn}
	if d := s.Plan(Turn{}, &capped, 3); d.Reason != "" {
		t.Fatalf("checkpoint fired past the per-turn cap: %+v", d)
	}
	if _, err := ParseSettings(`{}`); err == nil {
		t.Fatal("settings without a director model were accepted")
	}

	defaults, err := ParseSettings(`{"director":{"model_id":7}}`)
	if err != nil || defaults.Checkpoints.Steps != 0 || defaults.Checkpoints.Repeats != 3 {
		t.Fatalf("default checkpoints = %+v, %v", defaults.Checkpoints, err)
	}
	if _, err := ParseSettings(`{"director":{"model_id":7},"checkpoints":{"repeats":-1}}`); err == nil {
		t.Fatal("negative repeats accepted")
	}
	off, _ := ParseSettings(`{"director":{"model_id":7},"checkpoints":{"repeats":0}}`)
	if d := off.Plan(Turn{Steps: 3, RepeatCount: 9}, &State{LastCheckpointStep: -1, DirectorCalls: 1}, 3); d.Reason != "" {
		t.Fatalf("repeat checkpoint fired while off: %+v", d)
	}
}

func TestVerifyCommand(t *testing.T) {
	verifications := []string{
		"go test ./...", "go vet ./...", "golangci-lint run", "$(go env GOPATH)/bin/golangci-lint run ./...",
		"pytest -q", "python -m pytest tests", "tox -e py312", "nox -s lint",
		"npm test", "npm run lint", "pnpm typecheck", "yarn run check", "bun test", "npm run test:unit",
		"npx vitest run", "jest --ci", "cargo test", "cargo clippy -- -D warnings", "make verify",
		"mvn test", "./gradlew check", "gradle test", "dotnet test", "vendor/bin/phpunit", "bundle exec rspec",
		"mix test", "tsc --noEmit", "eslint .", "ruff check .", "mypy src",
		"./extranet-dev test unit", "bin/check lint", "./scripts/ci verify",
		"cd services/api && go test -race ./... 2>&1 | tail -20", "CGO_ENABLED=1 timeout 300 go test ./...",
	}
	for _, c := range verifications {
		if !verifyCommand.MatchString(c) {
			t.Errorf("%q is not recognized as a verification", c)
		}
	}
	for _, c := range []string{"git status", "ls", "cat test.go", "grep test", "grep -rn pytest .", "cat ./scripts/ci test", "go build ./...", "npm install"} {
		if verifyCommand.MatchString(c) {
			t.Errorf("%q is recognized as a verification", c)
		}
	}
}

func TestPatterns(t *testing.T) {
	cases := []struct {
		pattern *regexp.Regexp
		text    string
		want    bool
	}{
		{passPattern, "✓ test completed (20.76s)", true},
		{passPattern, "✔ build ok", true},
		{passPattern, "Tests: 12 tests passed, 12 total", true},
		{passPattern, "===== 3 passed, 1 skipped in 0.12s =====", true},
		{passPattern, "All checks passed!", true},
		{passPattern, "ok  \tcalc\t0.2s", true},
		{passPattern, "Running 42 unit tests\nDone in 20.76s", false},
		{unsurePattern, "I'm unsure which config the loader reads.", true},
		{unsurePattern, "I'm not sure whether the cache is stale.", true},
		{unsurePattern, "Not sure how the fixture gets created.", true},
		{unsurePattern, "I don't know why the handler returns 500.", true},
		{unsurePattern, "I am going in circles here.", true},
		{unsurePattern, "The test still fails after the change.", true},
		{unsurePattern, "The tests pass now; the change is done.", false},
	}
	for _, c := range cases {
		if got := c.pattern.MatchString(c.text); got != c.want {
			t.Errorf("%s matches %q = %v, want %v", c.pattern, c.text, got, c.want)
		}
	}
}

func TestAnalyzeLastResults(t *testing.T) {
	loop := func(results string) string {
		return `{"tools":[{"name":"Bash"}],"messages":[{"role":"user","content":"Fix the unit tests"},` +
			`{"role":"assistant","content":[{"type":"tool_use","id":"e1","name":"Edit","input":{"file_path":"calc.go"}},` +
			`{"type":"tool_use","id":"b1","name":"Bash","input":{"command":"./extranet-dev test unit"}},{"type":"tool_use","id":"b2","name":"Bash","input":{"command":"cat calc.go"}}]},` +
			`{"role":"user","content":[{"type":"tool_result","tool_use_id":"e1","content":"The file calc.go has been updated."},` + results + `]}]}`
	}
	cases := []struct {
		name, results  string
		passed, failed bool
	}{
		{"wrapper without a pass phrase", `{"type":"tool_result","tool_use_id":"b1","content":"Running 42 unit tests\nDone in 20.76s"}`, true, false},
		{"wrapper summary line", `{"type":"tool_result","tool_use_id":"b1","content":[{"type":"text","text":"✓ test completed (20.76s)"}]}`, true, false},
		{"wrapper exit error", `{"type":"tool_result","tool_use_id":"b1","is_error":true,"content":"Exit code 1\nRunning 42 unit tests"}`, false, true},
		{"failure text without is_error", `{"type":"tool_result","tool_use_id":"b1","content":"--- FAIL: TestAdd (0.00s)\nFAIL"}`, false, true},
		{"plain command", `{"type":"tool_result","tool_use_id":"b2","content":"package calc"}`, false, false},
		{"one failure spoils a pass", `{"type":"tool_result","tool_use_id":"b1","content":"Done in 20.76s"},{"type":"tool_result","tool_use_id":"b2","is_error":true,"content":"cat: calc.go: No such file or directory"}`, false, true},
	}
	for _, c := range cases {
		turn, err := Analyze([]byte(loop(c.results)))
		if err != nil {
			t.Fatal(err)
		}
		if turn.LastResultsPassed != c.passed || turn.LastResultsFailed != c.failed {
			t.Errorf("%s: passed %v failed %v, want %v %v", c.name, turn.LastResultsPassed, turn.LastResultsFailed, c.passed, c.failed)
		}
	}
}

func TestAnalyzeRepeats(t *testing.T) {
	call := func(id, name, input, output string) string {
		return `,{"role":"assistant","content":[{"type":"tool_use","id":"` + id + `","name":"` + name + `","input":` + input + `}]},` +
			`{"role":"user","content":[{"type":"tool_result","tool_use_id":"` + id + `","content":"` + output + `"}]}`
	}
	failed := func(duration string) string { return `--- FAIL: TestAdd (0.00s)\nFAIL\tcalc\t` + duration }
	edit := call("e1", "Edit", `{"file_path":"calc.go"}`, "The file calc.go has been updated.")
	session := func(calls string) string {
		return `{"tools":[{"name":"Bash"}],"messages":[{"role":"user","content":"Fix calc"}` + calls + `]}`
	}

	stuck, err := Analyze([]byte(session(call("b1", "Bash", `{"command": "go test ./..."}`, failed("0.215s")) + edit +
		call("b2", "Bash", `{"command":"go test ./..."}`, failed("0.301s")) + call("b3", "Bash", `{"command":"go test ./..."}`, failed("0.198s")))))
	if err != nil {
		t.Fatal(err)
	}
	if stuck.RepeatCount != 3 || stuck.RepeatDetail != `Bash "go test ./..." ran 3 times with the same result` {
		t.Fatalf("stuck: count %d detail %q", stuck.RepeatCount, stuck.RepeatDetail)
	}

	fixed, err := Analyze([]byte(session(call("b1", "Bash", `{"command":"go test ./..."}`, failed("0.215s")) + edit +
		call("b2", "Bash", `{"command":"go test ./..."}`, `ok  \tcalc\t0.2s`))))
	if err != nil {
		t.Fatal(err)
	}
	if fixed.RepeatCount != 1 || fixed.RepeatDetail != "" {
		t.Fatalf("fixed: count %d detail %q", fixed.RepeatCount, fixed.RepeatDetail)
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

func TestStripEchoedBlocks(t *testing.T) {
	// An executor that copies the block into its reply leaves it in the
	// history, where it teaches the executor to copy it again.
	echoed := `{"messages":[` +
		`{"role":"user","content":"fix the bug"},` +
		`{"role":"assistant","content":[{"type":"text","text":"<director-guidance checkpoint=\"turn start\">\n1. Read the file.\n</director-guidance>\nHere is the fix."}]},` +
		`{"role":"user","content":"go on"}]}`
	out, cut, err := StripEchoedBlocks([]byte(echoed))
	if err != nil || !cut {
		t.Fatalf("StripEchoedBlocks = %v, %v", cut, err)
	}
	if strings.Contains(string(out), "director-guidance") || !strings.Contains(string(out), "Here is the fix.") {
		t.Fatalf("block still there: %s", out)
	}

	// A copy that lost its closing marker runs to the end of the block.
	truncated := `{"messages":[{"role":"assistant","content":[{"type":"text","text":"Done.\n<director-guidance checkpoint=\"repeated action\">\nA senior director reviewed"}]}]}`
	out, cut, err = StripEchoedBlocks([]byte(truncated))
	if err != nil || !cut || strings.Contains(string(out), "director-guidance") || !strings.Contains(string(out), "Done.") {
		t.Fatalf("truncated block: %s, %v, %v", out, cut, err)
	}

	// Every wrapper the gateway injects is cut, not only the director's.
	advice := `{"messages":[{"role":"assistant","content":[{"type":"text","text":"<advisor-answer>\nuse a mutex\n</advisor-answer>\nPatch below."}]}]}`
	out, cut, err = StripEchoedBlocks([]byte(advice))
	if err != nil || !cut || strings.Contains(string(out), "advisor-answer") || !strings.Contains(string(out), "Patch below.") {
		t.Fatalf("advisor block: %s, %v, %v", out, cut, err)
	}

	// The user's own turn keeps the text: only the executor's copy is a problem.
	user := `{"messages":[{"role":"user","content":"why does <director-guidance leak?"}]}`
	out, cut, err = StripEchoedBlocks([]byte(user))
	if err != nil || cut || string(out) != user {
		t.Fatalf("user message changed: %s, %v, %v", out, cut, err)
	}

	// A clean body keeps its bytes, so the prompt cache still covers it.
	clean := `{"messages":[{"role":"assistant","content":"all good"}]}`
	out, cut, err = StripEchoedBlocks([]byte(clean))
	if err != nil || cut || string(out) != clean {
		t.Fatalf("clean body changed: %s, %v, %v", out, cut, err)
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
