package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

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
	return nil
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
