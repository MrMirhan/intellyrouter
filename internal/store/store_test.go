package store

import (
	"bytes"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"), bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestProviderKeyIsEncryptedAtRest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path, bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	p, err := s.CreateProvider(ctx, Provider{Type: "anthropic", Name: "a", APIKey: "sk-secret-123", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := s.db.QueryRowContext(ctx, `SELECT api_key_enc FROM providers WHERE id = ?`, p.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 || bytes.Contains(raw, []byte("sk-secret-123")) {
		t.Fatal("api key is not encrypted at rest")
	}
	got, err := s.GetProvider(ctx, p.ID)
	if err != nil || got.APIKey != "sk-secret-123" {
		t.Fatalf("GetProvider = %q, %v", got.APIKey, err)
	}
	s.Close()

	other, err := Open(path, bytes.Repeat([]byte{8}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err := other.GetProvider(ctx, p.ID); err == nil {
		t.Fatal("a different master key decrypted the api key")
	}
}

func TestGatewayKeys(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	plain, k, err := s.CreateGatewayKey(ctx, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.VerifyGatewayKey(ctx, plain); !ok || err != nil {
		t.Fatalf("new key rejected: %v", err)
	}
	if _, ok, _ := s.VerifyGatewayKey(ctx, plain+"x"); ok {
		t.Fatal("wrong key accepted")
	}
	if err := s.RevokeGatewayKey(ctx, k.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.VerifyGatewayKey(ctx, plain); ok {
		t.Fatal("revoked key accepted")
	}
}

func TestAdminToken(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	token, err := s.EnsureAdminToken(ctx, false)
	if err != nil || token == "" {
		t.Fatalf("first EnsureAdminToken = %q, %v", token, err)
	}
	if again, _ := s.EnsureAdminToken(ctx, false); again != "" {
		t.Fatal("EnsureAdminToken issued a second token without reset")
	}
	if ok, _ := s.VerifyAdminToken(ctx, token); !ok {
		t.Fatal("admin token rejected")
	}
	if ok, _ := s.VerifyAdminToken(ctx, ""); ok {
		t.Fatal("empty admin token accepted")
	}
	reset, _ := s.EnsureAdminToken(ctx, true)
	if ok, _ := s.VerifyAdminToken(ctx, token); ok || reset == "" {
		t.Fatal("old admin token still valid after reset")
	}
}

func TestRouteTiersAndModelInUse(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	p, err := s.CreateProvider(ctx, Provider{Type: "anthropic-compatible", Name: "deepseek", APIKey: "k", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	flash, err := s.CreateModel(ctx, Model{ProviderID: p.ID, ModelID: "deepseek-v4-flash", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	opus, err := s.CreateModel(ctx, Model{ProviderID: p.ID, ModelID: "claude-opus-5", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	created, err := s.CreateRoute(ctx, Route{
		Name: "intelly-claude-auto", Strategy: StrategyEscalate,
		Tiers: []Tier{{ModelID: flash.ID, Label: "flash"}, {ModelID: opus.ID, Label: "opus"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.RouteByName(ctx, "intelly-claude-auto")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Tiers, created.Tiers) || got.Settings != "{}" {
		t.Fatalf("route = %+v, want tiers %+v", got, created.Tiers)
	}
	if names, _ := s.RoutesUsingModel(ctx, opus.ID); !reflect.DeepEqual(names, []string{"intelly-claude-auto"}) {
		t.Fatalf("RoutesUsingModel = %v", names)
	}
	if err := s.DeleteProvider(ctx, p.ID); !errors.Is(err, ErrInUse) {
		t.Fatalf("deleting a provider used by a route: err = %v, want ErrInUse", err)
	}

	got.Strategy, got.Tiers = StrategyDirect, got.Tiers[:1]
	if err := s.UpdateRoute(ctx, got); err != nil {
		t.Fatal(err)
	}
	if names, _ := s.RoutesUsingModel(ctx, opus.ID); len(names) != 0 {
		t.Fatalf("model still in use after removing its tier: %v", names)
	}
	_, err = s.CreateRoute(ctx, Route{Name: "intelly-claude-auto", Strategy: StrategyDirect, Tiers: []Tier{{ModelID: flash.ID, Label: "flash"}}})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate route name: err = %v, want ErrConflict", err)
	}
}

func TestSyncModelsKeepsUserEdits(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	p, err := s.CreateProvider(ctx, Provider{Type: "openrouter", Name: "or", APIKey: "k", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SyncModels(ctx, p.ID, []Model{{ModelID: "m1", DisplayName: "M1", PriceIn: 1, PriceOut: 2}}); err != nil {
		t.Fatal(err)
	}
	ms, err := s.ListModels(ctx, p.ID)
	if err != nil || len(ms) != 1 || ms[0].Enabled || ms[0].PriceIn != 1 {
		t.Fatalf("after first sync: %+v, %v", ms, err)
	}
	m := ms[0]
	m.Enabled, m.PriceIn = true, 9
	if err := s.UpdateModel(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncModels(ctx, p.ID, []Model{{ModelID: "m1", DisplayName: "M1 renamed", PriceIn: 1, PriceOut: 2}, {ModelID: "m2"}}); err != nil {
		t.Fatal(err)
	}
	ms, err = s.ListModels(ctx, p.ID)
	if err != nil || len(ms) != 2 {
		t.Fatalf("after second sync: %+v, %v", ms, err)
	}
	if ms[0].ModelID != "m1" || !ms[0].Enabled || ms[0].PriceIn != 9 || ms[0].DisplayName != "M1 renamed" {
		t.Fatalf("sync overwrote user edits: %+v", ms[0])
	}
}

func TestRequestLedger(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	id, err := s.InsertRequest(ctx, Request{
		TS: 1, Route: "r", Strategy: StrategyDirect, ClientModel: "r", Status: "ok", HTTPStatus: 200, CostUSD: 0.5,
		Legs: []Leg{{Role: "direct", Provider: "p", Model: "m", Billing: "subscription", InputTokens: 10, Status: "ok"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetRequest(ctx, id)
	if err != nil || len(got.Legs) != 1 || got.Legs[0].InputTokens != 10 || got.Legs[0].Billing != "subscription" || got.CostUSD != 0.5 {
		t.Fatalf("GetRequest = %+v, %v", got, err)
	}
	items, total, err := s.ListRequests(ctx, RequestFilter{Route: "r"})
	if err != nil || total != 1 || len(items) != 1 || items[0].Legs != nil {
		t.Fatalf("ListRequests(route r) = %+v, %d, %v", items, total, err)
	}
	if _, total, _ := s.ListRequests(ctx, RequestFilter{Route: "other"}); total != 0 {
		t.Fatalf("ListRequests(route other) total = %d", total)
	}
}
