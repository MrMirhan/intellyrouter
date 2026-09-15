// Package gateway serves the Anthropic Messages API that Claude Code talks to.
package gateway

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"intellyrouter/internal/escalate"
	"intellyrouter/internal/guided"
	"intellyrouter/internal/ledger"
	"intellyrouter/internal/store"
)

type Server struct {
	store       *store.Store
	ledger      *ledger.Recorder
	client      *http.Client
	log         *slog.Logger
	decider     *escalate.Decider
	compat      *compat
	guidedTurns *guided.Tracker
	signatures  *signatureCache
}

// A turn decision outlives any realistic tool loop.
const turnTTL = 6 * time.Hour

func New(st *store.Store, rec *ledger.Recorder, client *http.Client, log *slog.Logger) *Server {
	return &Server{
		store: st, ledger: rec, client: client, log: log,
		decider: escalate.NewDecider(turnTTL), compat: newCompat(), guidedTurns: guided.NewTracker(turnTTL), signatures: newSignatureCache(turnTTL),
	}
}

func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/messages", s.handleMessages)
	mux.HandleFunc("POST /v1/messages/count_tokens", s.handleCountTokens)
	mux.HandleFunc("GET /v1/models", s.handleModels)
	// Claude Code sends a best-effort connection-warming probe here.
	mux.HandleFunc("HEAD /api/hello", func(http.ResponseWriter, *http.Request) {})
}

const keyHeader = "X-Intelly-Key"

// clientKey extracts the gateway key and reports whether it came from
// Authorization. x-intelly-key wins so that a Claude login can stay in Authorization.
func clientKey(h http.Header) (key string, fromAuthorization bool) {
	if k := h.Get(keyHeader); k != "" {
		return k, false
	}
	if k := h.Get("X-Api-Key"); k != "" {
		return k, false
	}
	if a := h.Get("Authorization"); strings.HasPrefix(a, "Bearer ") {
		return strings.TrimPrefix(a, "Bearer "), true
	}
	return "", false
}

// authenticate verifies the gateway key. When Authorization did not carry the
// gateway key, its value is returned as the client's Claude login, which only
// subscription routes pass through.
func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) (claudeAuth string, ok bool) {
	key, fromAuthorization := clientKey(r.Header)
	if key == "" {
		writeError(w, http.StatusUnauthorized, "authentication_error", "missing gateway key")
		return "", false
	}
	_, valid, err := s.store.VerifyGatewayKey(r.Context(), key)
	if err != nil {
		s.internalError(w, "verify gateway key", err)
		return "", false
	}
	if !valid {
		writeError(w, http.StatusUnauthorized, "authentication_error", "invalid gateway key")
		return "", false
	}
	if fromAuthorization {
		return "", true
	}
	return r.Header.Get("Authorization"), true
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
