package admin_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"intellyrouter/internal/admin"
	"intellyrouter/internal/store"
)

func TestStatsEndpoint(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "admin.db"), bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	token, _ := st.EnsureAdminToken(t.Context(), false)
	mux := http.NewServeMux()
	admin.New(st, http.DefaultClient, slog.New(slog.DiscardHandler)).Register(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client{t: t, base: srv.URL, token: token}

	var got struct {
		Range    string           `json:"range"`
		BucketMS int64            `json:"bucket_ms"`
		Totals   map[string]any   `json:"totals"`
		Series   []map[string]any `json:"series"`
		ByRoute  []map[string]any `json:"by_route"`
	}
	if err := json.Unmarshal(c.do("GET", "/api/admin/stats?range=7d", nil, http.StatusOK), &got); err != nil {
		t.Fatal(err)
	}
	if got.Range != "7d" || got.BucketMS != 6*3_600_000 || got.Totals["requests"] != 0.0 || got.Series == nil || got.ByRoute == nil {
		t.Fatalf("stats = %+v", got)
	}
	c.do("GET", "/api/admin/stats?range=1y", nil, http.StatusBadRequest)
}
