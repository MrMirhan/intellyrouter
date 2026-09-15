package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"intellyrouter/internal/escalate"
	"intellyrouter/internal/guided"
	"intellyrouter/internal/provider"
	"intellyrouter/internal/store"
)

type tierJSON struct {
	ModelID int64  `json:"model_id"`
	Label   string `json:"label"`
}

type routeJSON struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Strategy  string          `json:"strategy"`
	Tiers     []tierJSON      `json:"tiers"`
	Settings  json.RawMessage `json:"settings"`
	CreatedAt int64           `json:"created_at"`
}

func toRouteJSON(r store.Route) routeJSON {
	out := routeJSON{
		ID: r.ID, Name: r.Name, Strategy: r.Strategy, Tiers: make([]tierJSON, 0, len(r.Tiers)),
		Settings: json.RawMessage(r.Settings), CreatedAt: r.CreatedAt,
	}
	for _, t := range r.Tiers {
		out.Tiers = append(out.Tiers, tierJSON{ModelID: t.ModelID, Label: t.Label})
	}
	return out
}

type routeInput struct {
	Name     *string         `json:"name"`
	Strategy *string         `json:"strategy"`
	Tiers    []tierJSON      `json:"tiers"`
	Settings json.RawMessage `json:"settings"`
}

func (in routeInput) apply(r *store.Route) {
	if in.Name != nil {
		r.Name = strings.TrimSpace(*in.Name)
	}
	if in.Strategy != nil {
		r.Strategy = *in.Strategy
	}
	if in.Tiers != nil {
		r.Tiers = make([]store.Tier, 0, len(in.Tiers))
		for _, t := range in.Tiers {
			r.Tiers = append(r.Tiers, store.Tier{ModelID: t.ModelID, Label: strings.ToLower(strings.TrimSpace(t.Label))})
		}
	}
	if in.Settings != nil {
		r.Settings = string(in.Settings)
	}
}

var labelPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// normalizeRoute validates the route and fills empty tier labels from model IDs.
func (a *API) normalizeRoute(ctx context.Context, r *store.Route) error {
	if r.Name == "" || strings.ContainsAny(r.Name, " \t\r\n") {
		return errors.New("name is required and cannot contain whitespace")
	}
	switch r.Strategy {
	case store.StrategyDirect:
		if len(r.Tiers) != 1 {
			return errors.New("a direct route needs exactly one model")
		}
	case store.StrategyEscalate:
		if len(r.Tiers) < 2 {
			return errors.New("an escalate route needs a base model and at least one escalation model")
		}
	case store.StrategyGuided:
		if len(r.Tiers) < 1 {
			return errors.New("a guided route needs at least one executor model")
		}
	default:
		return fmt.Errorf("unknown strategy %q", r.Strategy)
	}
	seen := make(map[string]bool)
	for i := range r.Tiers {
		t := &r.Tiers[i]
		m, err := a.store.GetModel(ctx, t.ModelID)
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("tier %d: model %d does not exist", i+1, t.ModelID)
		}
		if err != nil {
			return err
		}
		if !m.Enabled {
			return fmt.Errorf("tier %d: model %s is disabled", i+1, m.ModelID)
		}
		if t.Label == "" {
			t.Label = defaultLabel(m.ModelID)
		}
		if !labelPattern.MatchString(t.Label) {
			return fmt.Errorf("tier %d: label %q may only contain a-z, 0-9, '.', '_' and '-'", i+1, t.Label)
		}
		if seen[t.Label] {
			return fmt.Errorf("tier label %q is used more than once", t.Label)
		}
		seen[t.Label] = true
	}
	var settings map[string]any
	if err := json.Unmarshal([]byte(r.Settings), &settings); err != nil || settings == nil {
		return errors.New("settings must be a JSON object")
	}
	if err := a.validateAdvisor(ctx, r.Settings); err != nil {
		return err
	}
	switch r.Strategy {
	case store.StrategyEscalate:
		return a.validateRules(ctx, r.Settings)
	case store.StrategyGuided:
		return a.validateGuided(ctx, r.Settings)
	}
	return nil
}

