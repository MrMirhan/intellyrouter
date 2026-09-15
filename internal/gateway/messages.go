package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"intellyrouter/internal/jsonbytes"
	"intellyrouter/internal/ledger"
	"intellyrouter/internal/provider"
	"intellyrouter/internal/store"
)

const maxBodyBytes = 64 << 20

const errNoClaudeLogin = "this route uses a Claude subscription: log in with /login in Claude Code and send the gateway key in the x-intelly-key header"

type requestMeta struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

// clientRequest is an authenticated, parsed /v1/messages call.
type clientRequest struct {
	body       []byte
	stream     bool
	claudeAuth string
	// capture keeps the output of each leg for the ledger.
	capture bool
	// advisor is the route's advisor model, when it has one.
	advisor *routeAdvisor
}

var errDisabled = errors.New("model or provider is disabled")

type target struct {
	provider store.Provider
	model    store.Model
	config   provider.Config
	// combo is set when the model is a combo of other models.
	combo *comboTarget
}

func (t target) price() ledger.Price {
	if t.combo != nil {
		return t.combo.members[0].price()
	}
	p := ledger.Price{In: t.model.PriceIn, Out: t.model.PriceOut, CacheRead: t.model.PriceCacheRead, CacheWrite: t.model.PriceCacheWrite}
	if p == (ledger.Price{}) {
		if builtin, ok := ledger.BuiltinPrice(t.model.ModelID); ok {
			return builtin
		}
	}
	return p
}

func (t target) newLeg() ledger.Leg {
	leg := ledger.Leg{Provider: t.provider.Name, Model: t.model.ModelID, Price: t.price(), Billing: ledger.BillingAPI}
	if t.config.Type == provider.AnthropicSubscription {
		leg.Billing = ledger.BillingSubscription
	}
	return leg
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	e := ledger.Entry{
		Started:   time.Now(),
		SessionID: r.Header.Get("X-Claude-Code-Session-Id"),
		AgentID:   r.Header.Get("X-Claude-Code-Agent-Id"),
	}
	claudeAuth, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	body, meta, ok := readRequest(w, r)
	if !ok {
		return
	}
	route, ok := s.route(w, r, meta.Model)
	if !ok {
		return
	}
	e.Route, e.Strategy, e.ClientModel, e.Stream = route.Name, route.Strategy, meta.Model, meta.Stream
	cr := clientRequest{body: body, stream: meta.Stream, claudeAuth: claudeAuth, capture: s.captureContent(r.Context())}
	if cr.capture {
		e.Input = body
	}
	adv, off := s.loadRouteAdvisor(r.Context(), route)
	if out, err := applyRouteAdvisor(adv, off, cr.body); err == nil {
		cr.body = out
	} else {
		s.log.Warn("apply route advisor", "route", route.Name, "err", err)
	}
	cr.advisor = adv

	switch route.Strategy {
	case store.StrategyDirect:
		s.direct(w, r, route, cr, &e)
	case store.StrategyEscalate:
		s.escalate(w, r, route, cr, &e)
	case store.StrategyGuided:
		s.guided(w, r, route, cr, &e)
	default:
		writeError(w, http.StatusNotImplemented, "api_error", fmt.Sprintf("strategy %q is not implemented yet", route.Strategy))
		return
	}
	s.ledger.Record(context.WithoutCancel(r.Context()), e)
}

func (s *Server) direct(w http.ResponseWriter, r *http.Request, route store.Route, cr clientRequest, e *ledger.Entry) {
	t, err := s.resolve(r.Context(), baseModel(route))
	if err != nil {
		e.Finish(ledger.StatusError, http.StatusServiceUnavailable, "route model unavailable: "+err.Error())
		writeError(w, http.StatusServiceUnavailable, "api_error", e.Error)
		return
	}
	if s.runWithAdvisor(w, r, e, cr, t, "", ledger.RoleDirect, "") {
		return
	}
	leg := s.call(w, r, t, cr)
	leg.Role = ledger.RoleDirect
	e.Legs = append(e.Legs, leg)
	e.Finish(leg.Status, leg.HTTPStatus, leg.Error)
}

// call sends the client request to one model in its provider's format and
// relays the answer. It writes the client response in every case.
func (s *Server) call(w http.ResponseWriter, r *http.Request, t target, cr clientRequest) ledger.Leg {
	fail := func(status int, typ, msg string) ledger.Leg {
		writeError(w, status, typ, msg)
		leg := t.newLeg()
		leg.Status, leg.HTTPStatus, leg.Error = ledger.StatusError, status, msg
		return leg
	}
	if t.combo != nil {
		return s.callCombo(w, r, t, cr)
	}
	if t.config.Type == provider.AnthropicSubscription && cr.claudeAuth == "" {
		return fail(http.StatusUnauthorized, "authentication_error", errNoClaudeLogin)
	}
	if t.config.Type.Format() == provider.FormatOpenAI {
		return s.forwardOpenAI(w, r, t, cr.body, cr.stream, cr.capture)
	}
	upstream, err := withModel(cr.body, t.model.ModelID)
	if err != nil {
		return fail(http.StatusBadRequest, "invalid_request_error", "invalid request body: "+err.Error())
	}
	upstream, droppedAdvisor, err := dropAdvisorTools(t, upstream)
	if err != nil {
		return fail(http.StatusBadRequest, "invalid_request_error", "invalid request body: "+err.Error())
	}
	leg := s.forward(w, r, t, upstream, cr.claudeAuth, cr.capture)
	if droppedAdvisor {
		leg.Note = joinNote("advisor tool removed for "+t.model.ModelID, leg.Note)
	}
	return leg
}

