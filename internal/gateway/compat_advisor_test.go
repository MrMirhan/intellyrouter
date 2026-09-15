package gateway_test

import (
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"intellyrouter/internal/provider"
)

// A route can send the same advisor to models that accept it and to a model
// that rejects it. The gateway learns the rejection from the 400 and retries.
func TestRejectedAdvisorIsLearned(t *testing.T) {
	var mu sync.Mutex
	var sawAdvisor []bool
	e := setup(t, provider.Anthropic, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		has := strings.Contains(string(raw), `"model":"claude-opus-5"`)
		mu.Lock()
		sawAdvisor = append(sawAdvisor, has)
		mu.Unlock()
		if has {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"'claude-opus-5' cannot be used as an advisor when the request model is 'claude-haiku-4-5'"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, advisorStream)
	})
	body := strings.Replace(advisorBody(route), "claude-fable-5-1", "claude-opus-5", 1)
	for range 2 {
		if resp, out := send(t, http.MethodPost, e.url+"/v1/messages", body, map[string]string{"X-Intelly-Key": e.key}); resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d: %s", resp.StatusCode, out)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(sawAdvisor, []bool{true, false, false}) {
		t.Fatalf("upstream saw the advisor = %v, want only the first attempt", sawAdvisor)
	}
	legs := requestLegs(t, e.store)
	if len(legs) != 2 || !strings.Contains(legs[0][0].Note, "advisor:claude-opus-5") || !strings.Contains(legs[1][0].Note, "advisor:claude-opus-5") {
		t.Fatalf("legs = %+v", legs)
	}
}
