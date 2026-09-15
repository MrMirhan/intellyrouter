// Package admin serves the JSON API behind the dashboard.
package admin

import (
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"intellyrouter/internal/store"
)

const cookieName = "intelly_admin"

type API struct {
	store  *store.Store
	client *http.Client
	log    *slog.Logger
}

func New(st *store.Store, client *http.Client, log *slog.Logger) *API {
	return &API{store: st, client: client, log: log}
}

func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/admin/login", a.login)
	mux.HandleFunc("POST /api/admin/logout", a.logout)
	for _, rt := range []struct {
		pattern string
		handler http.HandlerFunc
	}{
		{"GET /api/admin/session", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }},

		{"GET /api/admin/providers", a.listProviders},
		{"POST /api/admin/providers", a.createProvider},
		{"PATCH /api/admin/providers/{id}", a.updateProvider},
		{"DELETE /api/admin/providers/{id}", a.deleteProvider},
		{"POST /api/admin/providers/{id}/sync-models", a.syncModels},
		{"POST /api/admin/providers/{id}/models", a.createModel},

		{"GET /api/admin/models", a.listModels},
		{"PATCH /api/admin/models/{id}", a.updateModel},

		{"GET /api/admin/routes", a.listRoutes},
		{"POST /api/admin/routes", a.createRoute},
		{"PATCH /api/admin/routes/{id}", a.updateRoute},
		{"DELETE /api/admin/routes/{id}", a.deleteRoute},

		{"GET /api/admin/keys", a.listKeys},
		{"POST /api/admin/keys", a.createKey},
		{"DELETE /api/admin/keys/{id}", a.revokeKey},

		{"GET /api/admin/requests", a.listRequests},
		{"GET /api/admin/requests/{id}", a.getRequest},

		{"GET /api/admin/settings", a.getSettings},
		{"PUT /api/admin/settings", a.putSettings},
	} {
		mux.Handle(rt.pattern, a.requireAdmin(rt.handler))
	}
}

func (a *API) requireAdmin(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := ""
		if c, err := r.Cookie(cookieName); err == nil {
			token = c.Value
		}
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimPrefix(h, "Bearer ")
		}
		ok, err := a.store.VerifyAdminToken(r.Context(), token)
		if err != nil {
			a.fail(w, err)
			return
		}
		if !ok {
			writeError(w, http.StatusUnauthorized, "admin login required")
			return
		}
		// A cross-site HTML form cannot send JSON, so requiring it blocks CSRF.
		if r.Method == http.MethodPost || r.Method == http.MethodPatch || r.Method == http.MethodPut {
			if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
				writeError(w, http.StatusUnsupportedMediaType, "content type must be application/json")
				return
			}
		}
		next(w, r)
	})
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &in) {
		return
	}
	ok, err := a.store.VerifyAdminToken(r.Context(), in.Token)
	if err != nil {
		a.fail(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, "invalid admin token")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: in.Token, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 30 * 24 * 60 * 60,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) logout(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

func (a *API) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "a record with this name already exists")
	case errors.Is(err, store.ErrInUse):
		writeError(w, http.StatusConflict, "still used by a route; remove it from the route first")
	default:
		a.log.Error("admin request failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}
