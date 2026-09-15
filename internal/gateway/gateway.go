// Package gateway serves the Anthropic Messages API that Claude Code talks to.
package gateway

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"intellyrouter/internal/ledger"
	"intellyrouter/internal/store"
)

type Server struct {
	store  *store.Store
	ledger *ledger.Recorder
	client *http.Client
	log    *slog.Logger
}

func New(st *store.Store, rec *ledger.Recorder, client *http.Client, log *slog.Logger) *Server {
	return &Server{store: st, ledger: rec, client: client, log: log}
}

func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/messages", s.handleMessages)
	mux.HandleFunc("POST /v1/messages/count_tokens", s.handleCountTokens)
	mux.HandleFunc("GET /v1/models", s.handleModels)
	// Claude Code sends a best-effort connection-warming probe here.
	mux.HandleFunc("HEAD /api/hello", func(http.ResponseWriter, *http.Request) {})
}

const keyHeader = "X-Intelly-Key"

// clientKey extracts the gateway key. x-intelly-key wins so that a Claude
// subscription token can stay in Authorization.
func clientKey(h http.Header) string {
	if k := h.Get(keyHeader); k != "" {
		return k
	}
	if k := h.Get("X-Api-Key"); k != "" {
		return k
	}
	if a := h.Get("Authorization"); strings.HasPrefix(a, "Bearer ") {
		return strings.TrimPrefix(a, "Bearer ")
	}
	return ""
}

func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) bool {
	key := clientKey(r.Header)
	if key == "" {
		writeError(w, http.StatusUnauthorized, "authentication_error", "missing gateway key")
		return false
	}
	_, ok, err := s.store.VerifyGatewayKey(r.Context(), key)
	if err != nil {
		s.internalError(w, "verify gateway key", err)
		return false
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication_error", "invalid gateway key")
		return false
	}
	return true
}

type errorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// errorJSON builds an error in the Anthropic API shape Claude Code expects.
func errorJSON(typ, message string) []byte {
	b, _ := json.Marshal(struct {
		Type  string      `json:"type"`
		Error errorDetail `json:"error"`
	}{"error", errorDetail{typ, message}})
	return b
}

func writeError(w http.ResponseWriter, status int, typ, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(errorJSON(typ, message))
}

func (s *Server) internalError(w http.ResponseWriter, what string, err error) {
	s.log.Error(what, "err", err)
	writeError(w, http.StatusInternalServerError, "api_error", "internal gateway error")
}
