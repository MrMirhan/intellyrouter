package admin

import (
	"net/http"
	"time"

	"intellyrouter/internal/store"
)

type statsRange struct {
	span, bucket time.Duration
}

var statsRanges = map[string]statsRange{
	"24h": {24 * time.Hour, time.Hour},
	"7d":  {7 * 24 * time.Hour, 6 * time.Hour},
	"30d": {30 * 24 * time.Hour, 24 * time.Hour},
}

type statsTotalsJSON struct {
	Requests             int64   `json:"requests"`
	Errors               int64   `json:"errors"`
	EscalateRequests     int64   `json:"escalate_requests"`
	EscalatedRequests    int64   `json:"escalated_requests"`
	CostUSD              float64 `json:"cost_usd"`
	SubscriptionValueUSD float64 `json:"subscription_value_usd"`
	ReferenceCostUSD     float64 `json:"reference_cost_usd"`
	SavingsUSD           float64 `json:"savings_usd"`
	InputTokens          int64   `json:"input_tokens"`
	OutputTokens         int64   `json:"output_tokens"`
	CacheReadTokens      int64   `json:"cache_read_tokens"`
	CacheWriteTokens     int64   `json:"cache_write_tokens"`
	APITokens            int64   `json:"api_tokens"`
	SubscriptionTokens   int64   `json:"subscription_tokens"`
	ClassifierCostUSD    float64 `json:"classifier_cost_usd"`
	DirectorCostUSD      float64 `json:"director_cost_usd"`
}

type statsPointJSON struct {
	TS                   int64   `json:"ts"`
	Requests             int64   `json:"requests"`
	CostUSD              float64 `json:"cost_usd"`
	SubscriptionValueUSD float64 `json:"subscription_value_usd"`
	ReferenceCostUSD     float64 `json:"reference_cost_usd"`
	APITokens            int64   `json:"api_tokens"`
	SubscriptionTokens   int64   `json:"subscription_tokens"`
}

type routeStatsJSON struct {
	Route                string  `json:"route"`
	Requests             int64   `json:"requests"`
	CostUSD              float64 `json:"cost_usd"`
	SubscriptionValueUSD float64 `json:"subscription_value_usd"`
	ReferenceCostUSD     float64 `json:"reference_cost_usd"`
}

type modelStatsJSON struct {
	Provider         string  `json:"provider"`
	Model            string  `json:"model"`
	Billing          string  `json:"billing"`
	Calls            int64   `json:"calls"`
	CostUSD          float64 `json:"cost_usd"`
	InputTokens      int64   `json:"input_tokens"`
	OutputTokens     int64   `json:"output_tokens"`
	CacheReadTokens  int64   `json:"cache_read_tokens"`
	CacheWriteTokens int64   `json:"cache_write_tokens"`
}

type statsJSON struct {
	Range    string           `json:"range"`
	Since    int64            `json:"since"`
	BucketMS int64            `json:"bucket_ms"`
	Totals   statsTotalsJSON  `json:"totals"`
	Series   []statsPointJSON `json:"series"`
	ByRoute  []routeStatsJSON `json:"by_route"`
	ByModel  []modelStatsJSON `json:"by_model"`
}

func (a *API) getStats(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("range")
	if name == "" {
		name = "24h"
	}
	rg, ok := statsRanges[name]
	if !ok {
		writeError(w, http.StatusBadRequest, "range must be one of 24h, 7d, 30d")
		return
	}
	bucket := rg.bucket.Milliseconds()
	since := time.Now().Add(-rg.span).UnixMilli() / bucket * bucket
	st, err := a.store.Stats(r.Context(), since, bucket)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toStatsJSON(name, since, bucket, st))
}

func toStatsJSON(name string, since, bucket int64, st store.Stats) statsJSON {
	t := st.Totals
	out := statsJSON{
		Range: name, Since: since, BucketMS: bucket,
		Totals: statsTotalsJSON{
			Requests: t.Requests, Errors: t.Errors, EscalateRequests: t.EscalateRequests, EscalatedRequests: t.EscalatedRequests,
			CostUSD: t.CostUSD, SubscriptionValueUSD: t.SubscriptionValueUSD, ReferenceCostUSD: t.ReferenceCostUSD, SavingsUSD: t.SavingsUSD,
			InputTokens: t.InputTokens, OutputTokens: t.OutputTokens, CacheReadTokens: t.CacheReadTokens, CacheWriteTokens: t.CacheWriteTokens,
			APITokens: t.APITokens, SubscriptionTokens: t.SubscriptionTokens, ClassifierCostUSD: t.ClassifierCostUSD,
			DirectorCostUSD: t.DirectorCostUSD,
		},
		Series:  make([]statsPointJSON, 0, len(st.Series)),
		ByRoute: make([]routeStatsJSON, 0, len(st.ByRoute)),
		ByModel: make([]modelStatsJSON, 0, len(st.ByModel)),
	}
	for _, p := range st.Series {
		out.Series = append(out.Series, statsPointJSON(p))
	}
	for _, g := range st.ByRoute {
		out.ByRoute = append(out.ByRoute, routeStatsJSON(g))
	}
	for _, g := range st.ByModel {
		out.ByModel = append(out.ByModel, modelStatsJSON(g))
	}
	return out
}
