package gateway

import (
	"testing"

	"github.com/MrMirhan/intellyrouter/internal/guided"
)

// The body 9router returns for a non-streaming director request: a complete
// message, then an event-stream terminator.
const messageWithSSETerminator = `{"id":"msg_1","type":"message","role":"assistant",` +
	`"content":[{"type":"thinking","thinking":"","signature":"x"},{"type":"text","text":"Blue."}],` +
	`"stop_reason":"end_turn","usage":{"input_tokens":8,"output_tokens":4}}` + "data: [DONE]\n\n"

func TestFirstJSONValueCutsTheStreamTerminator(t *testing.T) {
	out := firstJSONValue([]byte(messageWithSSETerminator))
	if string(out[len(out)-1:]) != "}" {
		t.Fatalf("body still ends with %q", string(out[max(0, len(out)-16):]))
	}
	// The message survives whole, so the director's answer is readable.
	guidance, _, err := guided.ParseGuidance(out)
	if err != nil {
		t.Fatalf("ParseGuidance = %v", err)
	}
	if guidance != "Blue." {
		t.Fatalf("guidance = %q, want %q", guidance, "Blue.")
	}
}

// Without the cut the body is what the director actually failed on.
func TestParseGuidanceFailsOnTheUncutBody(t *testing.T) {
	_, _, err := guided.ParseGuidance([]byte(messageWithSSETerminator))
	if err == nil {
		t.Fatal("expected the trailing terminator to break the parse")
	}
}

func TestFirstJSONValueLeavesACleanBodyAlone(t *testing.T) {
	for _, body := range []string{
		`{"id":"msg_1","content":[{"type":"text","text":"hi"}]}`,
		`{"type":"error","error":{"type":"invalid_request_error","message":"no"}}`,
		"",
		"not json at all",
	} {
		if out := firstJSONValue([]byte(body)); string(out) != body {
			t.Fatalf("firstJSONValue(%q) = %q", body, out)
		}
	}
}

// Trailing whitespace is not junk worth cutting, and cutting it changes nothing.
func TestFirstJSONValueKeepsWhitespaceHarmless(t *testing.T) {
	out := firstJSONValue([]byte("{\"a\":1}\n  \n"))
	if string(out) != `{"a":1}` && string(out) != "{\"a\":1}\n  \n" {
		t.Fatalf("firstJSONValue = %q", out)
	}
}
