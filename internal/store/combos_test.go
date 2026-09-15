package store

import (
	"errors"
	"slices"
	"testing"
)

func TestCombos(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	p, err := s.CreateProvider(ctx, Provider{Type: "anthropic-compatible", Name: "MiniMax", Slug: "mm", BaseURL: "https://api.minimax.io/anthropic", APIKey: "k", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	m3, err := s.CreateModel(ctx, Model{ProviderID: p.ID, ModelID: "MiniMax-M3", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	m27, err := s.CreateModel(ctx, Model{ProviderID: p.ID, ModelID: "MiniMax-M2.7", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	c, err := s.CreateCombo(ctx, Combo{Name: "stack", Strategy: ComboFallback, Enabled: true, Members: []int64{m27.ID, m3.ID}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetCombo(ctx, c.ID)
	if err != nil || got.Name != "stack" || got.Strategy != ComboFallback || !slices.Equal(got.Members, []int64{m27.ID, m3.ID}) {
		t.Fatalf("GetCombo = %+v, %v", got, err)
	}
	cp, err := s.ProviderBySlug(ctx, "combo")
	if err != nil || cp.Type != ComboProviderType {
		t.Fatalf("combo provider = %+v, %v", cp, err)
	}
	if m, err := s.ModelByProvider(ctx, cp.ID, "stack"); err != nil || m.ID != c.ID {
		t.Fatalf("combo model = %+v, %v", m, err)
	}
	if byPrefix, err := s.ProviderBySlug(ctx, "mm"); err != nil || byPrefix.ID != p.ID {
		t.Fatalf("ProviderBySlug(mm) = %+v, %v", byPrefix, err)
	}
	if _, err := s.CreateCombo(ctx, Combo{Name: "stack", Strategy: ComboFallback, Members: []int64{m3.ID}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate combo name: %v", err)
	}

	c.Name, c.Strategy, c.Members = "stack-2", ComboRoundRobin, []int64{m3.ID}
	if err := s.UpdateCombo(ctx, c); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetCombo(ctx, c.ID); got.Name != "stack-2" || got.Strategy != ComboRoundRobin || !slices.Equal(got.Members, []int64{m3.ID}) {
		t.Fatalf("updated combo = %+v", got)
	}
	if names, _ := s.CombosUsingModel(ctx, m3.ID); !slices.Equal(names, []string{"stack-2"}) {
		t.Fatalf("CombosUsingModel(m3) = %v", names)
	}
	if names, _ := s.CombosUsingModel(ctx, m27.ID); len(names) != 0 {
		t.Fatalf("CombosUsingModel(m27) = %v", names)
	}

	rt, err := s.CreateRoute(ctx, Route{Name: "claude-combo", Strategy: StrategyDirect, Tiers: []Tier{{ModelID: c.ID, Label: "stack"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteCombo(ctx, c.ID); !errors.Is(err, ErrInUse) {
		t.Fatalf("delete a combo a route uses: %v", err)
	}
	if err := s.DeleteRoute(ctx, rt.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteCombo(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetCombo(ctx, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted combo: %v", err)
	}
	if err := s.DeleteCombo(ctx, m3.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteCombo on a plain model: %v", err)
	}
}

func TestSlug(t *testing.T) {
	for name, want := range map[string]string{"Claude subscription": "claude-subscription", " Z.ai ": "z.ai", "MiniMax (CN)": "minimax--cn"} {
		if got := Slug(name); got != want {
			t.Errorf("Slug(%q) = %q, want %q", name, got, want)
		}
	}
}
