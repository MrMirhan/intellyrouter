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

type requestMeta struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

var errDisabled = errors.New("model or provider is disabled")

type target struct {
	provider store.Provider
	model    store.Model
	config   provider.Config
}

func (t target) price() ledger.Price {
	p := ledger.Price{In: t.model.PriceIn, Out: t.model.PriceOut, CacheRead: t.model.PriceCacheRead, CacheWrite: t.model.PriceCacheWrite}
	if p == (ledger.Price{}) {
		if builtin, ok := ledger.BuiltinPrice(t.model.ModelID); ok {
			return builtin
		}
	}
	return p
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	e := ledger.Entry{
		Started:   time.Now(),
		SessionID: r.Header.Get("X-Claude-Code-Session-Id"),
		AgentID:   r.Header.Get("X-Claude-Code-Agent-Id"),
		AuthMode:  ledger.AuthKey,
	}
	if !s.authenticate(w, r) {
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

	switch route.Strategy {
	case store.StrategyDirect:
		s.direct(w, r, route, body, meta.Stream, &e)
	default:
		writeError(w, http.StatusNotImplemented, "api_error", fmt.Sprintf("strategy %q is not implemented yet", route.Strategy))
		return
	}
	s.ledger.Record(context.WithoutCancel(r.Context()), e)
}

func (s *Server) direct(w http.ResponseWriter, r *http.Request, route store.Route, body []byte, stream bool, e *ledger.Entry) {
	t, err := s.resolve(r.Context(), baseModel(route))
	if err != nil {
		e.Finish(ledger.StatusError, http.StatusServiceUnavailable, "route model unavailable: "+err.Error())
		writeError(w, http.StatusServiceUnavailable, "api_error", e.Error)
		return
	}
	leg := s.call(w, r, t, body, stream)
	leg.Role = ledger.RoleDirect
	e.Legs = append(e.Legs, leg)
	e.Finish(leg.Status, leg.HTTPStatus, leg.Error)
}

// call sends the client request to one model in its provider's format and
// relays the answer. It writes the client response in every case.
func (s *Server) call(w http.ResponseWriter, r *http.Request, t target, body []byte, stream bool) ledger.Leg {
	if t.config.Type.Format() == provider.FormatOpenAI {
		return s.forwardOpenAI(w, r, t, body, stream)
	}
	upstream, err := withModel(body, t.model.ModelID)
	if err != nil {
		msg := "invalid request body: " + err.Error()
		writeError(w, http.StatusBadRequest, "invalid_request_error", msg)
		return ledger.Leg{Provider: t.provider.Name, Model: t.model.ModelID, Status: ledger.StatusError, HTTPStatus: http.StatusBadRequest, Error: msg}
	}
	return s.forward(w, r, t, upstream)
}

func baseModel(r store.Route) int64 {
	if len(r.Tiers) == 0 {
		return 0
	}
	return r.Tiers[0].ModelID
}

func (s *Server) handleCountTokens(w http.ResponseWriter, r *http.Request) {
	if !s.authenticate(w, r) {
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
	if t.config.Type.Format() != provider.FormatAnthropic {
		// Claude Code falls back to its own estimate when counting is unavailable.
		writeError(w, http.StatusNotFound, "not_found_error", "token counting is not available for this route")
		return
	}
	upstream, err := withModel(body, t.model.ModelID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "invalid request body: "+err.Error())
		return
	}
	s.forward(w, r, t, upstream)
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
	if !s.authenticate(w, r) {
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
	if n := len(resp.Data); n > 0 {
		resp.FirstID, resp.LastID = &resp.Data[0].ID, &resp.Data[n-1].ID
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
