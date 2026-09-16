package gateway_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/MrMirhan/intellyrouter/internal/provider"
	"github.com/MrMirhan/intellyrouter/internal/store"
)

func TestFallbackRouteServesUnknownModelNames(t *testing.T) {
	var upstreamModel string
	e := setup(t, provider.Anthropic, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		upstreamModel = body.Model
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"type":"message","content":[],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	})
	const background = `{"model":"claude-haiku-4-5-20251001","max_tokens":64,"messages":[{"role":"user","content":"title"}]}`
	headers := map[string]string{"X-Api-Key": e.key}

	if resp, _ := send(t, http.MethodPost, e.url+"/v1/messages", background, headers); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("without a fallback: status %d, want 404", resp.StatusCode)
	}
	must(t, e.store.SetSetting(t.Context(), store.FallbackRouteSetting, route))
	resp, out := send(t, http.MethodPost, e.url+"/v1/messages", background, headers)
	if resp.StatusCode != http.StatusOK || upstreamModel != "claude-haiku-4-5" {
		t.Fatalf("with a fallback: status %d, upstream model %q: %s", resp.StatusCode, upstreamModel, out)
	}
	req := onlyRequest(t, e.store)
	if req.Route != route || req.ClientModel != "claude-haiku-4-5-20251001" {
		t.Fatalf("ledger request = %+v", req)
	}
}
