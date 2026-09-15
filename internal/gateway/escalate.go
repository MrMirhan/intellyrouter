package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"intellyrouter/internal/escalate"
	"intellyrouter/internal/ledger"
	"intellyrouter/internal/provider"
	"intellyrouter/internal/store"
	"intellyrouter/internal/translate"
)

const classifyTimeout = 20 * time.Second

// escalate serves the request with the tier the decider picks for its turn.
func (s *Server) escalate(w http.ResponseWriter, r *http.Request, route store.Route, cr clientRequest, e *ledger.Entry) {
	fail := func(status int, typ, msg string) {
		e.Finish(ledger.StatusError, status, msg)
		writeError(w, status, typ, msg)
	}
	rules, err := escalate.ParseRules(route.Settings)
	if err != nil {
		fail(http.StatusInternalServerError, "api_error", fmt.Sprintf("route %s has invalid settings: %v", route.Name, err))
		return
	}
	if len(route.Tiers) < 2 {
		fail(http.StatusServiceUnavailable, "api_error", fmt.Sprintf("route %s needs at least two models", route.Name))
		return
	}
	turn, err := escalate.Analyze(cr.body)
	if err != nil {
		fail(http.StatusBadRequest, "invalid_request_error", "cannot read messages: "+err.Error())
		return
	}

	labels := make([]string, len(route.Tiers))
	for i, t := range route.Tiers {
		labels[i] = t.Label
	}
	classify := func(ctx context.Context) (bool, string, error) {
		up, why, leg, err := s.classify(ctx, rules.Classifier.ModelID, turn, cr.capture)
		if leg.Model != "" {
			leg.Role = ledger.RoleClassifier
			e.Legs = append(e.Legs, leg)
		}
		return up, why, err
	}
	key := e.SessionID + "\x00" + e.AgentID + "\x00" + turn.Key
	dec := s.decider.Decide(r.Context(), key, turn, labels, rules, classify)

	sizeKey := e.SessionID + "\x00" + e.AgentID
	tierIndex, fitNote, err := s.fittingTier(r.Context(), route.Tiers, dec.Tier, s.sizes.estimate(sizeKey, len(cr.body)))
	if err != nil {
		fail(http.StatusServiceUnavailable, "api_error", "route models unavailable: "+err.Error())
		return
	}
	t, err := s.resolve(r.Context(), route.Tiers[tierIndex].ModelID)
	if err != nil {
		fail(http.StatusServiceUnavailable, "api_error", fmt.Sprintf("tier %s unavailable: %v", labels[tierIndex], err))
		return
	}
	// Savings compare base-tier tokens with the route's strongest model.
	if top, err := s.resolve(r.Context(), route.Tiers[len(route.Tiers)-1].ModelID); err == nil {
		ref := top.price()
		e.Reference = &ref
	}

	legs := s.callWithFallback(w, r, route.Tiers, tierIndex, t, sizeKey, func(target) clientRequest { return cr })
	note := fmt.Sprintf("tier %s: %s", labels[tierIndex], dec.Reason)
	if fitNote != "" {
		note += "; " + fitNote
	}
	legs[0].Note = joinNote(note, legs[0].Note)
	for i := range legs {
		legs[i].Role = ledger.RoleExecutor
		if tierIndex > 0 || i > 0 {
			legs[i].Role = ledger.RoleEscalation
		}
	}
	e.Legs = append(e.Legs, legs...)
	last := legs[len(legs)-1]
	e.Finish(last.Status, last.HTTPStatus, last.Error)
}

// classify asks the route's classifier model about the turn. The gateway
// makes this call itself, so it never uses a Claude subscription login.
func (s *Server) classify(ctx context.Context, modelID int64, turn escalate.Turn, capture bool) (bool, string, ledger.Leg, error) {
	ctx, cancel := context.WithTimeout(ctx, classifyTimeout)
	defer cancel()
	t, err := s.resolve(ctx, modelID)
	if err != nil {
		return false, "", ledger.Leg{}, fmt.Errorf("classifier model: %w", err)
	}
	if t.config.Type == provider.AnthropicSubscription {
		return false, "", ledger.Leg{}, errors.New("classifier cannot use a subscription provider")
	}
	leg := t.newLeg()
	start := time.Now()
	message, usage, err := s.complete(ctx, t, escalate.ClassifierRequest(t.model.ModelID, turn))
	leg.Latency, leg.Usage = time.Since(start), usage
	if capture {
		leg.Output = message
	}
	if err != nil {
		leg.Status, leg.Error = ledger.StatusError, err.Error()
		return false, "", leg, err
	}
	leg.Status = ledger.StatusOK
	up, why, err := escalate.ParseVerdict(message)
	if err != nil {
		leg.Error = err.Error()
	}
	return up, why, leg, err
}

// complete makes a non-streaming call that the gateway originates and returns
// the answer as an Anthropic message body.
func (s *Server) complete(ctx context.Context, t target, body []byte) ([]byte, ledger.Usage, error) {
	openAI := t.config.Type.Format() == provider.FormatOpenAI
	var req *http.Request
	var err error
	if openAI {
		var upBody []byte
		upBody, err = translate.Request(body, translate.Options{Model: t.model.ModelID, MaxTokensField: t.config.Type.MaxTokensField()})
		if err == nil {
			req, err = provider.NewOpenAIRequest(ctx, t.config, upBody, false)
		}
	} else {
		req, err = provider.NewAnthropicRequest(ctx, t.config, "/v1/messages", "", body, nil, "")
	}
	if err != nil {
		return nil, ledger.Usage{}, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, ledger.Usage{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, ledger.Usage{}, err
	}
	if openAI {
		if resp.StatusCode != http.StatusOK {
			raw = translate.Error(resp.StatusCode, raw)
		} else if raw, err = translate.Response(raw, t.model.ModelID); err != nil {
			return nil, ledger.Usage{}, err
		}
	}
	var tr ledger.AnthropicTracker
	tr.Response(resp.StatusCode, raw)
	if resp.StatusCode != http.StatusOK {
		return nil, tr.Usage, fmt.Errorf("upstream returned %d: %s", resp.StatusCode, tr.Error)
	}
	return raw, tr.Usage, nil
}
