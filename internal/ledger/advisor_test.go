package ledger

import "testing"

func TestTrackerRecordsAdvisorIterations(t *testing.T) {
	var stream AnthropicTracker
	stream.Event("message_delta", []byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5,"iterations":[`+
		`{"type":"message","input_tokens":2,"output_tokens":3},`+
		`{"type":"advisor_message","model":"claude-fable-5-1","input_tokens":27076,"output_tokens":414,"cache_read_input_tokens":10,"cache_creation_input_tokens":20}]}}`))
	if len(stream.Advisors) != 1 || stream.Advisors[0].Model != "claude-fable-5-1" || stream.Usage.Output != 5 ||
		stream.Advisors[0].Usage != (Usage{Input: 27076, Output: 414, CacheRead: 10, CacheWrite: 20}) {
		t.Fatalf("stream tracker = %+v", stream)
	}

	var plain AnthropicTracker
	plain.Response(200, []byte(`{"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1,"iterations":[{"type":"advisor_message","model":"claude-fable-5-1","input_tokens":9,"output_tokens":8}]}}`))
	if len(plain.Advisors) != 1 || plain.Advisors[0].Usage.Output != 8 {
		t.Fatalf("response tracker = %+v", plain)
	}

	var none AnthropicTracker
	none.Event("message_delta", []byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`))
	if len(none.Advisors) != 0 {
		t.Fatalf("advisors without iterations = %+v", none.Advisors)
	}
}
