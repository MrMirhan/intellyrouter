package guided

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAdvisorTool(t *testing.T) {
	body := `{"tools":[{"name":"Bash","input_schema":{"type":"object"}}],"messages":[]}`
	out, added, err := AddConsultTool([]byte(body), RoleAdvisor)
	if err != nil || !added || !json.Valid(out) || !strings.Contains(string(out), `{"name":"ask_advisor","description":"Ask the advisor, a senior engineer`) {
		t.Fatalf("AddConsultTool = %s, %v, %v", out, added, err)
	}
	if !HasTools(out) || HasTools([]byte(`{"tools":[],"messages":[]}`)) || HasTools([]byte(`{"messages":[]}`)) {
		t.Fatal("HasTools reads the tools array wrong")
	}
}

func TestAdvisorRequest(t *testing.T) {
	out, err := AdvisorRequest([]byte(session), "glm-5.3", "high", "Where is the bug?")
	if err != nil {
		t.Fatal(err)
	}
	var req struct {
		Model        string
		System       string
		OutputConfig struct{ Effort string } `json:"output_config"`
		Messages     []textMessage
	}
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatal(err)
	}
	last := req.Messages[len(req.Messages)-1].Content
	if req.Model != "glm-5.3" || !strings.HasPrefix(req.System, "You advise a coding agent") || req.OutputConfig.Effort != "high" ||
		!strings.Contains(last[len(last)-1].Text, "The executor asks you:\nWhere is the bug?") {
		t.Fatalf("request = %s", out)
	}
}

func TestInjectAdvice(t *testing.T) {
	const earlier = `{"messages":[{"role":"user","content":"fix it"},{"role":"assistant","content":"Running."},{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}`
	out, err := InjectAdvice([]byte(earlier+`]}]}`), "Where?", "In calc.go.")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), earlier+`,{"type":"text","text":"`) ||
		!strings.Contains(string(out), `Earlier in this task you asked the advisor: Where?\n\nIn calc.go.`) {
		t.Fatalf("out = %s", out)
	}
}

func TestRemoveConsultTool(t *testing.T) {
	const body = `{"tools":[{"name":"Bash","input_schema":{"type":"object"}}],"messages":[]}`
	withTool, _, err := AddConsultTool([]byte(body), RoleAdvisor)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := RemoveConsultTool(withTool, RoleAdvisor); err != nil || string(out) != body {
		t.Fatalf("RemoveConsultTool = %s, %v", out, err)
	}
}
