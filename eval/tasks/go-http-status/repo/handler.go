// Package notes implements the notes HTTP API.
package notes

import (
	"encoding/json"
	"mime"
	"net/http"
	"strconv"
)

const (
	maxBodyBytes = 1 << 20
	maxTitleLen  = 120
)

type handler struct {
	store *Store
}

// NewHandler returns the HTTP handler for the notes API.
func NewHandler(store *Store) http.Handler {
	h := &handler{store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /notes", h.list)
	mux.HandleFunc("POST /notes", h.create)
	mux.HandleFunc("GET /notes/{id}", h.get)
	mux.HandleFunc("DELETE /notes/{id}", h.delete)
	return mux
}

type createRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.store.List())
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "content type must be application/json")
		return
	}

	var req createRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&req); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if len(req.Title) > maxTitleLen {
		writeError(w, http.StatusBadRequest, "title is too long")
		return
	}

	note := h.store.Create(req.Title, req.Body)
	writeJSON(w, http.StatusOK, note)
}

func (h *handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "note not found")
		return
	}
	note, _ := h.store.Get(id)
	writeJSON(w, http.StatusOK, note)
}

func (h *handler) delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "note not found")
		return
	}
	h.store.Delete(id)
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
