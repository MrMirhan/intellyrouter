package translate

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/MrMirhan/intellyrouter/internal/sse"
)

type event struct{ name, data string }

func runStream(t *testing.T, input io.Reader, model string) ([]event, error) {
	t.Helper()
	var events []event
	s := NewStream(model, func(name string, data []byte) error {
		events = append(events, event{name, string(data)})
		return nil
	})
	r := sse.NewReader(input)
	for {
		ev, err := r.Next()
		if err == io.EOF || (err == nil && string(ev.Data) == "[DONE]") {
			return events, s.Finish()
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Chunk(ev.Data); err != nil {
			return events, err
		}
	}
}

func openFixture(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open("../../testdata/openai/" + name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestStreamTextAndInterleavedToolCalls(t *testing.T) {
	events, err := runStream(t, openFixture(t, "stream_text_tools.sse"), "deepseek-v4-flash")
	if err != nil {
		t.Fatal(err)
	}
	want := []event{
		{"message_start", `{"type":"message_start","message":{"id":"msg_chatcmpl-1","type":"message","role":"assistant","model":"deepseek-v4-flash","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`},
		{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Let me "}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"check."}}`},
		{"content_block_stop", `{"type":"content_block_stop","index":0}`},
		{"content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_a","name":"Read","input":{}}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"file_path\":\"/tmp/a.go\"}"}}`},
		{"content_block_stop", `{"type":"content_block_stop","index":1}`},
		{"content_block_start", `{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"call_b","name":"Grep","input":{}}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"pattern\":\"TODO\"}"}}`},
		{"content_block_stop", `{"type":"content_block_stop","index":2}`},
		{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":500,"output_tokens":60,"cache_read_input_tokens":1000,"cache_creation_input_tokens":0}}`},
		{"message_stop", `{"type":"message_stop"}`},
	}
	if len(events) != len(want) {
		t.Fatalf("got %d events, want %d:\n%v", len(events), len(want), events)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Errorf("event %d\n got: %s %s\nwant: %s %s", i, events[i].name, events[i].data, want[i].name, want[i].data)
		}
	}
}

// Recorded from Gemini's OpenAI-compatible endpoint: one chunk carries the
// complete tool call, the finish reason, and usage.
func TestStreamGeminiSingleChunkToolCall(t *testing.T) {
	events, err := runStream(t, openFixture(t, "gemini_stream_tools.sse"), "gemini-2.5-flash")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range events {
		names = append(names, e.name)
	}
	wantNames := "message_start content_block_start content_block_delta content_block_stop message_delta message_stop"
	if strings.Join(names, " ") != wantNames {
		t.Fatalf("events = %v", names)
	}
	if !strings.Contains(events[1].data, `"id":"function-call-11628457821388745023","name":"Read"`) ||
		!strings.Contains(events[2].data, `"partial_json":"{\"file_path\":\"/tmp/a.go\"}"`) ||
		!strings.Contains(events[4].data, `"stop_reason":"tool_use"`) ||
		!strings.Contains(events[4].data, `"input_tokens":108,"output_tokens":19`) {
		t.Fatalf("unexpected translation: %v", events)
	}
}

func TestStreamSplitsCallsThatShareAnIndex(t *testing.T) {
	in := `data: {"id":"c","choices":[{"delta":{"tool_calls":[{"index":0,"id":"a","function":{"name":"Read","arguments":"{\"file_path\":\"x\"}"}}]}}]}

data: {"id":"c","choices":[{"delta":{"tool_calls":[{"index":0,"id":"b","function":{"name":"Grep","arguments":"{\"pattern\":\"y\"}"}}]},"finish_reason":"tool_calls"}]}

data: [DONE]
`
	events, err := runStream(t, strings.NewReader(in), "m")
	if err != nil {
		t.Fatal(err)
	}
	starts := 0
	for _, e := range events {
		if e.name == "content_block_start" {
			starts++
		}
	}
	if starts != 2 {
		t.Fatalf("got %d tool blocks, want 2: %v", starts, events)
	}
}

func TestStreamErrorChunk(t *testing.T) {
	in := "data: {\"error\":{\"message\":\"boom\",\"code\":500}}\n\n"
	events, err := runStream(t, strings.NewReader(in), "m")
	var upErr *UpstreamError
	if !errors.As(err, &upErr) || upErr.Message != "boom" {
		t.Fatalf("err = %v", err)
	}
	if len(events) != 1 || events[0].name != "error" || !strings.Contains(events[0].data, `"message":"boom"`) {
		t.Fatalf("events = %v", events)
	}
}
