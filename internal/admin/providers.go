package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"intellyrouter/internal/provider"
	"intellyrouter/internal/store"
)

type providerJSON struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	BaseURL   string `json:"base_url"`
	HasKey    bool   `json:"has_key"`
	Enabled   bool   `json:"enabled"`
	CreatedAt int64  `json:"created_at"`
}

func toProviderJSON(p store.Provider) providerJSON {
	return providerJSON{ID: p.ID, Type: p.Type, Name: p.Name, Slug: p.Slug, BaseURL: p.BaseURL, HasKey: p.APIKey != "", Enabled: p.Enabled, CreatedAt: p.CreatedAt}
}

type providerInput struct {
	Type    *string `json:"type"`
	Name    *string `json:"name"`
	Slug    *string `json:"slug"`
	BaseURL *string `json:"base_url"`
	APIKey  *string `json:"api_key"`
	Enabled *bool   `json:"enabled"`
}

func (in providerInput) apply(p *store.Provider) {
	if in.Type != nil {
		p.Type = *in.Type
	}
	if in.Name != nil {
		p.Name = strings.TrimSpace(*in.Name)
	}
	if in.Slug != nil {
		p.Slug = strings.TrimSpace(*in.Slug)
	}
	if in.BaseURL != nil {
		p.BaseURL = strings.TrimSpace(*in.BaseURL)
	}
	if in.APIKey != nil {
		p.APIKey = strings.TrimSpace(*in.APIKey)
	}
	if in.Enabled != nil {
		p.Enabled = *in.Enabled
	}
}

// validateProvider checks the provider, fills an empty slug from the name, and
// clears any key sent for a subscription provider, which must never store a
// credential.
func (a *API) validateProvider(ctx context.Context, p *store.Provider) error {
	t := provider.Type(p.Type)
	if p.Slug == "" {
		p.Slug = store.Slug(p.Name)
	}
	switch {
	case p.Type == store.ComboProviderType:
		return errComboProvider
	case !labelPattern.MatchString(p.Slug):
		return errors.New(`slug may only contain a-z, 0-9, ".", "_" and "-", and must start with a letter or digit`)
	case p.Slug == "combo":
		return errors.New(`the slug "combo" is reserved for combos`)
	case !t.Valid():
		return fmt.Errorf("unknown provider type %q", p.Type)
	case p.Name == "":
		return errors.New("name is required")
	case t.NeedsBaseURL() && p.BaseURL == "":
		return fmt.Errorf("%s providers need a base_url", p.Type)
	case t.NeedsKey() && p.APIKey == "":
		return errors.New("api_key is required")
	}
	providers, err := a.store.ListProviders(ctx)
	if err != nil {
		return err
	}
	for _, other := range providers {
		if other.ID != p.ID && other.Slug == p.Slug {
			return fmt.Errorf("provider %s already uses the slug %q", other.Name, p.Slug)
		}
	}
	if t == provider.AnthropicSubscription {
		p.APIKey = ""
	}
	return nil
}

var errComboProvider = errors.New("the combo provider is managed on the Combos page")

func (a *API) listProviders(w http.ResponseWriter, r *http.Request) {
	ps, err := a.store.ListProviders(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	out := make([]providerJSON, 0, len(ps))
	for _, p := range ps {
		out = append(out, toProviderJSON(p))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) createProvider(w http.ResponseWriter, r *http.Request) {
	var in providerInput
	if !decode(w, r, &in) {
		return
	}
	p := store.Provider{Enabled: true}
	in.apply(&p)
	if err := a.validateProvider(r.Context(), &p); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	p, err := a.store.CreateProvider(r.Context(), p)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toProviderJSON(p))
}

func (a *API) updateProvider(w http.ResponseWriter, r *http.Request) {
	p, ok := a.loadProvider(w, r)
	if !ok {
		return
	}
	var in providerInput
	if !decode(w, r, &in) {
		return
	}
	if p.Type == store.ComboProviderType {
		writeError(w, http.StatusBadRequest, errComboProvider.Error())
		return
	}
	in.apply(&p)
	if err := a.validateProvider(r.Context(), &p); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.store.UpdateProvider(r.Context(), p); err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toProviderJSON(p))
}

