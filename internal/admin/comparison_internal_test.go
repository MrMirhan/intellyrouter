package admin

import (
	"math"
	"testing"

	"github.com/MrMirhan/intellyrouter/internal/store"
)

func TestNewComparison(t *testing.T) {
	work := store.WorkTotals{APIUSD: 1.5, SubscriptionValueUSD: 2, InputTokens: 1_000_000, OutputTokens: 100_000, CacheReadTokens: 2_000_000}
	// Per 1M tokens: fable 5.1 10/50/0.25, opus 5 and 4.8 5/25/0.5, sonnet 5 2/10/0.2, haiku 4.5 1/5/0.1.
	fable, opus5, opus48 := modelCostJSON{"claude-fable-5-1", 15.5}, modelCostJSON{"claude-opus-5", 8.5}, modelCostJSON{"claude-opus-4-8", 8.5}
	sonnet, haiku := modelCostJSON{"claude-sonnet-5", 3.4}, modelCostJSON{"claude-haiku-4-5", 1.7}
	cases := []struct {
		name      string
		reference string
		want      []modelCostJSON
	}{
		{"default reference", "claude-fable-5-1", []modelCostJSON{fable, opus5, opus48, sonnet, haiku}},
		{"listed reference comes first once", "claude-opus-5", []modelCostJSON{opus5, fable, opus48, sonnet, haiku}},
		{"reference spelled with a date suffix", "claude-haiku-4-5-20251001", []modelCostJSON{{"claude-haiku-4-5-20251001", 1.7}, fable, opus5, opus48, sonnet, haiku}},
		{"unknown reference is skipped", "deepseek-v4-pro", []modelCostJSON{fable, opus5, opus48, sonnet, haiku}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := newComparison(c.reference, work)
			if got.ActualUSD != 3.5 || got.APIUSD != 1.5 || got.SubscriptionValueUSD != 2 || got.ReferenceModel != c.reference ||
				got.WorkTokens != (workTokensJSON{Input: 1_000_000, Output: 100_000, CacheRead: 2_000_000}) {
				t.Fatalf("comparison = %+v", got)
			}
			if len(got.SingleModel) != len(c.want) {
				t.Fatalf("single_model = %+v, want %+v", got.SingleModel, c.want)
			}
			for i, w := range c.want {
				if g := got.SingleModel[i]; g.Model != w.Model || math.Abs(g.CostUSD-w.CostUSD) > 1e-9 {
					t.Errorf("single_model[%d] = %+v, want %+v", i, g, w)
				}
			}
		})
	}
}
