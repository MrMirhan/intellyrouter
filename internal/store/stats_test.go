package store

import (
	"math"
	"testing"
)

func TestStats(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	const hour = 3_600_000
	requests := []Request{
		{
			TS: 1000, Route: "direct-flash", Strategy: StrategyDirect, Status: "ok", CostUSD: 0.01, ReferenceCostUSD: 0.05,
			Legs: []Leg{{Role: "direct", Provider: "deepseek", Model: "flash", Billing: "api", InputTokens: 100, OutputTokens: 10, CostUSD: 0.01, Status: "ok"}},
		},
		// A client that disconnects is not an error.
		{TS: 2000, Route: "direct-flash", Strategy: StrategyDirect, Status: "canceled"},
		{
			TS: hour + 1, Route: "auto", Strategy: StrategyEscalate, Status: "ok", CostUSD: 0.002, SubscriptionValueUSD: 0.3,
			Legs: []Leg{
				{Role: "classifier", Provider: "deepseek", Model: "flash", Billing: "api", InputTokens: 50, OutputTokens: 10, CostUSD: 0.002, Status: "ok"},
				{Seq: 1, Role: "escalation", Provider: "claude", Model: "opus", Billing: "subscription", InputTokens: 1000, OutputTokens: 100, CacheReadTokens: 500, CostUSD: 0.3, Status: "ok"},
			},
		},
		{
			TS: hour + 2, Route: "auto", Strategy: StrategyEscalate, Status: "upstream_error", CostUSD: 0.004, ReferenceCostUSD: 0.02,
			Legs: []Leg{
				{Role: "classifier", Provider: "deepseek", Model: "flash", Billing: "api", InputTokens: 10, OutputTokens: 1, CostUSD: 0.001, Status: "ok"},
				{Seq: 1, Role: "executor", Provider: "deepseek", Model: "flash", Billing: "api", InputTokens: 200, OutputTokens: 20, CostUSD: 0.003, Status: "upstream_error"},
			},
		},
		{
			TS: hour + 3, Route: "guided", Strategy: StrategyGuided, Status: "ok", CostUSD: 0.012, ReferenceCostUSD: 0.1,
			Legs: []Leg{
				{Role: "director", Provider: "anthropic", Model: "sonnet", Billing: "api", InputTokens: 300, OutputTokens: 30, CostUSD: 0.01, Status: "ok"},
				{Seq: 1, Role: "executor", Provider: "deepseek", Model: "flash", Billing: "api", InputTokens: 400, OutputTokens: 40, CostUSD: 0.002, Status: "ok"},
			},
		},
	}
	for _, r := range requests {
		if _, err := s.InsertRequest(ctx, r); err != nil {
			t.Fatal(err)
		}
	}

	st, err := s.Stats(ctx, 0, hour)
	if err != nil {
		t.Fatal(err)
	}
	tot := st.Totals
	ints := []struct {
		name      string
		got, want int64
	}{
		{"requests", tot.Requests, 5}, {"errors", tot.Errors, 1},
		{"escalate requests", tot.EscalateRequests, 2}, {"escalated requests", tot.EscalatedRequests, 1},
		{"input tokens", tot.InputTokens, 2060}, {"output tokens", tot.OutputTokens, 211}, {"cache read", tot.CacheReadTokens, 500},
		{"api tokens", tot.APITokens, 770}, {"subscription tokens", tot.SubscriptionTokens, 1600},
	}
	for _, c := range ints {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
	floats := []struct {
		name      string
		got, want float64
	}{
		{"cost", tot.CostUSD, 0.028}, {"subscription value", tot.SubscriptionValueUSD, 0.3},
		{"reference", tot.ReferenceCostUSD, 0.17}, {"savings", tot.SavingsUSD, 0.144},
		{"classifier cost", tot.ClassifierCostUSD, 0.003}, {"director cost", tot.DirectorCostUSD, 0.01},
	}
	for _, c := range floats {
		if math.Abs(c.got-c.want) > 1e-9 {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}

	if len(st.Series) != 2 || st.Series[0].TS != 0 || st.Series[0].Requests != 2 || st.Series[0].APITokens != 110 ||
		st.Series[1].TS != hour || st.Series[1].Requests != 3 || st.Series[1].APITokens != 660 || st.Series[1].SubscriptionTokens != 1600 {
		t.Errorf("series = %+v", st.Series)
	}
	if len(st.ByRoute) != 3 || st.ByRoute[0].Route != "auto" || st.ByRoute[0].Requests != 2 {
		t.Errorf("by route = %+v", st.ByRoute)
	}
	if len(st.ByModel) != 3 || st.ByModel[0].Model != "flash" || st.ByModel[0].Calls != 5 || st.ByModel[1].Billing != "subscription" {
		t.Errorf("by model = %+v", st.ByModel)
	}

	empty, err := s.Stats(ctx, 10*hour, hour)
	if err != nil || empty.Totals.Requests != 0 || empty.Series == nil || empty.ByRoute == nil || empty.ByModel == nil {
		t.Fatalf("empty range = %+v, %v", empty, err)
	}
}
