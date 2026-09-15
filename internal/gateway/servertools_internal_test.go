package gateway

import (
	"strings"
	"testing"

	"intellyrouter/internal/provider"
	"intellyrouter/internal/store"
)

func TestDropAdvisorTools(t *testing.T) {
	body := []byte(`{"model":"x","tools":[{"name":"Read","input_schema":{"type":"object"}},{"type":"advisor_20260301","name":"advisor","model":"claude-opus-5"}],"messages":[]}`)
	for _, c := range []struct {
		typ      provider.Type
		wantDrop bool
	}{
		{provider.AnthropicCompatible, true},
		{provider.Type("openrouter"), true},
		{provider.Anthropic, false},
		{provider.AnthropicSubscription, false},
	} {
		t.Run(string(c.typ), func(t *testing.T) {
			out, dropped, err := dropAdvisorTools(target{config: provider.Config{Type: c.typ}, model: store.Model{ModelID: "some-model"}}, body)
			if err != nil || dropped != c.wantDrop || strings.Contains(string(out), "advisor_20260301") == c.wantDrop || !strings.Contains(string(out), `"name":"Read"`) {
				t.Fatalf("dropped=%v body=%s err=%v", dropped, out, err)
			}
		})
	}
}

func TestRemoveAdvisorToolsByModel(t *testing.T) {
	body := []byte(`{"tools":[{"type":"advisor_20260301","name":"advisor","model":"claude-opus-5"},{"name":"Read"}],"messages":[]}`)
	if out, dropped, err := removeAdvisorTools(body, "another-model"); err != nil || dropped || string(out) != string(body) {
		t.Fatalf("other advisor model: %s, %v, %v", out, dropped, err)
	}
	out, dropped, err := removeAdvisorTools(body, "claude-opus-5")
	if err != nil || !dropped || strings.Contains(string(out), "advisor") || !strings.Contains(string(out), `"name":"Read"`) {
		t.Fatalf("matching advisor model: %s, %v, %v", out, dropped, err)
	}
	only := []byte(`{"tools":[{"type":"advisor_20260301","name":"advisor","model":"m"}],"messages":[]}`)
	if out, dropped, err := removeAdvisorTools(only, ""); err != nil || !dropped || strings.Contains(string(out), "tools") {
		t.Fatalf("only advisor tool: %s, %v, %v", out, dropped, err)
	}

	for message, want := range map[string]string{
		"'claude-opus-5' cannot be used as an advisor when the request model is 'claude-fable-5-1'": "advisor:claude-opus-5",
		"This advisor cannot be used as an advisor for this model":                                  "advisor",
	} {
		if got, ok := adaptationFor(message); !ok || got != want {
			t.Errorf("adaptationFor(%q) = %q, %v; want %q", message, got, ok, want)
		}
	}
}
