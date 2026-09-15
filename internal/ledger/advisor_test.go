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

func TestOneHourCacheWrites(t *testing.T) {
	fable := Price{In: 10, Out: 50, CacheRead: 0.25, CacheWrite: 12.5}
	if got := fable.Cost(Usage{CacheWrite: 1_000_000, CacheWrite1h: 1_000_000}); got != 20 {
		t.Fatalf("one-hour write cost = %v, want 20", got)
	}
	if got := fable.Cost(Usage{CacheWrite: 2_000_000, CacheWrite1h: 1_000_000}); got != 32.5 {
		t.Fatalf("mixed write cost = %v, want 32.5", got)
	}

	var tr AnthropicTracker
	tr.Event("message_delta", []byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"cache_creation_input_tokens":300,"cache_creation":{"ephemeral_5m_input_tokens":100,"ephemeral_1h_input_tokens":200},`+
		`"iterations":[{"type":"advisor_message","model":"claude-opus-5","input_tokens":5,"cache_creation_input_tokens":9000,"cache_creation":{"ephemeral_1h_input_tokens":9000}}]}}`))
	if tr.Usage.CacheWrite != 300 || tr.Usage.CacheWrite1h != 200 || len(tr.Advisors) != 1 || tr.Advisors[0].Usage.CacheWrite1h != 9000 {
		t.Fatalf("tracker = %+v", tr)
	}
}
