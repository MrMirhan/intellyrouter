package gateway_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/MrMirhan/intellyrouter/internal/provider"
	"github.com/MrMirhan/intellyrouter/internal/store"
)

func TestRouteAdvisorSetting(t *testing.T) {
	cases := []struct {
		name     string
		settings func(opusID int64) string
		check    func(upstream string) bool
	}{
		{"keeps Claude Code's advisor", func(int64) string { return `{}` },
			func(u string) bool { return strings.Contains(u, `"model":"claude-fable-5-1"`) }},
		{"switches the advisor model", func(id int64) string { return fmt.Sprintf(`{"advisor":{"model_id":%d}}`, id) },
			func(u string) bool {
				return strings.Contains(u, `"model":"claude-opus-5"`) && !strings.Contains(u, "claude-fable-5-1")
			}},
		{"turns the advisor off", func(int64) string { return `{"advisor":{"off":true}}` },
			func(u string) bool {
				return !strings.Contains(u, "advisor_20260301") && strings.Contains(u, `"name":"Read"`)
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var mu sync.Mutex
			var upstream string
			e := setup(t, provider.Anthropic, func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				mu.Lock()
				upstream = string(raw)
				mu.Unlock()
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, advisorStream)
			})
			ctx := t.Context()
			providers, err := e.store.ListProviders(ctx)
			must(t, err)
			opus, err := e.store.CreateModel(ctx, store.Model{ProviderID: providers[0].ID, ModelID: "claude-opus-5", Enabled: true})
			must(t, err)
			rt, err := e.store.RouteByName(ctx, route)
			must(t, err)
			rt.Settings = c.settings(opus.ID)
			must(t, e.store.UpdateRoute(ctx, rt))

			if resp, out := send(t, http.MethodPost, e.url+"/v1/messages", advisorBody(route), map[string]string{"X-Intelly-Key": e.key}); resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d: %s", resp.StatusCode, out)
			}
			mu.Lock()
			defer mu.Unlock()
			if !c.check(upstream) {
				t.Fatalf("upstream body = %s", upstream)
			}
		})
	}
}
