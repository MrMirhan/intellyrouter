package admin

import (
	"net/http"
	"time"

	"intellyrouter/internal/ledger"
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
	Requests             int64          `json:"requests"`
	Errors               int64          `json:"errors"`
	EscalateRequests     int64          `json:"escalate_requests"`
	EscalatedRequests    int64          `json:"escalated_requests"`
	CostUSD              float64        `json:"cost_usd"`
	SubscriptionValueUSD float64        `json:"subscription_value_usd"`
	ReferenceCostUSD     float64        `json:"reference_cost_usd"`
	SavingsUSD           float64        `json:"savings_usd"`
	InputTokens          int64          `json:"input_tokens"`
	OutputTokens         int64          `json:"output_tokens"`
	CacheReadTokens      int64          `json:"cache_read_tokens"`
	CacheWriteTokens     int64          `json:"cache_write_tokens"`
	APITokens            int64          `json:"api_tokens"`
	SubscriptionTokens   int64          `json:"subscription_tokens"`
	ClassifierCostUSD    float64        `json:"classifier_cost_usd"`
	DirectorCostUSD      float64        `json:"director_cost_usd"`
	AdvisorCostUSD       float64        `json:"advisor_cost_usd"`
	Comparison           comparisonJSON `json:"comparison"`
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

type workTokensJSON struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
}

type modelCostJSON struct {
	Model   string  `json:"model"`
	CostUSD float64 `json:"cost_usd"`
}

// comparisonJSON sets what the routed requests cost next to one model serving
// all of their work.
type comparisonJSON struct {
	ActualUSD            float64         `json:"actual_usd"`
	APIUSD               float64         `json:"api_usd"`
	SubscriptionValueUSD float64         `json:"subscription_value_usd"`
	ReferenceModel       string          `json:"reference_model"`
	WorkTokens           workTokensJSON  `json:"work_tokens"`
	SingleModel          []modelCostJSON `json:"single_model"`
}

// comparisonModels are priced after the reference model.
var comparisonModels = []string{"claude-fable-5-1", "claude-opus-5", "claude-opus-4-8", "claude-sonnet-5", "claude-haiku-4-5"}

// newComparison prices the work tokens on the reference model and the
// comparison models with known prices. The actual cost includes routing overhead.
func newComparison(reference string, w store.WorkTotals) comparisonJSON {
	out := comparisonJSON{
		ActualUSD:            w.APIUSD + w.SubscriptionValueUSD,
		APIUSD:               w.APIUSD,
		SubscriptionValueUSD: w.SubscriptionValueUSD,
		ReferenceModel:       reference,
		WorkTokens:           workTokensJSON{Input: w.InputTokens, Output: w.OutputTokens, CacheRead: w.CacheReadTokens, CacheWrite: w.CacheWriteTokens},
		SingleModel:          []modelCostJSON{},
	}
	usage := ledger.Usage{Input: w.InputTokens, Output: w.OutputTokens, CacheRead: w.CacheReadTokens, CacheWrite: w.CacheWriteTokens}
	seen := make(map[string]bool)
	for _, model := range append([]string{reference}, comparisonModels...) {
		p, ok := ledger.BuiltinPrice(model)
		if !ok || seen[model] {
			continue
		}
		seen[model] = true
		out.SingleModel = append(out.SingleModel, modelCostJSON{Model: model, CostUSD: p.Cost(usage)})
	}
	return out
}

// savedAgainst prices the session's work tokens on model and returns the gap
// between that price and what the session actually spent, plus true. A model
// without a known price returns 0, false.
func savedAgainst(model string, w store.WorkTotals) (float64, bool) {
	p, ok := ledger.BuiltinPrice(model)
	if !ok {
		return 0, false
	}
	usage := ledger.Usage{Input: w.InputTokens, Output: w.OutputTokens, CacheRead: w.CacheReadTokens, CacheWrite: w.CacheWriteTokens}
	actual := w.APIUSD + w.SubscriptionValueUSD
	gap := p.Cost(usage) - actual
	if gap < 0 {
		gap = 0
	}
	return gap, true
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
	ref, err := a.referenceModel(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toStatsJSON(name, since, bucket, st, ref))
}

func toStatsJSON(name string, since, bucket int64, st store.Stats, reference string) statsJSON {
	t := st.Totals
	out := statsJSON{
		Range: name, Since: since, BucketMS: bucket,
		Totals: statsTotalsJSON{
			Requests: t.Requests, Errors: t.Errors, EscalateRequests: t.EscalateRequests, EscalatedRequests: t.EscalatedRequests,
			CostUSD: t.CostUSD, SubscriptionValueUSD: t.SubscriptionValueUSD, ReferenceCostUSD: t.ReferenceCostUSD, SavingsUSD: t.SavingsUSD,
			InputTokens: t.InputTokens, OutputTokens: t.OutputTokens, CacheReadTokens: t.CacheReadTokens, CacheWriteTokens: t.CacheWriteTokens,
			APITokens: t.APITokens, SubscriptionTokens: t.SubscriptionTokens, ClassifierCostUSD: t.ClassifierCostUSD,
			DirectorCostUSD: t.DirectorCostUSD, AdvisorCostUSD: t.AdvisorCostUSD, Comparison: newComparison(reference, st.Work),
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
