package gateway

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/MrMirhan/intellyrouter/internal/ledger"
	"github.com/MrMirhan/intellyrouter/internal/provider"
	"github.com/MrMirhan/intellyrouter/internal/store"
)

// comboTarget is a combo's strategy and its available member models.
type comboTarget struct {
	strategy string
	members  []target
}

// subscription reports whether a request to t can reach a Claude subscription.
// The gateway then must not change the request or continue it on its own.
func (t target) subscription() bool {
	if t.config.Type == provider.AnthropicSubscription {
		return true
	}
	return t.combo != nil && slices.ContainsFunc(t.combo.members, func(m target) bool {
		return m.config.Type == provider.AnthropicSubscription
	})
}

// comboBalancer keeps, per combo, the next member for round-robin and the
// requests each member is serving for least-used.
type comboBalancer struct {
	mu       sync.Mutex
	next     map[int64]int
	inFlight map[int64]map[int64]int
}

func newComboBalancer() *comboBalancer {
	return &comboBalancer{next: map[int64]int{}, inFlight: map[int64]map[int64]int{}}
}

// order returns the member indexes in the order a request tries them.
func (b *comboBalancer) order(t target) []int {
	c := t.combo
	order := make([]int, len(c.members))
	for i := range order {
		order[i] = i
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	switch c.strategy {
	case store.ComboRoundRobin:
		start := b.next[t.model.ID] % len(order)
		b.next[t.model.ID] = start + 1
		for i := range order {
			order[i] = (start + i) % len(order)
		}
	case store.ComboLeastUsed:
		load := b.inFlight[t.model.ID]
		slices.SortStableFunc(order, func(x, y int) int {
			return cmp.Compare(load[c.members[x].model.ID], load[c.members[y].model.ID])
		})
	}
	return order
}

// start counts a request on a member until the returned function runs.
func (b *comboBalancer) start(combo, member int64) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.inFlight[combo] == nil {
		b.inFlight[combo] = map[int64]int{}
	}
	b.inFlight[combo][member]++
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.inFlight[combo][member]--
	}
}

func (s *Server) resolveCombo(ctx context.Context, p store.Provider, m store.Model) (target, error) {
	c, err := s.store.GetCombo(ctx, m.ID)
	if err != nil {
		return target{}, err
	}
	ct := &comboTarget{strategy: c.Strategy}
	for _, id := range c.Members {
		if member, err := s.resolveModel(ctx, id, false); err == nil {
			ct.members = append(ct.members, member)
		}
	}
	if len(ct.members) == 0 {
		return target{}, fmt.Errorf("combo %s has no available model", m.ModelID)
	}
	return target{provider: p, model: m, config: provider.ConfigFor(p), combo: ct}, nil
}

// callCombo sends the request to the combo's members in the strategy's order.
// When a member fails before its response starts, the next member gets the
// request, so the client sees the first answer or the last member's error.
func (s *Server) callCombo(w http.ResponseWriter, r *http.Request, t target, cr clientRequest) ledger.Leg {
	var tries []target
	for _, i := range s.combos.order(t) {
		if m := t.combo.members[i]; m.config.Type != provider.AnthropicSubscription || cr.claudeAuth != "" {
			tries = append(tries, m)
		}
	}
	if len(tries) == 0 {
		writeError(w, http.StatusUnauthorized, "authentication_error", errNoClaudeLogin)
		leg := t.newLeg()
		leg.Status, leg.HTTPStatus, leg.Error = ledger.StatusError, http.StatusUnauthorized, errNoClaudeLogin
		return leg
	}
	var failed []string
	note := func(leg ledger.Leg) ledger.Leg {
		n := "combo " + t.model.ModelID
		if len(failed) > 0 {
			n += " after " + strings.Join(failed, ", ")
		}
		leg.Note = joinNote(n, leg.Note)
		return leg
	}
	for _, m := range tries[:len(tries)-1] {
		held := newErrorBuffer(w)
		done := s.combos.start(t.model.ID, m.model.ID)
		leg := s.call(held, r, m, cr)
		done()
		if !held.retryable() || r.Context().Err() != nil {
			held.release()
			return note(leg)
		}
		failed = append(failed, fmt.Sprintf("%s returned %d", m.model.ModelID, held.status))
	}
	last := tries[len(tries)-1]
	done := s.combos.start(t.model.ID, last.model.ID)
	defer done()
	return note(s.call(w, r, last, cr))
}

// completeCombo makes a gateway call through a combo and returns the member
// that answered. The gateway never uses a subscription login for its own
// calls, so it skips subscription members.
func (s *Server) completeCombo(ctx context.Context, t target, body []byte) ([]byte, ledger.Usage, target, error) {
	var errs []error
	for _, i := range s.combos.order(t) {
		m := t.combo.members[i]
		if m.config.Type == provider.AnthropicSubscription {
			continue
		}
		req, err := withModel(body, m.model.ModelID)
		if err != nil {
			return nil, ledger.Usage{}, m, err
		}
		done := s.combos.start(t.model.ID, m.model.ID)
		out, usage, _, err := s.complete(ctx, m, req)
		done()
		if err == nil {
			return out, usage, m, nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", m.model.ModelID, err))
		if ctx.Err() != nil {
			break
		}
	}
	if len(errs) == 0 {
		return nil, ledger.Usage{}, t, fmt.Errorf("combo %s has no model the gateway can call on its own", t.model.ModelID)
	}
	return nil, ledger.Usage{}, t, errors.Join(errs...)
}

// directRoute serves a model named "<provider slug>/<model id>" without a
// configured route, as a direct route to that model. Combos use the slug of
// the combo provider, "combo".
func (s *Server) directRoute(ctx context.Context, name string) (store.Route, bool) {
	slug, modelID, ok := strings.Cut(name, "/")
	if !ok || slug == "" || modelID == "" {
		return store.Route{}, false
	}
	p, err := s.store.ProviderBySlug(ctx, slug)
	if err != nil {
		return store.Route{}, false
	}
	m, err := s.store.ModelByProvider(ctx, p.ID, modelID)
	if err != nil {
		return store.Route{}, false
	}
	return store.Route{Name: name, Strategy: store.StrategyDirect, Settings: "{}", Tiers: []store.Tier{{ModelID: m.ID, Label: modelID}}}, true
}
