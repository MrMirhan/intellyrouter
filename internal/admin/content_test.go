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
	"testing"

	"github.com/MrMirhan/intellyrouter/internal/admin"
	"github.com/MrMirhan/intellyrouter/internal/store"
)

func get(t *testing.T, url, token string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, out
}

func TestContentAndSessionEndpoints(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "admin.db"), bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := t.Context()
	token, _ := st.EnsureAdminToken(ctx, false)
	mux := http.NewServeMux()
	admin.New(st, http.DefaultClient, slog.New(slog.DiscardHandler), nil).Register(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client{t: t, base: srv.URL, token: token}

	const body = `{"model":"guided","messages":[{"role":"user","content":"hi"},{"role":"user","content":"<b>fix</b> it"}]}`
	captured, err := st.InsertRequest(ctx, store.Request{
		TS: 1000, SessionID: "sess-1", Route: "guided", Strategy: store.StrategyGuided, ClientModel: "guided", Status: "ok", HTTPStatus: 200, CostUSD: 0.02,
		Legs: []store.Leg{
			{Role: "director", Provider: "a", Model: "claude-opus-5", Billing: "api", InputTokens: 100, CostUSD: 0.01, Status: "ok", Note: "checkpoint: plan"},
			{Seq: 1, Role: "executor", Provider: "d", Model: "deepseek-v4-flash", Billing: "api", InputTokens: 1000, OutputTokens: 100, CostUSD: 0.01, Status: "ok"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = st.SaveContent(ctx, captured, store.Content{CreatedAt: 1000, Input: []byte(body), Legs: []store.LegContent{
		{Seq: 0, Input: "Checkpoint: plan", Output: []byte(`{"id":"msg_d","type":"message","content":[{"type":"text","text":"do X"}]}`)},
		{Seq: 1, Input: "do X", Output: []byte(`{"id":"msg_e","type":"message","content":[{"type":"text","text":"done"}]}`)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	plain, err := st.InsertRequest(ctx, store.Request{
		TS: 2000, SessionID: "sess-1", Route: "guided", Strategy: store.StrategyGuided, ClientModel: "guided", Status: "upstream_error", HTTPStatus: 529,
		Legs: []store.Leg{{Role: "executor", Provider: "d", Model: "deepseek-v4-flash", Billing: "api", InputTokens: 500, Status: "upstream_error"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("content", func(t *testing.T) {
		out := c.do("GET", fmt.Sprintf("/api/admin/requests/%d/content", plain), nil, http.StatusNotFound)
		if !bytes.Contains(out, []byte(`"no content was captured for this request"`)) {
			t.Errorf("404 body = %s", out)
		}
		c.do("GET", "/api/admin/requests/x/content", nil, http.StatusBadRequest)

		got := decodeInto[struct {
			MessageCount int `json:"message_count"`
			Request      struct {
				Model    string            `json:"model"`
				Messages []json.RawMessage `json:"messages"`
			} `json:"request"`
			Response struct {
				ID string `json:"id"`
			} `json:"response"`
			Legs []struct {
				Seq    int    `json:"seq"`
				Role   string `json:"role"`
				Model  string `json:"model"`
				Input  string `json:"input"`
				Output struct {
					ID string `json:"id"`
				} `json:"output"`
			} `json:"legs"`
		}](t, c.do("GET", fmt.Sprintf("/api/admin/requests/%d/content?tail=1", captured), nil, http.StatusOK))
		if got.MessageCount != 2 || got.Request.Model != "guided" || len(got.Request.Messages) != 1 || got.Response.ID != "msg_e" {
			t.Fatalf("content = %+v", got)
		}
		if len(got.Legs) != 2 || got.Legs[0].Role != "director" || got.Legs[0].Input != "Checkpoint: plan" || got.Legs[0].Output.ID != "msg_d" ||
			got.Legs[1].Seq != 1 || got.Legs[1].Model != "deepseek-v4-flash" || got.Legs[1].Input != "do X" {
			t.Fatalf("content legs = %+v", got.Legs)
		}
	})

	t.Run("request export", func(t *testing.T) {
		resp, out := get(t, fmt.Sprintf("%s/api/admin/requests/%d/export", srv.URL, captured), token)
		want := fmt.Sprintf(`attachment; filename="request-%d.json"`, captured)
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/json" || resp.Header.Get("Content-Disposition") != want {
			t.Fatalf("export = %d %v", resp.StatusCode, resp.Header)
		}
		got := decodeInto[struct {
			Request struct {
				Messages []json.RawMessage `json:"messages"`
			} `json:"request"`
			Meta struct {
				ID          int64            `json:"id"`
				Captured    bool             `json:"captured"`
				AnswerModel string           `json:"answer_model"`
				Legs        []map[string]any `json:"legs"`
			} `json:"meta"`
		}](t, out)
		if len(got.Request.Messages) != 2 || got.Meta.ID != captured || !got.Meta.Captured || len(got.Meta.Legs) != 2 || got.Meta.AnswerModel != "deepseek-v4-flash" {
			t.Fatalf("export body = %s", out)
		}
	})

	t.Run("requests list", func(t *testing.T) {
		out := c.do("GET", "/api/admin/requests", nil, http.StatusOK)
		got := decodeInto[struct {
			Items []struct {
				ID          int64          `json:"id"`
				Captured    bool           `json:"captured"`
				AnswerModel string         `json:"answer_model"`
				Models      []legModelJSON `json:"models"`
			} `json:"items"`
		}](t, out)
		if len(got.Items) != 2 || got.Items[0].ID != plain || got.Items[0].Captured || !got.Items[1].Captured {
			t.Fatalf("items = %s", out)
		}
		wantModels := []legModelJSON{{"director", "claude-opus-5", "api"}, {"executor", "deepseek-v4-flash", "api"}}
		if got.Items[1].AnswerModel != "deepseek-v4-flash" || fmt.Sprint(got.Items[1].Models) != fmt.Sprint(wantModels) {
			t.Fatalf("models = %+v, answer = %q", got.Items[1].Models, got.Items[1].AnswerModel)
		}
		if bytes.Contains(out, []byte(`"legs"`)) {
			t.Errorf("list items include legs: %s", out)
		}
		detail := c.do("GET", fmt.Sprintf("/api/admin/requests/%d", captured), nil, http.StatusOK)
		if !bytes.Contains(detail, []byte(`"legs":[`)) || !bytes.Contains(detail, []byte(`"answer_model":"deepseek-v4-flash"`)) {
			t.Errorf("request detail = %s", detail)
		}
	})

	t.Run("sessions", func(t *testing.T) {
		list := decodeInto[struct {
			Total int              `json:"total"`
			Items []map[string]any `json:"items"`
		}](t, c.do("GET", "/api/admin/sessions?limit=10", nil, http.StatusOK))
		if list.Total != 1 || len(list.Items) != 1 {
			t.Fatalf("sessions = %+v", list)
		}
		item := list.Items[0]
		if item["session_id"] != "sess-1" || item["requests"] != 2.0 || item["errors"] != 1.0 || item["director_calls"] != 1.0 ||
			item["captured_requests"] != 1.0 || fmt.Sprint(item["routes"]) != "[guided]" || item["input_tokens"] != 1600.0 {
			t.Fatalf("session item = %+v", item)
		}
		empty := c.do("GET", "/api/admin/sessions?route=other", nil, http.StatusOK)
		if !bytes.Contains(empty, []byte(`"items":[]`)) || !bytes.Contains(empty, []byte(`"total":0`)) {
			t.Errorf("filtered sessions = %s", empty)
		}

		detail := decodeInto[struct {
			Summary struct {
				Requests int `json:"requests"`
			} `json:"summary"`
			ByModel []struct {
				Role  string `json:"role"`
				Calls int    `json:"calls"`
			} `json:"by_model"`
			Checkpoints []struct {
				RequestID int64  `json:"request_id"`
				Note      string `json:"note"`
			} `json:"checkpoints"`
			Comparison struct {
				ReferenceModel string `json:"reference_model"`
				WorkTokens     struct {
					Input int64 `json:"input"`
				} `json:"work_tokens"`
				SingleModel []struct {
					Model string `json:"model"`
				} `json:"single_model"`
			} `json:"comparison"`
			Requests []struct {
				ID       int64            `json:"id"`
				Captured bool             `json:"captured"`
				Models   []map[string]any `json:"models"`
			} `json:"requests"`
		}](t, c.do("GET", "/api/admin/sessions/sess-1", nil, http.StatusOK))
		if detail.Summary.Requests != 2 || len(detail.ByModel) != 2 || detail.ByModel[0].Role != "executor" || detail.ByModel[0].Calls != 2 {
			t.Fatalf("session detail = %+v", detail)
		}
		if len(detail.Checkpoints) != 1 || detail.Checkpoints[0].RequestID != captured || detail.Checkpoints[0].Note != "checkpoint: plan" {
			t.Errorf("checkpoints = %+v", detail.Checkpoints)
		}
		if detail.Comparison.ReferenceModel != "claude-fable-5-1" || detail.Comparison.WorkTokens.Input != 1500 || len(detail.Comparison.SingleModel) != 5 {
			t.Errorf("comparison = %+v", detail.Comparison)
		}
		if len(detail.Requests) != 2 || detail.Requests[0].ID != captured || !detail.Requests[0].Captured || len(detail.Requests[1].Models) != 1 {
			t.Errorf("session requests = %+v", detail.Requests)
		}
		c.do("GET", "/api/admin/sessions/unknown", nil, http.StatusNotFound)
	})

	t.Run("session export", func(t *testing.T) {
		resp, out := get(t, srv.URL+"/api/admin/sessions/sess-1/export", token)
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Disposition") != `attachment; filename="session-sess-1.jsonl"` {
			t.Fatalf("export = %d %v", resp.StatusCode, resp.Header)
		}
		lines := bytes.Split(bytes.TrimSpace(out), []byte("\n"))
		if len(lines) != 2 {
			t.Fatalf("export lines = %d: %s", len(lines), out)
		}
		type line struct {
			Meta struct {
				ID int64 `json:"id"`
			} `json:"meta"`
			Envelope      map[string]any    `json:"envelope"`
			MessagesAdded []json.RawMessage `json:"messages_added"`
			LegsContent   []map[string]any  `json:"legs_content"`
		}
		first, second := decodeInto[line](t, lines[0]), decodeInto[line](t, lines[1])
		if first.Meta.ID != captured || first.Envelope["model"] != "guided" || len(first.MessagesAdded) != 2 || len(first.LegsContent) != 2 {
			t.Errorf("first line = %s", lines[0])
		}
		if !bytes.Contains(lines[0], []byte("<b>fix</b> it")) {
			t.Errorf("export escapes message text: %s", lines[0])
		}
		if second.Meta.ID != plain || !bytes.Contains(lines[1], []byte(`"envelope":null,"messages_added":null,"legs_content":null`)) {
			t.Errorf("second line = %s", lines[1])
		}
		c.do("GET", "/api/admin/sessions/unknown/export", nil, http.StatusNotFound)
	})

	t.Run("settings", func(t *testing.T) {
		defaults := c.do("GET", "/api/admin/settings", nil, http.StatusOK)
		if !bytes.Contains(defaults, []byte(`"capture_content":false`)) || !bytes.Contains(defaults, []byte(`"capture_retention_days":14`)) {
			t.Fatalf("default settings = %s", defaults)
		}
		for _, bad := range []map[string]any{
			{"capture_retention_days": 0}, {"capture_retention_days": 366}, {"capture_retention_days": "7"}, {"capture_content": "yes"},
		} {
			c.do("PUT", "/api/admin/settings", bad, http.StatusBadRequest)
		}
		updated := c.do("PUT", "/api/admin/settings", map[string]any{"capture_content": true, "capture_retention_days": 30}, http.StatusOK)
		if !bytes.Contains(updated, []byte(`"capture_content":true`)) || !bytes.Contains(updated, []byte(`"capture_retention_days":30`)) {
			t.Fatalf("updated settings = %s", updated)
		}
	})

	t.Run("stats comparison", func(t *testing.T) {
		got := decodeInto[struct {
			Totals struct {
				Comparison struct {
					ReferenceModel string            `json:"reference_model"`
					SingleModel    []json.RawMessage `json:"single_model"`
				} `json:"comparison"`
			} `json:"totals"`
		}](t, c.do("GET", "/api/admin/stats", nil, http.StatusOK))
		if got.Totals.Comparison.ReferenceModel != "claude-fable-5-1" || len(got.Totals.Comparison.SingleModel) != 5 {
			t.Errorf("stats comparison = %+v", got.Totals.Comparison)
		}
	})
}

type legModelJSON struct {
	Role    string `json:"role"`
	Model   string `json:"model"`
	Billing string `json:"billing"`
}
