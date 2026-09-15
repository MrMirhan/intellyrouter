package eval

import (
	"context"

	"intellyrouter/internal/guided"
	"intellyrouter/internal/provider"
	"intellyrouter/internal/store"
)

// routeNeedsLogin reports whether a route sends requests to a Claude
// subscription model, which works only with a Claude Code login.
func routeNeedsLogin(ctx context.Context, st *store.Store, rt store.Route) (bool, error) {
	ids := make([]int64, 0, len(rt.Tiers)+1)
	for _, t := range rt.Tiers {
		ids = append(ids, t.ModelID)
	}
	if rt.Strategy == store.StrategyGuided {
		if s, err := guided.ParseSettings(rt.Settings); err == nil {
			ids = append(ids, s.Director.ModelID)
		}
	}
	for _, id := range ids {
		m, err := st.GetModel(ctx, id)
		if err != nil {
			return false, err
		}
		p, err := st.GetProvider(ctx, m.ProviderID)
		if err != nil {
			return false, err
		}
		if provider.Type(p.Type) == provider.AnthropicSubscription {
			return true, nil
		}
	}
	return false, nil
}
