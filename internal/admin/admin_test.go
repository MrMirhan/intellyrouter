package admin_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"intellyrouter/internal/admin"
	"intellyrouter/internal/store"
)

type client struct {
	t     *testing.T
	base  string
	token string
}

func (c client) do(method, path string, body any, want int) []byte {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(c.t.Context(), method, c.base+path, r)
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		c.t.Fatalf("%s %s = %d, want %d: %s", method, path, resp.StatusCode, want, out)
	}
	return out
}

func decodeInto[T any](t *testing.T, b []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
	return v
}

type model struct {
	ID      int64   `json:"id"`
	ModelID string  `json:"model_id"`
	PriceIn float64 `json:"price_in"`
	Enabled bool    `json:"enabled"`
}

func TestProviderModelRouteFlow(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("X-Api-Key") != "sk-x" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"claude-opus-5","display_name":"Claude Opus 5","max_input_tokens":1000000},{"id":"claude-haiku-4-5","display_name":"Claude Haiku 4.5"}]}`)
	}))
	defer upstream.Close()

	st, err := store.Open(filepath.Join(t.TempDir(), "admin.db"), bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	token, err := st.EnsureAdminToken(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	admin.New(st, upstream.Client(), slog.New(slog.DiscardHandler), nil).Register(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	anon := client{t: t, base: srv.URL}
	anon.do("GET", "/api/admin/providers", nil, http.StatusUnauthorized)
	c := client{t: t, base: srv.URL, token: token}

	c.do("POST", "/api/admin/providers", map[string]any{"type": "nope", "name": "x", "api_key": "k"}, http.StatusBadRequest)
	out := c.do("POST", "/api/admin/providers", map[string]any{"type": "anthropic", "name": "anthropic", "base_url": upstream.URL, "api_key": "sk-x"}, http.StatusCreated)
	if bytes.Contains(out, []byte("sk-x")) {
		t.Fatalf("provider response exposes the api key: %s", out)
	}
	p := decodeInto[struct {
		ID     int64 `json:"id"`
		HasKey bool  `json:"has_key"`
	}](t, out)
	if !p.HasKey {
		t.Fatal("has_key = false")
	}

	models := decodeInto[[]model](t, c.do("POST", fmt.Sprintf("/api/admin/providers/%d/sync-models", p.ID), nil, http.StatusOK))
	byID := map[string]model{}
	for _, m := range models {
		byID[m.ModelID] = m
	}
	opus, haiku := byID["claude-opus-5"], byID["claude-haiku-4-5"]
	if len(models) != 2 || opus.Enabled || opus.PriceIn != 5 {
		t.Fatalf("synced models = %+v", models)
	}

	routeBody := map[string]any{
		"name": "intelly-claude-auto", "strategy": "escalate",
		"tiers": []map[string]any{{"model_id": haiku.ID}, {"model_id": opus.ID, "label": "Opus"}},
	}
	c.do("POST", "/api/admin/routes", routeBody, http.StatusBadRequest)
	for _, id := range []int64{haiku.ID, opus.ID} {
		c.do("PATCH", fmt.Sprintf("/api/admin/models/%d", id), map[string]any{"enabled": true}, http.StatusOK)
	}
	rt := decodeInto[struct {
		Tiers []struct {
			Label string `json:"label"`
		} `json:"tiers"`
	}](t, c.do("POST", "/api/admin/routes", routeBody, http.StatusCreated))
	if len(rt.Tiers) != 2 || rt.Tiers[0].Label != "claude-haiku-4-5" || rt.Tiers[1].Label != "opus" {
		t.Fatalf("route tiers = %+v", rt.Tiers)
	}

	c.do("POST", "/api/admin/routes", map[string]any{"name": "one", "strategy": "escalate", "tiers": []map[string]any{{"model_id": haiku.ID}}}, http.StatusBadRequest)
	c.do("PATCH", fmt.Sprintf("/api/admin/models/%d", opus.ID), map[string]any{"enabled": false}, http.StatusConflict)
	c.do("DELETE", fmt.Sprintf("/api/admin/providers/%d", p.ID), nil, http.StatusConflict)

	key := decodeInto[struct {
		Key string `json:"key"`
	}](t, c.do("POST", "/api/admin/keys", map[string]any{"name": "laptop"}, http.StatusCreated))
	if !strings.HasPrefix(key.Key, "ik_") {
		t.Fatalf("gateway key = %q", key.Key)
	}

	sub := decodeInto[struct {
		ID     int64 `json:"id"`
		HasKey bool  `json:"has_key"`
	}](t, c.do("POST", "/api/admin/providers", map[string]any{"type": "anthropic-subscription", "name": "claude-login", "api_key": "must-be-dropped"}, http.StatusCreated))
	if sub.HasKey {
		t.Fatal("subscription provider stored an api key")
	}
	subModels := decodeInto[[]model](t, c.do("POST", fmt.Sprintf("/api/admin/providers/%d/sync-models", sub.ID), nil, http.StatusOK))
	found := false
	for _, m := range subModels {
		found = found || (m.ModelID == "claude-opus-5" && m.PriceIn == 5)
	}
	if !found {
		t.Fatalf("subscription models = %+v", subModels)
	}
	c.do("GET", "/api/admin/subscription/limits", nil, http.StatusOK)

	var subOpus int64
	for _, m := range subModels {
		if m.ModelID == "claude-opus-5" {
			subOpus = m.ID
		}
	}
	c.do("PATCH", fmt.Sprintf("/api/admin/models/%d", subOpus), map[string]any{"enabled": true}, http.StatusOK)
	escalateRoute := func(name string, classifier int64) map[string]any {
		return map[string]any{
			"name": name, "strategy": "escalate",
			"tiers":    []map[string]any{{"model_id": haiku.ID}, {"model_id": subOpus}},
			"settings": map[string]any{"classifier": map[string]any{"enabled": true, "model_id": classifier}},
		}
	}
	c.do("POST", "/api/admin/routes", escalateRoute("sub-classifier", subOpus), http.StatusBadRequest)
	c.do("POST", "/api/admin/routes", escalateRoute("haiku-classifier", haiku.ID), http.StatusCreated)

	// A subscription director is allowed: it takes the checkpoint step itself.
	guidedRoute := func(name string, settings map[string]any) map[string]any {
		return map[string]any{"name": name, "strategy": "guided", "tiers": []map[string]any{{"model_id": haiku.ID}}, "settings": settings}
	}
	c.do("POST", "/api/admin/routes", guidedRoute("guided-none", map[string]any{}), http.StatusBadRequest)
	c.do("POST", "/api/admin/routes", guidedRoute("guided-missing", map[string]any{"director": map[string]any{"model_id": 99999}}), http.StatusBadRequest)
	c.do("POST", "/api/admin/routes", guidedRoute("guided-effort", map[string]any{"director": map[string]any{"model_id": subOpus, "effort": "turbo"}}), http.StatusBadRequest)
	c.do("POST", "/api/admin/routes", guidedRoute("guided-sub", map[string]any{"director": map[string]any{"model_id": subOpus}}), http.StatusCreated)
	c.do("POST", "/api/admin/routes", map[string]any{"name": "guided-empty", "strategy": "guided", "tiers": []map[string]any{}, "settings": map[string]any{"director": map[string]any{"model_id": subOpus}}}, http.StatusBadRequest)
}

func TestMutationsRequireJSON(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "admin.db"), bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	token, _ := st.EnsureAdminToken(t.Context(), false)
	mux := http.NewServeMux()
	admin.New(st, http.DefaultClient, slog.New(slog.DiscardHandler), nil).Register(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequestWithContext(t.Context(), "POST", srv.URL+"/api/admin/keys", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("text/plain POST = %d, want 415", resp.StatusCode)
	}

	login, _ := http.NewRequestWithContext(t.Context(), "POST", srv.URL+"/api/admin/login", strings.NewReader(`{"token":"`+token+`"}`))
	login.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(login)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent || len(resp.Cookies()) != 1 || !resp.Cookies()[0].HttpOnly {
		t.Fatalf("login = %d cookies=%v", resp.StatusCode, resp.Cookies())
	}
}
