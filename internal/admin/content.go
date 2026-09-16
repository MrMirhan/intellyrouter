package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/MrMirhan/intellyrouter/internal/store"
)

type contentLegJSON struct {
	Seq     int             `json:"seq"`
	Role    string          `json:"role"`
	Model   string          `json:"model"`
	Billing string          `json:"billing"`
	Input   string          `json:"input"`
	Output  json.RawMessage `json:"output"`
}

type contentJSON struct {
	MessageCount int              `json:"message_count"`
	Request      json.RawMessage  `json:"request"`
	Response     json.RawMessage  `json:"response"`
	Legs         []contentLegJSON `json:"legs"`
	Meta         *requestJSON     `json:"meta,omitempty"`
}

// getRequestContent returns the captured request and leg outputs; tail=N keeps
// the last N messages.
func (a *API) getRequestContent(w http.ResponseWriter, r *http.Request) {
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	a.writeContent(w, r, tail, false)
}

func (a *API) exportRequest(w http.ResponseWriter, r *http.Request) {
	a.writeContent(w, r, 0, true)
}

func (a *API) writeContent(w http.ResponseWriter, r *http.Request, tail int, export bool) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	c, err := a.store.RequestContent(r.Context(), id, tail)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no content was captured for this request")
		return
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	req, err := a.store.GetRequest(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return
	}
	captured := make(map[int]store.LegContent, len(c.Legs))
	for _, l := range c.Legs {
		captured[l.Seq] = l
	}
	out := contentJSON{MessageCount: c.MessageCount, Request: rawJSON(c.Body), Legs: make([]contentLegJSON, 0, len(req.Legs))}
	for _, l := range req.Legs {
		lc := captured[l.Seq]
		out.Legs = append(out.Legs, contentLegJSON{Seq: l.Seq, Role: l.Role, Model: l.Model, Billing: l.Billing, Input: lc.Input, Output: rawJSON(lc.Output)})
	}
	// The response is the last output; an advisor leg has none of its own.
	for i := len(out.Legs) - 1; i >= 0; i-- {
		if o := out.Legs[i].Output; len(o) > 0 && string(o) != "null" {
			out.Response = o
			break
		}
	}
	if export {
		meta := toRequestJSON(req)
		out.Meta = &meta
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="request-%d.json"`, id))
	}
	writeJSON(w, http.StatusOK, out)
}

// rawJSON passes stored JSON through as is. An upstream can answer 200 with a
// body that is not JSON; that body becomes a string.
func rawJSON(b []byte) json.RawMessage {
	switch {
	case len(b) == 0:
		return nil
	case json.Valid(b):
		return b
	}
	s, _ := json.Marshal(string(b))
	return s
}
