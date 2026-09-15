package gateway_test

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"intellyrouter/internal/provider"
	"intellyrouter/internal/store"
)

func TestRejectedFeaturesAreRemovedAndRemembered(t *testing.T) {
	fixture, err := os.ReadFile("../../testdata/anthropic/stream_tool_use.sse")
	must(t, err)
	var mu sync.Mutex
	var bodies []string
	e := setup(t, provider.Anthropic, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		mu.Unlock()
		reject := func(msg string) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"`+msg+`"}}`)
		}
		switch {
		case bytes.Contains(raw, []byte(`"effort"`)):
			reject("This model does not support the effort parameter.")
		case bytes.Contains(raw, []byte(`"thinking"`)):
			reject("adaptive thinking is not supported on this model")
		case bytes.Contains(raw, []byte(`"role":"system"`)):
			reject("role 'system' is not supported on this model")
		default:
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write(fixture)
		}
	})
	body := `{"model":"intelly-claude-fast","max_tokens":1024,"stream":true,"thinking":{"type":"adaptive"},` +
		`"output_config":{"effort":"high"},"messages":[{"role":"user","content":"hi"},{"role":"system","content":"be brief"}]}`

	for range 2 {
		resp, out := send(t, http.MethodPost, e.url+"/v1/messages", body, map[string]string{"X-Api-Key": e.key})
		if resp.StatusCode != http.StatusOK || !bytes.Equal(out, fixture) {
			t.Fatalf("got %d: %s", resp.StatusCode, out)
		}
	}
	if len(bodies) != 5 {
		t.Fatalf("upstream calls = %d, want 4 for the first request and 1 for the second", len(bodies))
	}
	last := bodies[4]
	if strings.Contains(last, "effort") || strings.Contains(last, "thinking") || !strings.Contains(last, `{"role":"user","content":"be brief"}`) {
		t.Fatalf("adapted body = %s", last)
	}

	items, _, err := e.store.ListRequests(t.Context(), store.RequestFilter{})
	must(t, err)
	first, err := e.store.GetRequest(t.Context(), items[len(items)-1].ID)
	must(t, err)
	if first.Status != "ok" || first.Legs[0].Note != "adapted for claude-haiku-4-5: effort, thinking, system_messages" {
		t.Fatalf("first request = %+v", first)
	}
}

func TestUnrelatedBadRequestIsNotRetried(t *testing.T) {
	const errBody = `{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens: 200000 > 64000, which is the maximum"}}`
	calls := 0
	e := setup(t, provider.Anthropic, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, errBody)
	})
	resp, out := send(t, http.MethodPost, e.url+"/v1/messages", streamBody, map[string]string{"X-Api-Key": e.key})
	if resp.StatusCode != http.StatusBadRequest || string(out) != errBody || calls != 1 {
		t.Fatalf("got %d after %d calls: %s", resp.StatusCode, calls, out)
	}
}
