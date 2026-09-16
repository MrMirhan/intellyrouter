package store

import (
	"errors"
	"math"
	"reflect"
	"testing"
)

func TestSessions(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	requests := []Request{
		{
			TS: 1000, SessionID: "s1", Route: "guided", Strategy: StrategyGuided, Status: "ok", CostUSD: 0.03,
			Legs: []Leg{
				{Role: "director", Provider: "a", Model: "opus", Billing: "api", InputTokens: 100, OutputTokens: 10, CostUSD: 0.02, LatencyMS: 300, Status: "ok", Note: "checkpoint: plan"},
				{Seq: 1, Role: "executor", Provider: "d", Model: "flash", Billing: "api", InputTokens: 1000, OutputTokens: 50, CacheReadTokens: 400, CostUSD: 0.01, LatencyMS: 700, Status: "ok"},
				{Seq: 2, Role: "advisor", Provider: "d", Model: "flash", Billing: "api", LatencyMS: 120, Status: "ok", Note: "question: which file?"},
			},
		},
		{
			TS: 2000, SessionID: "s1", AgentID: "a1", Route: "auto", Strategy: StrategyEscalate, Status: "upstream_error", CostUSD: 0.001, SubscriptionValueUSD: 0.5,
			Legs: []Leg{
				{Role: "classifier", Provider: "d", Model: "flash", Billing: "api", InputTokens: 30, OutputTokens: 3, CostUSD: 0.001, LatencyMS: 50, Status: "ok"},
				{Seq: 1, Role: "escalation", Provider: "c", Model: "opus", Billing: "subscription", InputTokens: 2000, OutputTokens: 200, CacheWriteTokens: 100, CostUSD: 0.5, LatencyMS: 900, Status: "upstream_error"},
			},
		},
		{
			TS: 3000, SessionID: "s1", Route: "guided", Strategy: StrategyGuided, Status: "canceled", SubscriptionValueUSD: 0.2,
			Legs: []Leg{{Role: "director", Provider: "c", Model: "opus", Billing: "subscription", InputTokens: 500, OutputTokens: 20, CostUSD: 0.2, LatencyMS: 400, Status: "canceled", Note: "director step: review"}},
		},
		{
			TS: 4000, SessionID: "s2", Route: "direct", Strategy: StrategyDirect, Status: "ok", CostUSD: 0.004,
			Legs: []Leg{{Role: "direct", Provider: "d", Model: "flash", Billing: "api", InputTokens: 10, OutputTokens: 1, CostUSD: 0.004, Status: "ok"}},
		},
		{TS: 5000, Route: "direct", Strategy: StrategyDirect, Status: "ok"},
	}
	var ids []int64
	for _, r := range requests {
		ids = append(ids, insertTestRequest(t, s, r))
	}
	if err := s.SaveContent(ctx, ids[0], Content{CreatedAt: 1000, Input: []byte(`{"model":"guided","messages":[]}`)}); err != nil {
		t.Fatal(err)
	}

	items, total, err := s.ListSessions(ctx, SessionFilter{})
	if err != nil || total != 2 || len(items) != 2 || items[0].SessionID != "s2" || items[1].SessionID != "s1" {
		t.Fatalf("ListSessions = %+v, %d, %v", items, total, err)
	}
	s1 := items[1]
	ints := []struct {
		name      string
		got, want int64
	}{
		{"first ts", s1.FirstTS, 1000}, {"last ts", s1.LastTS, 3000}, {"requests", s1.Requests, 3}, {"errors", s1.Errors, 1},
		{"agents", s1.Agents, 2}, {"input", s1.InputTokens, 3630}, {"output", s1.OutputTokens, 283},
		{"cache read", s1.CacheReadTokens, 400}, {"cache write", s1.CacheWriteTokens, 100},
		{"director calls", s1.DirectorCalls, 2}, {"advisor calls", s1.AdvisorCalls, 1}, {"captured", s1.CapturedRequests, 1},
	}
	for _, c := range ints {
		if c.got != c.want {
			t.Errorf("s1 %s = %d, want %d", c.name, c.got, c.want)
		}
	}
	if math.Abs(s1.CostUSD-0.031) > 1e-9 || math.Abs(s1.SubscriptionValueUSD-0.7) > 1e-9 {
		t.Errorf("s1 cost = %v, subscription value = %v", s1.CostUSD, s1.SubscriptionValueUSD)
	}
	wantModels := []ModelCalls{{"flash", "api", 3}, {"opus", "subscription", 2}, {"opus", "api", 1}}
	if !reflect.DeepEqual(s1.Routes, []string{"auto", "guided"}) || !reflect.DeepEqual(s1.Models, wantModels) {
		t.Errorf("s1 routes = %v, models = %+v", s1.Routes, s1.Models)
	}

	filtered, total, err := s.ListSessions(ctx, SessionFilter{Route: "auto"})
	if err != nil || total != 1 || len(filtered) != 1 || filtered[0].Requests != 3 {
		t.Errorf("ListSessions(route auto) = %+v, %d, %v", filtered, total, err)
	}

	sess, err := s.Session(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sess.Summary, s1) || len(sess.ByModel) != 6 {
		t.Errorf("session summary = %+v, by model = %+v", sess.Summary, sess.ByModel)
	}
	if len(sess.Checkpoints) != 3 || sess.Checkpoints[0].RequestID != ids[0] || sess.Checkpoints[0].Billing != "api" ||
		sess.Checkpoints[1].Role != "advisor" || sess.Checkpoints[1].Note != "question: which file?" ||
		sess.Checkpoints[2].Note != "director step: review" || sess.Checkpoints[2].LatencyMS != 400 {
		t.Errorf("checkpoints = %+v", sess.Checkpoints)
	}
	// Work skips the classifier and the API director consult but keeps the subscription director step.
	wantWork := WorkTotals{APIUSD: 0.031, SubscriptionValueUSD: 0.7, InputTokens: 3500, OutputTokens: 270, CacheReadTokens: 400, CacheWriteTokens: 100}
	if w := sess.Work; math.Abs(w.APIUSD-wantWork.APIUSD) > 1e-9 || math.Abs(w.SubscriptionValueUSD-wantWork.SubscriptionValueUSD) > 1e-9 ||
		w.InputTokens != wantWork.InputTokens || w.OutputTokens != wantWork.OutputTokens || w.CacheReadTokens != wantWork.CacheReadTokens || w.CacheWriteTokens != wantWork.CacheWriteTokens {
		t.Errorf("work = %+v, want %+v", w, wantWork)
	}
	if len(sess.Requests) != 3 || sess.Requests[0].ID != ids[0] || !sess.Requests[0].Captured || len(sess.Requests[1].Legs) != 2 {
		t.Errorf("session requests = %+v", sess.Requests)
	}

	if _, err := s.Session(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Session(missing): err = %v, want ErrNotFound", err)
	}
	if _, err := s.Session(ctx, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("Session(\"\"): err = %v, want ErrNotFound", err)
	}

	st, err := s.Stats(ctx, 0, 3_600_000)
	if err != nil || st.Work.InputTokens != 3510 || math.Abs(st.Work.APIUSD-0.035) > 1e-9 {
		t.Errorf("stats work = %+v, %v", st.Work, err)
	}
}