func baseModel(r store.Route) int64 {
	if len(r.Tiers) == 0 {
		return 0
	}
	return r.Tiers[0].ModelID
}

func (s *Server) handleCountTokens(w http.ResponseWriter, r *http.Request) {
	claudeAuth, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	body, meta, ok := readRequest(w, r)
	if !ok {
		return
	}
	route, ok := s.route(w, r, meta.Model)
	if !ok {
		return
	}
	t, err := s.resolve(r.Context(), baseModel(route))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "api_error", "route model unavailable: "+err.Error())
		return
	}
	if t.combo != nil {
		t = t.combo.members[0]
	}
	if t.config.Type.Format() != provider.FormatAnthropic {
		// Claude Code falls back to its own estimate when counting is unavailable.
		writeError(w, http.StatusNotFound, "not_found_error", "token counting is not available for this route")
		return
	}
	if t.config.Type == provider.AnthropicSubscription && claudeAuth == "" {
		writeError(w, http.StatusUnauthorized, "authentication_error", errNoClaudeLogin)
		return
	}
	upstream, err := withModel(body, t.model.ModelID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "invalid request body: "+err.Error())
		return
	}
	if upstream, _, err = dropAdvisorTools(t, upstream); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "invalid request body: "+err.Error())
		return
	}
	s.forward(w, r, t, upstream, claudeAuth, false)
}

func readRequest(w http.ResponseWriter, r *http.Request) ([]byte, requestMeta, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body is too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid_request_error", "cannot read request body")
		}
		return nil, requestMeta{}, false
	}
	var meta requestMeta
	if err := json.Unmarshal(body, &meta); err != nil || meta.Model == "" {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "body must be a JSON object with a model field")
		return nil, requestMeta{}, false
	}
	return body, meta, true
}

func (s *Server) route(w http.ResponseWriter, r *http.Request, name string) (store.Route, bool) {
	route, err := s.store.RouteByName(r.Context(), name)
	if errors.Is(err, store.ErrNotFound) {
		if direct, ok := s.directRoute(r.Context(), name); ok {
			return direct, true
		}
		if fallback, ok, ferr := s.store.Setting(r.Context(), store.FallbackRouteSetting); ferr == nil && ok && fallback != "" {
			route, err = s.store.RouteByName(r.Context(), fallback)
		}
	}
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found_error", fmt.Sprintf("model %q is not a configured route", name))
		return store.Route{}, false
	}
	if err != nil {
		s.internalError(w, "load route", err)
		return store.Route{}, false
	}
	return route, true
}

func (s *Server) resolve(ctx context.Context, modelID int64) (target, error) {
	return s.resolveModel(ctx, modelID, true)
}

// resolveModel loads a model and its provider; combos says whether the model
// may be a combo.
func (s *Server) resolveModel(ctx context.Context, modelID int64, combos bool) (target, error) {
	m, err := s.store.GetModel(ctx, modelID)
	if err != nil {
		return target{}, err
	}
	p, err := s.store.GetProvider(ctx, m.ProviderID)
	if err != nil {
		return target{}, err
	}
	if !m.Enabled || !p.Enabled {
		return target{}, errDisabled
	}
	if p.Type == store.ComboProviderType {
		if !combos {
			return target{}, errors.New("a combo cannot contain another combo")
		}
		return s.resolveCombo(ctx, p, m)
	}
	return target{provider: p, model: m, config: provider.ConfigFor(p)}, nil
}

func withModel(body []byte, model string) ([]byte, error) {
	v, err := json.Marshal(model)
	if err != nil {
		return nil, err
	}
	return jsonbytes.SetField(body, "model", v)
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticate(w, r); !ok {
		return
	}
	routes, err := s.store.ListRoutes(r.Context())
	if err != nil {
		s.internalError(w, "list routes", err)
		return
	}
	type model struct {
		Type        string `json:"type"`
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
		CreatedAt   string `json:"created_at"`
	}
	resp := struct {
		Data    []model `json:"data"`
		HasMore bool    `json:"has_more"`
		FirstID *string `json:"first_id"`
		LastID  *string `json:"last_id"`
	}{Data: []model{}}
	for _, rt := range routes {
		resp.Data = append(resp.Data, model{
			Type:        "model",
			ID:          rt.Name,
			DisplayName: rt.Name,
			CreatedAt:   time.UnixMilli(rt.CreatedAt).UTC().Format(time.RFC3339),
		})
	}
	// Models and combos can also be used directly as "<provider slug>/<model id>".
	providers, err := s.store.ListProviders(r.Context())
	if err != nil {
		s.internalError(w, "list providers", err)
		return
	}
	models, err := s.store.ListModels(r.Context(), 0)
	if err != nil {
		s.internalError(w, "list models", err)
		return
	}
	byID := make(map[int64]store.Provider, len(providers))
	for _, p := range providers {
		byID[p.ID] = p
	}
	for _, m := range models {
		if p := byID[m.ProviderID]; m.Enabled && p.Enabled && p.Slug != "" {
			name := p.Slug + "/" + m.ModelID
			resp.Data = append(resp.Data, model{Type: "model", ID: name, DisplayName: name, CreatedAt: time.UnixMilli(p.CreatedAt).UTC().Format(time.RFC3339)})
		}
	}
	if n := len(resp.Data); n > 0 {
		resp.FirstID, resp.LastID = &resp.Data[0].ID, &resp.Data[n-1].ID
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
