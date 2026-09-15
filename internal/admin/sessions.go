package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"intellyrouter/internal/store"
)

type sessionModelJSON struct {
	Model   string `json:"model"`
	Billing string `json:"billing"`
	Calls   int64  `json:"calls"`
}

type sessionSummaryJSON struct {
	SessionID            string             `json:"session_id"`
	FirstTS              int64              `json:"first_ts"`
	LastTS               int64              `json:"last_ts"`
	Requests             int64              `json:"requests"`
	Errors               int64              `json:"errors"`
	Agents               int64              `json:"agents"`
	Routes               []string           `json:"routes"`
	Models               []sessionModelJSON `json:"models"`
	CostUSD              float64            `json:"cost_usd"`
	SubscriptionValueUSD float64            `json:"subscription_value_usd"`
	InputTokens          int64              `json:"input_tokens"`
	OutputTokens         int64              `json:"output_tokens"`
	CacheReadTokens      int64              `json:"cache_read_tokens"`
	CacheWriteTokens     int64              `json:"cache_write_tokens"`
	DirectorCalls        int64              `json:"director_calls"`
	CapturedRequests     int64              `json:"captured_requests"`
}

// sessionByModelJSON costs legs at their API-equivalent price, so subscription
// rows show the subscription value. LatencyMS is the total of the calls.
type sessionByModelJSON struct {
	Role             string  `json:"role"`
	Model            string  `json:"model"`
	Billing          string  `json:"billing"`
	Calls            int64   `json:"calls"`
	InputTokens      int64   `json:"input_tokens"`
	OutputTokens     int64   `json:"output_tokens"`
	CacheReadTokens  int64   `json:"cache_read_tokens"`
	CacheWriteTokens int64   `json:"cache_write_tokens"`
	CostUSD          float64 `json:"cost_usd"`
	LatencyMS        int64   `json:"latency_ms"`
}

type checkpointJSON struct {
	RequestID int64  `json:"request_id"`
	TS        int64  `json:"ts"`
	Model     string `json:"model"`
	Billing   string `json:"billing"`
	Status    string `json:"status"`
	Note      string `json:"note"`
	LatencyMS int64  `json:"latency_ms"`
}

type sessionJSON struct {
	Summary     sessionSummaryJSON   `json:"summary"`
	ByModel     []sessionByModelJSON `json:"by_model"`
	Checkpoints []checkpointJSON     `json:"checkpoints"`
	Comparison  comparisonJSON       `json:"comparison"`
	Requests    []requestJSON        `json:"requests"`
}

// sessionLineJSON is one line of a session export. Content fields are null for
// requests without captured content.
type sessionLineJSON struct {
	Meta          requestJSON       `json:"meta"`
	Envelope      json.RawMessage   `json:"envelope"`
	MessagesAdded []json.RawMessage `json:"messages_added"`
	LegsContent   []legContentJSON  `json:"legs_content"`
}

type legContentJSON struct {
	Seq    int             `json:"seq"`
	Input  string          `json:"input"`
	Output json.RawMessage `json:"output"`
}

func toSessionSummaryJSON(s store.SessionSummary) sessionSummaryJSON {
	out := sessionSummaryJSON{
		SessionID: s.SessionID, FirstTS: s.FirstTS, LastTS: s.LastTS, Requests: s.Requests, Errors: s.Errors, Agents: s.Agents,
		Routes: s.Routes, Models: make([]sessionModelJSON, 0, len(s.Models)),
		CostUSD: s.CostUSD, SubscriptionValueUSD: s.SubscriptionValueUSD,
		InputTokens: s.InputTokens, OutputTokens: s.OutputTokens, CacheReadTokens: s.CacheReadTokens, CacheWriteTokens: s.CacheWriteTokens,
		DirectorCalls: s.DirectorCalls, CapturedRequests: s.CapturedRequests,
	}
	for _, m := range s.Models {
		out.Models = append(out.Models, sessionModelJSON(m))
	}
	return out
}

func (a *API) listSessions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	items, total, err := a.store.ListSessions(r.Context(), store.SessionFilter{Route: q.Get("route"), Limit: limit, Offset: offset})
	if err != nil {
		a.fail(w, err)
		return
	}
	out := make([]sessionSummaryJSON, 0, len(items))
	for _, it := range items {
		out = append(out, toSessionSummaryJSON(it))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "total": total})
}

func (a *API) getSession(w http.ResponseWriter, r *http.Request) {
	sess, err := a.store.Session(r.Context(), r.PathValue("id"))
	if err != nil {
		a.fail(w, err)
		return
	}
	ref, err := a.referenceModel(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	out := sessionJSON{
		Summary:     toSessionSummaryJSON(sess.Summary),
		ByModel:     make([]sessionByModelJSON, 0, len(sess.ByModel)),
		Checkpoints: make([]checkpointJSON, 0, len(sess.Checkpoints)),
		Comparison:  newComparison(ref, sess.Work),
		Requests:    make([]requestJSON, 0, len(sess.Requests)),
	}
	for _, m := range sess.ByModel {
		out.ByModel = append(out.ByModel, sessionByModelJSON(m))
	}
	for _, c := range sess.Checkpoints {
		out.Checkpoints = append(out.Checkpoints, checkpointJSON(c))
	}
	for _, req := range sess.Requests {
		out.Requests = append(out.Requests, toRequestItem(req))
	}
	writeJSON(w, http.StatusOK, out)
}

// exportSession writes one JSON line per request. Messages Claude Code resent
// from earlier requests of the same agent are left out; see store.SessionItem.
func (a *API) exportSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	items, err := a.store.SessionContent(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return
	}
	if len(items) == 0 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/x-ndjson")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Disposition", `attachment; filename="session-`+fileSafe(id)+`.jsonl"`)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for _, it := range items {
		line := sessionLineJSON{Meta: toRequestJSON(it.Request)}
		if it.Captured {
			line.Envelope = rawJSON(it.Envelope)
			if it.MessagesAdded != nil {
				line.MessagesAdded = make([]json.RawMessage, 0, len(it.MessagesAdded))
				for _, m := range it.MessagesAdded {
					line.MessagesAdded = append(line.MessagesAdded, rawJSON(m))
				}
			}
			line.LegsContent = make([]legContentJSON, 0, len(it.Legs))
			for _, l := range it.Legs {
				line.LegsContent = append(line.LegsContent, legContentJSON{Seq: l.Seq, Input: l.Input, Output: rawJSON(l.Output)})
			}
		}
		if err := enc.Encode(line); err != nil {
			return
		}
	}
}

// fileSafe keeps a session ID, which a client sets, safe in a quoted filename.
func fileSafe(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		}
		return '_'
	}, s)
}
