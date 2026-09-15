package duration

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseValid(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"90s", 90 * time.Second},
		{"1h30m", 90 * time.Minute},
		{"2d", 48 * time.Hour},
		{"1w", 7 * 24 * time.Hour},
		{"1d12h", 36 * time.Hour},
		{"250ms", 250 * time.Millisecond},
		{"1m30s500ms", 90*time.Second + 500*time.Millisecond},
		{"1.5h", 90 * time.Minute},
		{"0.5d", 12 * time.Hour},
		{"0.1s", 100 * time.Millisecond},
		{"2.25m", 135 * time.Second},
		{"0s", 0},
	}
	for _, tt := range tests {
		got, err := Parse(tt.in)
		if err != nil {
			t.Errorf("Parse(%q) error: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("Parse(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestParseInvalid(t *testing.T) {
	for _, in := range []string{"", "10", "h", "5x", "-5m", "1..5h", "1.5.2h", ".h", "3 h", "1hh"} {
		if got, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) = %v, want an error", in, got)
		}
	}
}

func TestDurationUnmarshalText(t *testing.T) {
	var cfg struct {
		CacheTTL  Duration `json:"cache_ttl"`
		Retention Duration `json:"retention"`
	}
	if err := json.Unmarshal([]byte(`{"cache_ttl":"750ms","retention":"1w"}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if time.Duration(cfg.CacheTTL) != 750*time.Millisecond {
		t.Errorf("cache_ttl = %v, want 750ms", cfg.CacheTTL)
	}
	if time.Duration(cfg.Retention) != 168*time.Hour {
		t.Errorf("retention = %v, want 168h0m0s", cfg.Retention)
	}
}
