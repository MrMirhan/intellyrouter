package gateway_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	"github.com/MrMirhan/intellyrouter/internal/admin"
	"github.com/MrMirhan/intellyrouter/internal/provider"
	"github.com/MrMirhan/intellyrouter/internal/store"
)

func sameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

func TestCaptureContentOfStreamedDirectRequest(t *testing.T) {
	fixture, err := os.ReadFile("../../testdata/anthropic/stream_tool_use.sse")
	must(t, err)
	upstream := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(fixture)
	}

	off := setup(t, provider.Anthropic, upstream)
	send(t, http.MethodPost, off.url+"/v1/messages", streamBody, map[string]string{"X-Api-Key": off.key})
	if req := onlyRequest(t, off.store); req.Captured {
		t.Fatal("content was captured while capture_content is off")
	}

	e := setup(t, provider.Anthropic, upstream)
	must(t, e.store.SetSetting(t.Context(), store.CaptureContentSetting, "true"))
	resp, out := send(t, http.MethodPost, e.url+"/v1/messages", streamBody, map[string]string{"X-Api-Key": e.key, "X-Claude-Code-Session-Id": "sess-1"})
	if resp.StatusCode != http.StatusOK || !bytes.Equal(out, fixture) {
		t.Fatalf("status %d: %s", resp.StatusCode, out)
	}
	req := onlyRequest(t, e.store)

	token, err := e.store.EnsureAdminToken(t.Context(), false)
	must(t, err)
	mux := http.NewServeMux()
	admin.New(e.store, http.DefaultClient, slog.New(slog.DiscardHandler), nil, "claude").Register(mux)
	adminSrv := httptest.NewServer(mux)
	t.Cleanup(adminSrv.Close)

	resp, out = send(t, http.MethodGet, fmt.Sprintf("%s/api/admin/requests/%d/content", adminSrv.URL, req.ID), "", map[string]string{"Authorization": "Bearer " + token})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("content status %d: %s", resp.StatusCode, out)
	}
	var got struct {
		MessageCount int             `json:"message_count"`
		Request      json.RawMessage `json:"request"`
		Response     json.RawMessage `json:"response"`
		Legs         []struct {
			Role   string          `json:"role"`
			Output json.RawMessage `json:"output"`
		} `json:"legs"`
	}
	must(t, json.Unmarshal(out, &got))
	if got.MessageCount != 1 || !sameJSON(t, got.Request, []byte(streamBody)) {
		t.Fatalf("captured request = %d messages, %s", got.MessageCount, got.Request)
	}
	const want = `{"id":"msg_01","type":"message","role":"assistant","model":"claude-haiku-4-5",
"content":[{"type":"text","text":"Reading the file."},{"type":"tool_use","id":"toolu_01","name":"Read","input":{"file_path":"/tmp/a.go"}}],
"stop_reason":"tool_use","usage":{"input_tokens":1200,"cache_creation_input_tokens":300,"cache_read_input_tokens":5000,"output_tokens":87}}`
	if !sameJSON(t, got.Response, []byte(want)) || len(got.Legs) != 1 || got.Legs[0].Role != "direct" || !sameJSON(t, got.Legs[0].Output, []byte(want)) {
		t.Fatalf("captured response = %s, legs = %+v", got.Response, got.Legs)
	}
}
