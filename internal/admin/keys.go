package admin

import (
	"net/http"
	"strings"

	"intellyrouter/internal/store"
)

type keyJSON struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Prefix     string `json:"prefix"`
	CreatedAt  int64  `json:"created_at"`
	LastUsedAt int64  `json:"last_used_at"`
	RevokedAt  int64  `json:"revoked_at"`
}

func toKeyJSON(k store.GatewayKey) keyJSON {
	return keyJSON{ID: k.ID, Name: k.Name, Prefix: k.Prefix, CreatedAt: k.CreatedAt, LastUsedAt: k.LastUsedAt, RevokedAt: k.RevokedAt}
}

func (a *API) listKeys(w http.ResponseWriter, r *http.Request) {
	ks, err := a.store.ListGatewayKeys(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	out := make([]keyJSON, 0, len(ks))
	for _, k := range ks {
		out = append(out, toKeyJSON(k))
	}
	writeJSON(w, http.StatusOK, out)
}

// createKey returns the plain key once; it cannot be read again.
func (a *API) createKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &in) {
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	plain, k, err := a.store.CreateGatewayKey(r.Context(), name)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, struct {
		keyJSON
		Key string `json:"key"`
	}{toKeyJSON(k), plain})
}

func (a *API) revokeKey(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := a.store.RevokeGatewayKey(r.Context(), id); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