func (a *API) deleteProvider(w http.ResponseWriter, r *http.Request) {
	p, ok := a.loadProvider(w, r)
	if !ok {
		return
	}
	if p.Type == store.ComboProviderType {
		writeError(w, http.StatusBadRequest, errComboProvider.Error())
		return
	}
	if err := a.store.DeleteProvider(r.Context(), p.ID); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// syncModels fetches the provider's model list; it doubles as a connection test.
func (a *API) syncModels(w http.ResponseWriter, r *http.Request) {
	p, ok := a.loadProvider(w, r)
	if !ok {
		return
	}
	if p.Type == store.ComboProviderType {
		writeError(w, http.StatusBadRequest, errComboProvider.Error())
		return
	}
	remote, err := provider.ListModels(r.Context(), a.client, provider.ConfigFor(p))
	if err != nil {
		writeError(w, http.StatusBadGateway, "list models: "+err.Error())
		return
	}
	models := make([]store.Model, 0, len(remote))
	for _, m := range remote {
		models = append(models, store.Model{
			ModelID: m.ID, DisplayName: m.DisplayName, Context: m.Context,
			PriceIn: m.Price.In, PriceOut: m.Price.Out, PriceCacheRead: m.Price.CacheRead, PriceCacheWrite: m.Price.CacheWrite,
		})
	}
	if err := a.store.SyncModels(r.Context(), p.ID, models); err != nil {
		a.fail(w, err)
		return
	}
	a.writeModels(w, r, p.ID)
}

func (a *API) loadProvider(w http.ResponseWriter, r *http.Request) (store.Provider, bool) {
	id, ok := pathID(w, r)
	if !ok {
		return store.Provider{}, false
	}
	p, err := a.store.GetProvider(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return store.Provider{}, false
	}
	return p, true
}

type modelJSON struct {
	ID              int64   `json:"id"`
	ProviderID      int64   `json:"provider_id"`
	ModelID         string  `json:"model_id"`
	DisplayName     string  `json:"display_name"`
	PriceIn         float64 `json:"price_in"`
	PriceOut        float64 `json:"price_out"`
	PriceCacheRead  float64 `json:"price_cache_read"`
	PriceCacheWrite float64 `json:"price_cache_write"`
	Context         int64   `json:"context"`
	Enabled         bool    `json:"enabled"`
	Vision          bool    `json:"vision"`
}

func toModelJSON(m store.Model) modelJSON {
	return modelJSON{
		ID: m.ID, ProviderID: m.ProviderID, ModelID: m.ModelID, DisplayName: m.DisplayName,
		PriceIn: m.PriceIn, PriceOut: m.PriceOut, PriceCacheRead: m.PriceCacheRead, PriceCacheWrite: m.PriceCacheWrite,
		Context: m.Context, Enabled: m.Enabled, Vision: m.Vision,
	}
}

type modelInput struct {
	ModelID         *string  `json:"model_id"`
	DisplayName     *string  `json:"display_name"`
	PriceIn         *float64 `json:"price_in"`
	PriceOut        *float64 `json:"price_out"`
	PriceCacheRead  *float64 `json:"price_cache_read"`
	PriceCacheWrite *float64 `json:"price_cache_write"`
	Context         *int64   `json:"context"`
	Enabled         *bool    `json:"enabled"`
	Vision          *bool    `json:"vision"`
}

func (in modelInput) apply(m *store.Model) {
	if in.DisplayName != nil {
		m.DisplayName = strings.TrimSpace(*in.DisplayName)
	}
	for _, f := range []struct {
		src *float64
		dst *float64
	}{{in.PriceIn, &m.PriceIn}, {in.PriceOut, &m.PriceOut}, {in.PriceCacheRead, &m.PriceCacheRead}, {in.PriceCacheWrite, &m.PriceCacheWrite}} {
		if f.src != nil {
			*f.dst = *f.src
		}
	}
	if in.Vision != nil {
		m.Vision = *in.Vision
	}
	if in.Context != nil {
		m.Context = *in.Context
	}
	if in.Enabled != nil {
		m.Enabled = *in.Enabled
	}
}

// createModel adds a model by hand for providers that do not list their models.
func (a *API) createModel(w http.ResponseWriter, r *http.Request) {
	p, ok := a.loadProvider(w, r)
	if !ok {
		return
	}
	if p.Type == store.ComboProviderType {
		writeError(w, http.StatusBadRequest, errComboProvider.Error())
		return
	}
	var in modelInput
	if !decode(w, r, &in) {
		return
	}
	if in.ModelID == nil || strings.TrimSpace(*in.ModelID) == "" {
		writeError(w, http.StatusBadRequest, "model_id is required")
		return
	}
	m := store.Model{ProviderID: p.ID, ModelID: strings.TrimSpace(*in.ModelID), Enabled: true}
	in.apply(&m)
	m, err := a.store.CreateModel(r.Context(), m)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toModelJSON(m))
}

func (a *API) listModels(w http.ResponseWriter, r *http.Request) {
	var providerID int64
	if v := r.URL.Query().Get("provider_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid provider_id")
			return
		}
		providerID = id
	}
	a.writeModels(w, r, providerID)
}

func (a *API) writeModels(w http.ResponseWriter, r *http.Request, providerID int64) {
	ms, err := a.store.ListModels(r.Context(), providerID)
	if err != nil {
		a.fail(w, err)
		return
	}
	out := make([]modelJSON, 0, len(ms))
	for _, m := range ms {
		out = append(out, toModelJSON(m))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) updateModel(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	m, err := a.store.GetModel(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return
	}
	var in modelInput
	if !decode(w, r, &in) {
		return
	}
	wasEnabled := m.Enabled
	in.apply(&m)
	if wasEnabled && !m.Enabled {
		routes, err := a.routesUsingModel(r.Context(), m.ID)
		if err != nil {
			a.fail(w, err)
			return
		}
		if len(routes) > 0 {
			writeError(w, http.StatusConflict, "model is used by routes: "+strings.Join(routes, ", "))
			return
		}
	}
	if err := a.store.UpdateModel(r.Context(), m); err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toModelJSON(m))
}
