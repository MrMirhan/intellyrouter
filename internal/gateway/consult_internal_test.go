package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"intellyrouter/internal/guided"
)

// writeInChunks splits the stream at arbitrary points, as the network does.
func writeInChunks(t *testing.T, w io.Writer, s string) {
	t.Helper()
	for len(s) > 0 {
		n := min(7, len(s))
		if _, err := w.Write([]byte(s[:n])); err != nil {
			t.Fatal(err)
		}
		s = s[n:]
	}
}

func startSegment(cw *consultWriter, status int) {
	cw.Header().Set("Content-Type", "text/event-stream")
	cw.WriteHeader(status)
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("../../testdata/anthropic/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestConsultWriterStitchesContinuation(t *testing.T) {
	rec := httptest.NewRecorder()
	cw := newConsultWriter(rec, guided.ConsultToolName)
	startSegment(cw, http.StatusOK)
	writeInChunks(t, cw, strings.ReplaceAll(readFixture(t, "stream_ask_director.sse"), "\n", "\r\n"))

	call, ok := cw.endSegment()
	if !ok || call.question != "Which file has the bug?" || call.text != "Checking the code." || call.clientTools {
		t.Fatalf("call = %+v, %v", call, ok)
	}
	if out := rec.Body.String(); strings.Contains(out, "ask_director") || strings.Contains(out, "message_stop") {
		t.Fatalf("hidden call or message end reached the client:\n%s", out)
	}

	cw.continueSegment()
	startSegment(cw, http.StatusOK)
	writeInChunks(t, cw, readFixture(t, "stream_after_answer.sse"))
	if _, ok := cw.endSegment(); ok {
		t.Fatal("continuation reported another question")
	}

	out := rec.Body.String()
	switch {
	case strings.Count(out, "event: message_start") != 1 || strings.Contains(out, "msg_continued"):
		t.Fatalf("continuation started a second message:\n%s", out)
	case !strings.Contains(out, `"index":1,"content_block":{"type":"text"`) || !strings.Contains(out, "calc.go has the bug."):
		t.Fatalf("continuation block not renumbered:\n%s", out)
	case !strings.Contains(out, `"stop_reason":"end_turn"`) || !strings.Contains(out, `"output_tokens":25`):
		t.Fatalf("final message_delta wrong:\n%s", out)
	case strings.Count(out, "event: message_stop") != 1:
		t.Fatalf("message_stop count wrong:\n%s", out)
	}
}

func TestConsultWriterReleasesWithClientTools(t *testing.T) {
	stream := strings.Join([]string{
		`event: message_start
data: {"type":"message_start","message":{"id":"m1","usage":{"input_tokens":5,"output_tokens":1}}}`,
		`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_ask","name":"ask_director","input":{}}}`,
		`event: content_block_stop
data: {"type":"content_block_stop","index":0}`,
		`event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_bash","name":"Bash","input":{}}}`,
		`event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"command\":\"ls\"}"}}`,
		`event: content_block_stop
data: {"type":"content_block_stop","index":1}`,
		`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":9}}`,
		`event: message_stop
data: {"type":"message_stop"}`,
	}, "\n\n") + "\n\n"

	rec := httptest.NewRecorder()
	cw := newConsultWriter(rec, guided.ConsultToolName)
	startSegment(cw, http.StatusOK)
	writeInChunks(t, cw, stream)
	call, ok := cw.endSegment()
	if !ok || !call.clientTools || call.question == "" {
		t.Fatalf("call = %+v, %v", call, ok)
	}
	if out := rec.Body.String(); !strings.Contains(out, `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_bash"`) || strings.Contains(out, "message_stop") {
		t.Fatalf("client output before release:\n%s", out)
	}
	cw.release()
	if out := rec.Body.String(); strings.Count(out, "event: message_stop") != 1 || !strings.Contains(out, `"stop_reason":"tool_use"`) {
		t.Fatalf("client output after release:\n%s", out)
	}
}

func TestConsultWriterReportsContinuationError(t *testing.T) {
	rec := httptest.NewRecorder()
	cw := newConsultWriter(rec, guided.ConsultToolName)
	startSegment(cw, http.StatusOK)
	writeInChunks(t, cw, readFixture(t, "stream_ask_director.sse"))
	if _, ok := cw.endSegment(); !ok {
		t.Fatal("question not detected")
	}
	cw.continueSegment()
	cw.Header().Set("Content-Type", "application/json")
	cw.WriteHeader(529)
	writeInChunks(t, cw, `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)
	if _, ok := cw.endSegment(); ok {
		t.Fatal("failed continuation reported a question")
	}
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\"") {
		t.Fatalf("client output:\n%s", rec.Body.String())
	}
}
