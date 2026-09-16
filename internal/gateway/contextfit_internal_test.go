package gateway

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MrMirhan/intellyrouter/internal/store"
)

// visionTiers builds a route whose middle tier is the only one that takes
// images, the shape that sends an escalated turn back down for one request.
func visionTiers(t *testing.T) (*Server, []store.Tier) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"), bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := t.Context()
	p, err := st.CreateProvider(ctx, store.Provider{Type: "anthropic-compatible", Name: "MiniMax", Slug: "mm", BaseURL: "https://api.minimax.io/anthropic", APIKey: "k", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	model := func(id string, window int64, vision bool) store.Model {
		m, err := st.CreateModel(ctx, store.Model{ProviderID: p.ID, ModelID: id, Context: window, Vision: vision, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	small := model("small-model", 200_000, false)
	seeing := model("seeing-model", 1_000_000, true)
	big := model("big-model", 0, false)
	return &Server{store: st}, []store.Tier{
		{ModelID: small.ID, Label: "small"},
		{ModelID: seeing.ID, Label: "seeing"},
		{ModelID: big.ID, Label: "big"},
	}
}

func TestFittingTierMovesDownForAnImage(t *testing.T) {
	s, tiers := visionTiers(t)
	ctx := t.Context()

	// The turn is on the top tier, which does not take images, so the request
	// alone drops to the tier that does.
	i, note, err := s.fittingTier(ctx, tiers, 2, 1000, true)
	if err != nil || i != 1 || !strings.Contains(note, "moved down to seeing") {
		t.Fatalf("escalated turn with an image: tier %d, note %q, err %v", i, note, err)
	}

	// Without an image the top tier stays.
	if i, note, err := s.fittingTier(ctx, tiers, 2, 1000, false); err != nil || i != 2 || note != "" {
		t.Fatalf("escalated turn without an image: tier %d, note %q, err %v", i, note, err)
	}

	// From the base tier the image moves up, as before.
	if i, note, err := s.fittingTier(ctx, tiers, 0, 1000, true); err != nil || i != 1 || !strings.Contains(note, "request has an image; moved to seeing") {
		t.Fatalf("base tier with an image: tier %d, note %q, err %v", i, note, err)
	}
}

func TestFittingTierCountsComboMembers(t *testing.T) {
	s, tiers := visionTiers(t)
	ctx := t.Context()
	seeing, err := s.store.GetModel(ctx, tiers[1].ModelID)
	if err != nil {
		t.Fatal(err)
	}
	combo, err := s.store.CreateCombo(ctx, store.Combo{Name: "stack", Strategy: store.ComboFallback, Enabled: true, Members: []int64{seeing.ID}})
	if err != nil {
		t.Fatal(err)
	}
	comboModel, err := s.store.GetModel(ctx, combo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !s.acceptsImages(ctx, comboModel) {
		t.Fatal("combo with a member that takes images does not accept them")
	}

	// The combo row itself has no vision flag, so only its member answers for it.
	withCombo := []store.Tier{tiers[0], {ModelID: combo.ID, Label: "stack"}}
	if i, note, err := s.fittingTier(ctx, withCombo, 0, 1000, true); err != nil || i != 1 || !strings.Contains(note, "moved to stack") {
		t.Fatalf("combo tier with an image: tier %d, note %q, err %v", i, note, err)
	}
}
