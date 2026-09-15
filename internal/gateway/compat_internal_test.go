package gateway

import (
	"encoding/json"
	"testing"
)

func TestAdaptationFor(t *testing.T) {
	cases := map[string]string{
		"This model does not support the effort parameter.":         "effort",
		"adaptive thinking is not supported on this model":          "thinking",
		"role 'system' is not supported on this model":              "system_messages",
		"context_management: Extra inputs are not permitted":        "field:context_management",
		"output_config.task_budget: Extra inputs are not permitted": "field:output_config",
		"messages.0.content: Extra inputs are not permitted":        "",
		"max_tokens: 200000 > 64000, which is the maximum":          "",
	}
	for msg, want := range cases {
		got, ok := adaptationFor(msg)
		if got != want || ok != (want != "") {
			t.Errorf("adaptationFor(%q) = %q, %v; want %q", msg, got, ok, want)
		}
	}
}

func TestAdapt(t *testing.T) {
	cases := []struct {
		adaptation, in, want string
	}{
		{"effort", `{"model":"m","output_config":{"effort":"high"},"messages":[]}`, `{"model":"m","messages":[]}`},
		{"effort", `{"output_config":{"effort":"low","format":{"type":"json"}},"model":"m"}`, `{"output_config":{"format":{"type":"json"}},"model":"m"}`},
		{"thinking", `{"model":"m","thinking":{"type":"adaptive"}}`, `{"model":"m"}`},
		{"system_messages", `{"messages":[{"role":"user","content":"hi"},{"role":"system","content":"be brief"}]}`,
			`{"messages":[{"role":"user","content":"hi"},{"role":"user","content":"be brief"}]}`},
		{"field:context_management", `{"model":"m","context_management":{"edits":[]}}`, `{"model":"m"}`},
	}
	for _, c := range cases {
		got, changed, err := adapt([]byte(c.in), c.adaptation)
		if err != nil || !changed || string(got) != c.want || !json.Valid(got) {
			t.Errorf("adapt(%s, %s) = %s, %v, %v; want %s", c.adaptation, c.in, got, changed, err, c.want)
		}
	}
	if _, changed, _ := adapt([]byte(`{"model":"m"}`), "thinking"); changed {
		t.Error("adapt reported a change for a feature the request does not use")
	}
}
