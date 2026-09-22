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

	"github.com/MrMirhan/intellyrouter/internal/guided"
	"github.com/MrMirhan/intellyrouter/internal/ledger"
	"github.com/MrMirhan/intellyrouter/internal/provider"
	"github.com/MrMirhan/intellyrouter/internal/store"
)

// comboMember is one model in a combo and its share of the traffic.
type comboMember struct {
	target
	weight int
}

// comboTarget is a combo's strategy and its available member models.
type comboTarget struct {
	strategy string
	members  []comboMember
}

// subscription reports whether a request to t can reach a Claude subscription.
// The gateway then must not change the request or continue it on its own.
func (t target) subscription() bool {
	if t.config.Type == provider.AnthropicSubscription {
		return true
	}
	return t.combo != nil && slices.ContainsFunc(t.combo.members, func(m comboMember) bool {
		return m.config.Type == provider.AnthropicSubscription
	})
}

// comboBalancer keeps, per combo, the round-robin credit each member has built
// up and the requests each member is serving for least-used.
type comboBalancer struct {
	mu       sync.Mutex
	credit   map[int64][]int
	inFlight map[int64]map[int64]int
}

func newComboBalancer() *comboBalancer {
	return &comboBalancer{credit: map[int64][]int{}, inFlight: map[int64]map[int64]int{}}
}

// order returns the member indexes in the order a request tries them.
func (b *comboBalancer) order(t target) []int {
	c := t.combo
	order := make([]int, 0, len(c.members))
	b.mu.Lock()
	defer b.mu.Unlock()
	switch c.strategy {
	case store.ComboRoundRobin:
		// Only the member that serves the request follows the weights. The
		// rest keep their configured order, as the list a failure falls through.
		head := b.pick(t.model.ID, c.members)
		order = append(order, head)
		for i := range c.members {
			if i != head {
				order = append(order, i)
			}
		}
	case store.ComboLeastUsed:
		for i := range c.members {
			order = append(order, i)
		}
		load := b.inFlight[t.model.ID]
		slices.SortStableFunc(order, func(x, y int) int {
			// Load per unit of weight, cross-multiplied to stay in integers.
			return cmp.Compare(load[c.members[x].model.ID]*c.members[y].weight,
				load[c.members[y].model.ID]*c.members[x].weight)
		})
	default:
		for i := range c.members {
			order = append(order, i)
		}
	}
	return order
}

// pick chooses a member by smooth weighted round-robin: every call adds each
// member's weight to its credit, hands the turn to the member holding the most,
// and charges that member the whole round. Weights are then spread through the
// sequence instead of arriving in bursts.
func (b *comboBalancer) pick(combo int64, members []comboMember) int {
	credit := b.credit[combo]
	if len(credit) != len(members) {
		credit = make([]int, len(members))
	}
	total, best := 0, 0
	for i, m := range members {
		credit[i] += m.weight
		total += m.weight
		if credit[i] > credit[best] {
			best = i
		}
	}
	credit[best] -= total
	b.credit[combo] = credit
	return best
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
	for _, m := range c.Members {
		if member, err := s.resolveModel(ctx, m.ModelID, false); err == nil {
			ct.members = append(ct.members, comboMember{target: member, weight: max(m.Weight, 1)})
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
	var tries, seeing []target
	for _, i := range s.combos.order(t) {
		m := t.combo.members[i]
		if m.config.Type == provider.AnthropicSubscription && cr.claudeAuth == "" {
			continue
		}
		tries = append(tries, m.target)
		if m.model.Vision {
			seeing = append(seeing, m.target)
		}
	}
	// A combo counts as taking images when any one member does, so a request
	// that carries one skips the members that would only reject it. When no
	// member reads images the combo still tries them all: a tier chose this
	// combo, and the upstream error says more than a refusal here would.
	blind := 0
	if guided.HasImage(cr.body) && len(seeing) > 0 {
		blind, tries = len(tries)-len(seeing), seeing
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
		if blind > 0 {
			n += fmt.Sprintf("; request has an image, skipped %d that do not read one", blind)
		}
		if len(failed) > 0 {
			n += " after " + strings.Join(failed, ", ")
		}
		leg.Note = joinNote(n, leg.Note)
		return leg
	}
	for _, m := range tries[:len(tries)-1] {
		held := newErrorBuffer(w)
		// This member can fail over, so a slow upstream that holds the buffer
		// with no visible content must not keep the client waiting.
		held.armHoldWatchdog()
		done := s.combos.start(t.model.ID, m.model.ID)
		leg := s.call(held, r, m, cr)
		done()
		if !held.retryable() || r.Context().Err() != nil {
			held.release()
			return note(leg)
		}
		failed = append(failed, fmt.Sprintf("%s returned %d", m.model.ModelID, held.status))
	}
	// The last member has nobody to fail over to, but its response still goes
	// through a buffer: a stream that never produces content reaches the client
	// as a plain error instead of a 200 it cannot parse. releaseOnContent hands
	// the buffer over as soon as the first content delta arrives, so a healthy
	// answer still streams.
	last := tries[len(tries)-1]
	held := newErrorBuffer(w)
	done := s.combos.start(t.model.ID, last.model.ID)
	leg := s.call(held, r, last, cr)
	done()
	held.release()
	return note(leg)
}

// completeCombo makes a gateway call through a combo and returns the member
// that answered. The gateway never uses a subscription login for its own
// calls, so it skips subscription members.
func (s *Server) completeCombo(ctx context.Context, t target, body []byte) ([]byte, ledger.Usage, target, error) {
	var errs []error
	for _, i := range s.combos.order(t) {
		m := t.combo.members[i].target
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
