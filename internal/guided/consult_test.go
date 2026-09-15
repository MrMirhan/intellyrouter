package guided

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAddConsultTool(t *testing.T) {
	body := `{"model":"m","tools":[{"name":"Bash","input_schema":{"type":"object"}}],"messages":[]}`
	out, added, err := AddConsultTool([]byte(body), RoleDirector)
	if err != nil || !added || !json.Valid(out) {
		t.Fatalf("AddConsultTool = %s, %v, %v", out, added, err)
	}
	if !strings.HasPrefix(string(out), `{"model":"m","tools":[{"name":"Bash","input_schema":{"type":"object"}},{"name":"ask_director"`) {
		t.Fatalf("existing tools changed: %s", out)
	}
	if _, again, _ := AddConsultTool(out, RoleDirector); again {
		t.Fatal("tool added twice")
	}
	empty, added, err := AddConsultTool([]byte(`{"tools":[],"messages":[]}`), RoleDirector)
	if err != nil || !added || !strings.HasPrefix(string(empty), `{"tools":[{"name":"ask_director"`) {
		t.Fatalf("empty tools: %s, %v", empty, err)
	}
	if _, _, err := AddConsultTool([]byte(`{"tools":null,"messages":[]}`), RoleDirector); err == nil {
		t.Fatal("request without a tools array accepted")
	}
}

func TestAppendConsultAnswer(t *testing.T) {
	body := `{"messages":[{"role":"user","content":[{"type":"text","text":"fix it"}]}]}`
	var req struct {
		Messages []textMessage `json:"messages"`
	}
	decode := func(out []byte) {
		t.Helper()
		req.Messages = nil
		if err := json.Unmarshal(out, &req); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}

	out, err := AppendConsultAnswer([]byte(body), RoleDirector, "Asking.", "Where?", "In calc.go.")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), `{"messages":[{"role":"user","content":[{"type":"text","text":"fix it"}]},{"role":"assistant"`) {
		t.Fatalf("earlier messages changed: %s", out)
	}
	decode(out)
	answer := req.Messages[2].Content[0].Text
	if len(req.Messages) != 3 || req.Messages[1].Content[0].Text != "Asking." || req.Messages[2].Role != "user" ||
		!strings.Contains(answer, "You asked the director: Where?") || !strings.Contains(answer, "In calc.go.") {
		t.Fatalf("messages = %s", out)
	}

	out, err = AppendConsultAnswer([]byte(body), RoleDirector, "  ", "Where?", "In calc.go.")
	if err != nil {
		t.Fatal(err)
	}
	decode(out)
	if req.Messages[1].Role != "assistant" || req.Messages[1].Content[0].Text == "" {
		t.Fatalf("empty executor text left an empty block: %s", out)
	}
}

func TestDirectorRequestForQuestion(t *testing.T) {
	out, err := DirectorRequest([]byte(session), "m", DirectorSettings{}, ReasonQuestion, "Where is the bug?", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `The executor asks you:\nWhere is the bug?`) {
		t.Fatalf("question missing: %s", out)
	}
}
