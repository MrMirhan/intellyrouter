package ledger

import (
	"io"
	"math"
	"os"
	"testing"

	"github.com/MrMirhan/intellyrouter/internal/sse"
)

func TestBuiltinPriceSpellings(t *testing.T) {
	want := builtinPrices["claude-haiku-4-5"]
	for _, id := range []string{"claude-haiku-4-5", "claude-haiku-4-5-20251001", "anthropic/claude-haiku-4.5", "claude-haiku-4-5[1m]"} {
		if got, ok := BuiltinPrice(id); !ok || got != want {
			t.Errorf("BuiltinPrice(%q) = %+v, %v", id, got, ok)
		}
	}
	if _, ok := BuiltinPrice("deepseek-v4-pro"); ok {
		t.Error("BuiltinPrice matched a non-Claude model")
	}
}

func TestPriceCost(t *testing.T) {
	p := Price{In: 1, Out: 5, CacheRead: 0.1, CacheWrite: 1.25}
	got := p.Cost(Usage{Input: 1200, Output: 87, CacheRead: 5000, CacheWrite: 300})
	if want := 2510.0 / 1e6; math.Abs(got-want) > 1e-12 {
		t.Fatalf("Cost = %v, want %v", got, want)
	}
}

func TestTrackerReadsStream(t *testing.T) {
	f, err := os.Open("../../testdata/anthropic/stream_tool_use.sse")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var tr AnthropicTracker
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
	want := Usage{Input: 1200, Output: 87, CacheRead: 5000, CacheWrite: 300}
	if tr.Usage != want || tr.StopReason != "tool_use" || tr.Error != "" {
		t.Fatalf("tracker = %+v", tr)
	}
}

func TestTrackerReadsResponses(t *testing.T) {
	var ok AnthropicTracker
	ok.Response(200, []byte(`{"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":7}}`))
	if ok.Usage != (Usage{Input: 5, Output: 7}) || ok.StopReason != "end_turn" {
		t.Fatalf("200 response: %+v", ok)
	}
	var bad AnthropicTracker
	bad.Response(429, []byte(`{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`))
	if bad.Error != "rate_limit_error: slow down" {
		t.Fatalf("429 response error = %q", bad.Error)
	}
}
