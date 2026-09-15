package ledger

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"testing"

	"intellyrouter/internal/sse"
)

func assertJSON(t *testing.T, got []byte, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("message is not JSON: %s", got)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("message:\n got %s\nwant %s", got, want)
	}
}

func TestTrackerBuildsMessageFromStream(t *testing.T) {
	f, err := os.Open("../../testdata/anthropic/stream_tool_use.sse")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	tr := AnthropicTracker{Capture: true}
	events := sse.NewReader(f)
	for {
		ev, err := events.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		tr.Event(ev.Name, ev.Data)
	}
	assertJSON(t, tr.Message(), `{"id":"msg_01","type":"message","role":"assistant","model":"claude-haiku-4-5",
"content":[{"type":"text","text":"Reading the file."},{"type":"tool_use","id":"toolu_01","name":"Read","input":{"file_path":"/tmp/a.go"}}],
"stop_reason":"tool_use","usage":{"input_tokens":1200,"cache_creation_input_tokens":300,"cache_read_input_tokens":5000,"output_tokens":87}}`)
}

func TestTrackerMessageBlocks(t *testing.T) {
	type event struct{ name, data string }
	start := event{"message_start", `{"type":"message_start","message":{"id":"msg_2","model":"claude-opus-5","content":[],"usage":{"input_tokens":10,"output_tokens":1}}}`}
	cases := []struct {
		name    string
		capture bool
		events  []event
		want    string
	}{
		{
			name: "thinking, redacted thinking, and text", capture: true,
			events: []event{
				start,
				{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`},
				{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Check "}}`},
				{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"the tests."}}`},
				{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"c2ln"}}`},
				{"content_block_stop", `{"type":"content_block_stop","index":0}`},
				{"content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"redacted_thinking","data":"ZW5j"}}`},
				{"content_block_start", `{"type":"content_block_start","index":2,"content_block":{"type":"text","text":""}}`},
				{"ping", `{"type": "ping"}`},
				{"content_block_delta", `{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"Done."}}`},
				{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":null,"output_tokens":42}}`},
				{"message_stop", `{"type":"message_stop"}`},
			},
			want: `{"id":"msg_2","type":"message","role":"assistant","model":"claude-opus-5",
"content":[{"type":"thinking","thinking":"Check the tests.","signature":"c2ln"},{"type":"redacted_thinking","data":"ZW5j"},{"type":"text","text":"Done."}],
"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":42}}`,
		},
		{
			name: "server tool input is assembled and other blocks stay as started", capture: true,
			events: []event{
				start,
				{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"server_tool_use","id":"srv_1","name":"web_search","input":{}}}`},
				{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"query\":"}}`},
				{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"go\"}"}}`},
				{"content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"web_search_tool_result","tool_use_id":"srv_1","content":[{"type":"web_search_result","url":"https://go.dev"}]}}`},
			},
			want: `{"id":"msg_2","type":"message","role":"assistant","model":"claude-opus-5",
"content":[{"type":"server_tool_use","id":"srv_1","name":"web_search","input":{"query":"go"}},
{"type":"web_search_tool_result","tool_use_id":"srv_1","content":[{"type":"web_search_result","url":"https://go.dev"}]}],
"stop_reason":null,"usage":{"input_tokens":10,"output_tokens":1}}`,
		},
		{
			name: "capture off", capture: false,
			events: []event{start, {"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"x"}}`}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := AnthropicTracker{Capture: c.capture}
			for _, ev := range c.events {
				tr.Event(ev.name, []byte(ev.data))
			}
			if c.want == "" {
				if got := tr.Message(); got != nil {
					t.Fatalf("Message = %s, want nil", got)
				}
				return
			}
			assertJSON(t, tr.Message(), c.want)
		})
	}
}

func TestTrackerMessageFromResponse(t *testing.T) {
	body := []byte(`{"id":"msg_4","type":"message","role":"assistant","content":[],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	on := AnthropicTracker{Capture: true}
	on.Response(200, body)
	if !bytes.Equal(on.Message(), body) {
		t.Fatalf("Message = %s", on.Message())
	}
	failed := AnthropicTracker{Capture: true}
	failed.Response(500, []byte(`{"type":"error","error":{"type":"api_error","message":"boom"}}`))
	if failed.Message() != nil {
		t.Fatalf("error response Message = %s", failed.Message())
	}
	var off AnthropicTracker
	off.Response(200, body)
	if off.Message() != nil {
		t.Fatal("Message without capture is not nil")
	}
}
