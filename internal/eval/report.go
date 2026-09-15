package eval

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"
)

type Summary struct {
	Route                string  `json:"route"`
	Tasks                int     `json:"tasks"`
	Passed               int     `json:"passed"`
	PassRate             float64 `json:"pass_rate"`
	CostUSD              float64 `json:"cost_usd"`
	SubscriptionValueUSD float64 `json:"subscription_value_usd"`
	// CostPerSolvedUSD counts API spend plus subscription usage at API prices.
	CostPerSolvedUSD   float64 `json:"cost_per_solved_usd"`
	APITokens          int64   `json:"api_tokens"`
	SubscriptionTokens int64   `json:"subscription_tokens"`
	EscalatedRequests  int     `json:"escalated_requests"`
	AvgDurationMS      int64   `json:"avg_duration_ms"`
}

// Summarize groups results by route, in the order routes first appear.
func Summarize(results []Result) []Summary {
	var out []Summary
	index := make(map[string]int)
	var durations []int64
	for _, r := range results {
		i, ok := index[r.Route]
		if !ok {
			i = len(out)
			index[r.Route] = i
			out = append(out, Summary{Route: r.Route})
			durations = append(durations, 0)
		}
		s := &out[i]
		s.Tasks++
		if r.Passed {
			s.Passed++
		}
		s.CostUSD += r.CostUSD
		s.SubscriptionValueUSD += r.SubscriptionValueUSD
		s.APITokens += r.APITokens
		s.SubscriptionTokens += r.SubscriptionTokens
		s.EscalatedRequests += r.EscalatedRequests
		durations[i] += r.DurationMS
	}
	for i := range out {
		s := &out[i]
		s.PassRate = float64(s.Passed) / float64(s.Tasks)
		s.AvgDurationMS = durations[i] / int64(s.Tasks)
		if s.Passed > 0 {
			s.CostPerSolvedUSD = (s.CostUSD + s.SubscriptionValueUSD) / float64(s.Passed)
		}
	}
	return out
}

func WriteTable(w io.Writer, summaries []Summary) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ROUTE\tPASSED\tAPI $\tSUBSCRIPTION VALUE $\t$ PER SOLVED\tAPI TOKENS\tCLAUDE TOKENS\tESCALATED\tAVG TIME")
	for _, s := range summaries {
		fmt.Fprintf(tw, "%s\t%d/%d (%.0f%%)\t%.4f\t%.4f\t%.4f\t%d\t%d\t%d\t%s\n",
			s.Route, s.Passed, s.Tasks, s.PassRate*100, s.CostUSD, s.SubscriptionValueUSD, s.CostPerSolvedUSD,
			s.APITokens, s.SubscriptionTokens, s.EscalatedRequests, (time.Duration(s.AvgDurationMS) * time.Millisecond).Round(time.Second))
	}
	return tw.Flush()
}
