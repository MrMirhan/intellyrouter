package admin

import (
	"net/http"
	"strconv"
	"strings"

	"intellyrouter/internal/ledger"
	"intellyrouter/internal/store"
)

type legJSON struct {
	Seq              int     `json:"seq"`
	Role             string  `json:"role"`
	Provider         string  `json:"provider"`
	Model            string  `json:"model"`
	InputTokens      int64   `json:"input_tokens"`
	OutputTokens     int64   `json:"output_tokens"`
	CacheReadTokens  int64   `json:"cache_read_tokens"`
	CacheWriteTokens int64   `json:"cache_write_tokens"`
	CostUSD          float64 `json:"cost_usd"`
	LatencyMS        int64   `json:"latency_ms"`
	Status           string  `json:"status"`
	StopReason       string  `json:"stop_reason"`
	Note             string  `json:"note"`
}

type requestJSON struct {
	ID               int64     `json:"id"`
	TS               int64     `json:"ts"`
	SessionID        string    `json:"session_id"`
	AgentID          string    `json:"agent_id"`
	Route            string    `json:"route"`
	Strategy         string    `json:"strategy"`
	AuthMode         string    `json:"auth_mode"`
	ClientModel      string    `json:"client_model"`
	Stream           bool      `json:"stream"`
	Status           string    `json:"status"`
	HTTPStatus       int       `json:"http_status"`
	Error            string    `json:"error"`
	CostUSD          float64   `json:"cost_usd"`
	ReferenceCostUSD float64   `json:"reference_cost_usd"`
	LatencyMS        int64     `json:"latency_ms"`
	Legs             []legJSON `json:"legs,omitempty"`
}

func toRequestJSON(r store.Request) requestJSON {
	out := requestJSON{
		ID: r.ID, TS: r.TS, SessionID: r.SessionID, AgentID: r.AgentID, Route: r.Route, Strategy: r.Strategy,
		AuthMode: r.AuthMode, ClientModel: r.ClientModel, Stream: r.Stream, Status: r.Status, HTTPStatus: r.HTTPStatus,
		Error: r.Error, CostUSD: r.CostUSD, ReferenceCostUSD: r.ReferenceCostUSD, LatencyMS: r.LatencyMS,
	}
	for _, l := range r.Legs {
		out.Legs = append(out.Legs, legJSON{
			Seq: l.Seq, Role: l.Role, Provider: l.Provider, Model: l.Model,
			InputTokens: l.InputTokens, OutputTokens: l.OutputTokens, CacheReadTokens: l.CacheReadTokens, CacheWriteTokens: l.CacheWriteTokens,
			CostUSD: l.CostUSD, LatencyMS: l.LatencyMS, Status: l.Status, StopReason: l.StopReason, Note: l.Note,
		})
	}
	return out
}

func (a *API) listRequests(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	items, total, err := a.store.ListRequests(r.Context(), store.RequestFilter{
		SessionID: q.Get("session_id"), Route: q.Get("route"), Status: q.Get("status"),
		Limit: limit, Offset: offset,
	})
	if err != nil {
		a.fail(w, err)
		return
	}
	out := make([]requestJSON, 0, len(items))
	for _, it := range items {
		out = append(out, toRequestJSON(it))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "total": total})
}

func (a *API) getRequest(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	req, err := a.store.GetRequest(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toRequestJSON(req))
}

func (a *API) getSettings(w http.ResponseWriter, r *http.Request) {
	ref, ok, err := a.store.Setting(r.Context(), ledger.ReferenceModelSetting)
	if err != nil {
		a.fail(w, err)
		return
	}
	if !ok {
		ref = ledger.DefaultReferenceModel
	}
	writeJSON(w, http.StatusOK, map[string]string{"reference_model": ref})
}

func (a *API) putSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ReferenceModel *string `json:"reference_model"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.ReferenceModel != nil {
		v := strings.TrimSpace(*in.ReferenceModel)
		if v == "" {
			writeError(w, http.StatusBadRequest, "reference_model cannot be empty")
			return
		}
		if err := a.store.SetSetting(r.Context(), ledger.ReferenceModelSetting, v); err != nil {
			a.fail(w, err)
			return
		}
	}
	a.getSettings(w, r)
}