// validateAdvisor checks the advisor model: Anthropic runs the advisor tool,
// so the model must be on an Anthropic provider.
func (a *API) validateAdvisor(ctx context.Context, settings string) error {
	adv, err := store.ParseRouteAdvisor(settings)
	if err != nil {
		return fmt.Errorf("advisor: %w", err)
	}
	if adv.ModelID == 0 {
		return nil
	}
	if adv.Off {
		return errors.New("advisor cannot have both a model and off")
	}
	m, err := a.store.GetModel(ctx, adv.ModelID)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("advisor model %d does not exist", adv.ModelID)
	}
	if err != nil {
		return err
	}
	if !m.Enabled {
		return fmt.Errorf("advisor model %s is disabled", m.ModelID)
	}
	p, err := a.store.GetProvider(ctx, m.ProviderID)
	if err != nil {
		return err
	}
	if t := provider.Type(p.Type); t != provider.Anthropic && t != provider.AnthropicSubscription {
		return fmt.Errorf("advisor model %s must be on an Anthropic provider, because Anthropic runs the advisor", m.ModelID)
	}
	return nil
}

func (a *API) validateGuided(ctx context.Context, settings string) error {
	s, err := guided.ParseSettings(settings)
	if err != nil {
		return err
	}
	m, err := a.store.GetModel(ctx, s.Director.ModelID)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("director model %d does not exist", s.Director.ModelID)
	}
	if err != nil {
		return err
	}
	if !m.Enabled {
		return fmt.Errorf("director model %s is disabled", m.ModelID)
	}
	return nil
}

// settingsModelID returns the model a route references in its settings: the
// escalate classifier or the guided director.
func settingsModelID(rt store.Route) int64 {
	switch rt.Strategy {
	case store.StrategyEscalate:
		if rules, err := escalate.ParseRules(rt.Settings); err == nil && rules.Classifier.Enabled {
			return rules.Classifier.ModelID
		}
	case store.StrategyGuided:
		if s, err := guided.ParseSettings(rt.Settings); err == nil {
			return s.Director.ModelID
		}
	}
	return 0
}

func (a *API) validateRules(ctx context.Context, settings string) error {
	rules, err := escalate.ParseRules(settings)
	if err != nil {
		return err
	}
	if !rules.Classifier.Enabled {
		return nil
	}
	m, err := a.store.GetModel(ctx, rules.Classifier.ModelID)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("classifier model %d does not exist", rules.Classifier.ModelID)
	}
	if err != nil {
		return err
	}
	if !m.Enabled {
		return fmt.Errorf("classifier model %s is disabled", m.ModelID)
	}
	p, err := a.store.GetProvider(ctx, m.ProviderID)
	if err != nil {
		return err
	}
	if provider.Type(p.Type) == provider.AnthropicSubscription {
		return errors.New("the classifier cannot use a subscription provider, because the gateway makes that call itself")
	}
	return nil
}

// routesUsingModel lists routes that use the model as a tier, classifier, or director.
func (a *API) routesUsingModel(ctx context.Context, modelID int64) ([]string, error) {
	names, err := a.store.RoutesUsingModel(ctx, modelID)
	if err != nil {
		return nil, err
	}
	routes, err := a.store.ListRoutes(ctx)
	if err != nil {
		return nil, err
	}
	for _, rt := range routes {
		adv, err := store.ParseRouteAdvisor(rt.Settings)
		if !slices.Contains(names, rt.Name) && (settingsModelID(rt) == modelID || (err == nil && adv.ModelID == modelID)) {
			names = append(names, rt.Name)
		}
	}
	return names, nil
}

// defaultLabel turns "deepseek/DeepSeek-V4-Pro" into "deepseek-v4-pro".
func defaultLabel(modelID string) string {
	id := strings.ToLower(modelID[strings.LastIndexByte(modelID, '/')+1:])
	var b strings.Builder
	for _, c := range id {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-' {
			b.WriteRune(c)
		} else {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-._")
}

func (a *API) listRoutes(w http.ResponseWriter, r *http.Request) {
	rs, err := a.store.ListRoutes(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	out := make([]routeJSON, 0, len(rs))
	for _, rt := range rs {
		out = append(out, toRouteJSON(rt))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) createRoute(w http.ResponseWriter, r *http.Request) {
	var in routeInput
	if !decode(w, r, &in) {
		return
	}
	rt := store.Route{Settings: "{}"}
	in.apply(&rt)
	if err := a.normalizeRoute(r.Context(), &rt); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rt, err := a.store.CreateRoute(r.Context(), rt)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toRouteJSON(rt))
}

func (a *API) updateRoute(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	rt, err := a.store.GetRoute(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return
	}
	var in routeInput
	if !decode(w, r, &in) {
		return
	}
	in.apply(&rt)
	if err := a.normalizeRoute(r.Context(), &rt); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.store.UpdateRoute(r.Context(), rt); err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toRouteJSON(rt))
}

func (a *API) deleteRoute(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := a.store.DeleteRoute(r.Context(), id); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
