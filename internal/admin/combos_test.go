package admin_test

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MrMirhan/intellyrouter/internal/admin"
	"github.com/MrMirhan/intellyrouter/internal/store"
)

func TestCombosAndProviderSlugs(t *testing.T) {
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
	admin.New(st, http.DefaultClient, slog.New(slog.DiscardHandler), nil, "claude").Register(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client{t: t, base: srv.URL, token: token}

	type providerOut struct {
		ID   int64  `json:"id"`
		Slug string `json:"slug"`
	}
	sub := decodeInto[providerOut](t, c.do("POST", "/api/admin/providers", map[string]any{"type": "anthropic-subscription", "name": "Claude subscription"}, http.StatusCreated))
	if sub.Slug != "claude-subscription" {
		t.Fatalf("default slug = %q", sub.Slug)
	}
	sub = decodeInto[providerOut](t, c.do("PATCH", fmt.Sprintf("/api/admin/providers/%d", sub.ID), map[string]any{"slug": "cc"}, http.StatusOK))
	if sub.Slug != "cc" {
		t.Fatalf("slug = %q", sub.Slug)
	}
	c.do("POST", "/api/admin/providers", map[string]any{"type": "anthropic-compatible", "name": "Other", "slug": "cc", "base_url": "https://x", "api_key": "k"}, http.StatusBadRequest)
	c.do("POST", "/api/admin/providers", map[string]any{"type": "anthropic-compatible", "name": "Other", "slug": "combo", "base_url": "https://x", "api_key": "k"}, http.StatusBadRequest)
	c.do("POST", "/api/admin/providers", map[string]any{"type": "anthropic-compatible", "name": "Other", "slug": "Bad Slug", "base_url": "https://x", "api_key": "k"}, http.StatusBadRequest)

	opus := decodeInto[model](t, c.do("POST", fmt.Sprintf("/api/admin/providers/%d/models", sub.ID), map[string]any{"model_id": "claude-opus-5"}, http.StatusCreated))
	sonnet := decodeInto[model](t, c.do("POST", fmt.Sprintf("/api/admin/providers/%d/models", sub.ID), map[string]any{"model_id": "claude-sonnet-5"}, http.StatusCreated))

	type comboOut struct {
		ID       int64   `json:"id"`
		Name     string  `json:"name"`
		Strategy string  `json:"strategy"`
		Members  []int64 `json:"members"`
	}
	c.do("POST", "/api/admin/combos", map[string]any{"name": "stack", "members": []int64{}}, http.StatusBadRequest)
	c.do("POST", "/api/admin/combos", map[string]any{"name": "stack", "strategy": "random", "members": []int64{opus.ID}}, http.StatusBadRequest)
	c.do("POST", "/api/admin/combos", map[string]any{"name": "has space", "members": []int64{opus.ID}}, http.StatusBadRequest)
	c.do("POST", "/api/admin/combos", map[string]any{"name": "stack", "members": []int64{opus.ID, opus.ID}}, http.StatusBadRequest)
	combo := decodeInto[comboOut](t, c.do("POST", "/api/admin/combos", map[string]any{"name": "stack", "members": []int64{opus.ID, sonnet.ID}}, http.StatusCreated))
	if combo.Strategy != "fallback" || len(combo.Members) != 2 {
		t.Fatalf("combo = %+v", combo)
	}
	c.do("POST", "/api/admin/combos", map[string]any{"name": "nested", "members": []int64{combo.ID}}, http.StatusBadRequest)
	combo = decodeInto[comboOut](t, c.do("PATCH", fmt.Sprintf("/api/admin/combos/%d", combo.ID), map[string]any{"strategy": "least-used"}, http.StatusOK))
	if combo.Strategy != "least-used" {
		t.Fatalf("updated combo = %+v", combo)
	}

	// A route can use the combo like a model, and the combo stays while it does.
	c.do("POST", "/api/admin/routes", map[string]any{"name": "claude-stack", "strategy": "direct", "tiers": []map[string]any{{"model_id": combo.ID}}}, http.StatusCreated)
	out := c.do("DELETE", fmt.Sprintf("/api/admin/combos/%d", combo.ID), nil, http.StatusConflict)
	if !strings.Contains(string(out), "claude-stack") {
		t.Fatalf("conflict does not name the route: %s", out)
	}
	providers := c.do("GET", "/api/admin/providers", nil, http.StatusOK)
	comboProvider := decodeInto[[]struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	}](t, providers)
	for _, p := range comboProvider {
		if p.Type == store.ComboProviderType {
			c.do("DELETE", fmt.Sprintf("/api/admin/providers/%d", p.ID), nil, http.StatusBadRequest)
			c.do("POST", fmt.Sprintf("/api/admin/providers/%d/sync-models", p.ID), nil, http.StatusBadRequest)
		}
	}
}
