package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/MrMirhan/intellyrouter/internal/ledger"
	"github.com/MrMirhan/intellyrouter/internal/store"
)

// Usage is what the gateway ledger recorded for one Claude Code session.
type Usage struct {
	Requests             int
	EscalatedRequests    int
	CostUSD              float64
	SubscriptionValueUSD float64
	APITokens            int64
	SubscriptionTokens   int64
}

// Ledger reads gateway usage by Claude Code session ID.
type Ledger interface {
	SessionUsage(ctx context.Context, sessionID string) (Usage, error)
}

type legUsage struct {
	Role             string `json:"role"`
	Billing          string `json:"billing"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`
}

func (u *Usage) add(cost, subscriptionValue float64, legs []legUsage) {
	u.Requests++
	u.CostUSD += cost
	u.SubscriptionValueUSD += subscriptionValue
	escalated := false
	for _, l := range legs {
		tokens := l.InputTokens + l.OutputTokens + l.CacheReadTokens + l.CacheWriteTokens
		switch {
		case l.Billing == ledger.BillingSubscription:
			u.SubscriptionTokens += tokens
		case l.Role != ledger.RoleClassifier:
			u.APITokens += tokens
		}
		escalated = escalated || l.Role == ledger.RoleEscalation
	}
	if escalated {
		u.EscalatedRequests++
	}
}

// StoreLedger reads usage from the gateway database in the same process.
type StoreLedger struct {
	Store *store.Store
}

func (l StoreLedger) SessionUsage(ctx context.Context, sessionID string) (Usage, error) {
	items, _, err := l.Store.ListRequests(ctx, store.RequestFilter{SessionID: sessionID, Limit: 500})
	if err != nil {
		return Usage{}, err
	}
	var u Usage
	for _, item := range items {
		req, err := l.Store.GetRequest(ctx, item.ID)
		if err != nil {
			return u, err
		}
		legs := make([]legUsage, 0, len(req.Legs))
		for _, leg := range req.Legs {
			legs = append(legs, legUsage{leg.Role, leg.Billing, leg.InputTokens, leg.OutputTokens, leg.CacheReadTokens, leg.CacheWriteTokens})
		}
		u.add(req.CostUSD, req.SubscriptionValueUSD, legs)
	}
	return u, nil
}

// AdminLedger reads usage through a running gateway's admin API.
type AdminLedger struct {
	Gateway string
	Token   string
	HTTP    *http.Client
}

func (l AdminLedger) SessionUsage(ctx context.Context, sessionID string) (Usage, error) {
	var list struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	if err := l.get(ctx, "/api/admin/requests?limit=500&session_id="+url.QueryEscape(sessionID), &list); err != nil {
		return Usage{}, err
	}
	var u Usage
	for _, item := range list.Items {
		var req struct {
			CostUSD              float64    `json:"cost_usd"`
			SubscriptionValueUSD float64    `json:"subscription_value_usd"`
			Legs                 []legUsage `json:"legs"`
		}
		if err := l.get(ctx, fmt.Sprintf("/api/admin/requests/%d", item.ID), &req); err != nil {
			return u, err
		}
		u.add(req.CostUSD, req.SubscriptionValueUSD, req.Legs)
	}
	return u, nil
}

// Routes returns the names of the routes the gateway serves.
func (l AdminLedger) Routes(ctx context.Context) ([]string, error) {
	var routes []struct {
		Name string `json:"name"`
	}
	if err := l.get(ctx, "/api/admin/routes", &routes); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(routes))
	for _, r := range routes {
		names = append(names, r.Name)
	}
	return names, nil
}

func (l AdminLedger) get(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(l.Gateway, "/")+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+l.Token)
	resp, err := l.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s: %s", path, resp.Status, tail(string(body), 300))
	}
	return json.Unmarshal(body, v)
}
