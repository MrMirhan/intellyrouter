package gateway

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"intellyrouter/internal/ledger"
	"intellyrouter/internal/provider"
	"intellyrouter/internal/sse"
	"intellyrouter/internal/translate"
)

// Claude Code aborts a stream after 300 seconds without bytes.
const pingInterval = 15 * time.Second

// forwardOpenAI translates the request for a Chat Completions upstream and the
// response back to Anthropic format. It writes the client response in every case.
func (s *Server) forwardOpenAI(w http.ResponseWriter, r *http.Request, t target, body []byte, stream, capture bool) ledger.Leg {
	leg := t.newLeg()
	start := time.Now()
	done := func(status string, httpStatus int, msg string) ledger.Leg {
		leg.Latency = time.Since(start)
		leg.Status, leg.HTTPStatus, leg.Error = status, httpStatus, msg
		return leg
	}

	opts := translate.Options{Model: t.model.ModelID, MaxTokensField: t.config.Type.MaxTokensField()}
	if needsThoughtSignatures(t) {
		opts.ThoughtSignature = s.signatures.get
	}
	upBody, err := translate.Request(body, opts)
	if err != nil {
		msg := "cannot translate request: " + err.Error()
		writeError(w, http.StatusBadRequest, "invalid_request_error", msg)
		return done(ledger.StatusError, http.StatusBadRequest, msg)
	}
	req, err := provider.NewOpenAIRequest(r.Context(), t.config, upBody, stream)
	if err != nil {
		s.internalError(w, "build upstream request", err)
		return done(ledger.StatusError, http.StatusInternalServerError, err.Error())
	}
	resp, err := s.client.Do(req)
	if err != nil {
		if r.Context().Err() != nil {
			return done(ledger.StatusCanceled, 0, "")
		}
		msg := "upstream request failed: " + err.Error()
		writeError(w, http.StatusBadGateway, "api_error", msg)
		return done(ledger.StatusError, http.StatusBadGateway, msg)
	}
	defer resp.Body.Close()

	tr := ledger.AnthropicTracker{Capture: capture}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		out := translate.Error(resp.StatusCode, raw)
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			w.Header().Set("Retry-After", ra)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(out)
		tr.Response(resp.StatusCode, out)
		return done(ledger.StatusUpstreamError, resp.StatusCode, tr.Error)
	}

	if !stream {
		raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		var out []byte
		if err == nil {
			out, err = translate.Response(raw, t.model.ModelID)
			s.signatures.put(translate.ThoughtSignatures(raw))
		}
		if err != nil {
			msg := "cannot translate upstream response: " + err.Error()
			writeError(w, http.StatusBadGateway, "api_error", msg)
			return done(ledger.StatusError, http.StatusBadGateway, msg)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(out)
		tr.Response(http.StatusOK, out)
		leg.Usage, leg.StopReason, leg.Output = tr.Usage, tr.StopReason, tr.Message()
		return done(ledger.StatusOK, http.StatusOK, "")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	ew := newEventWriter(w)
	stopPings := ew.keepAlive(pingInterval)
	ts := translate.NewStream(t.model.ModelID, func(name string, data []byte) error {
		tr.Event(name, data)
		return ew.write(name, data)
	})
	err = translateStream(resp.Body, ts)
	stopPings()
	s.signatures.put(ts.ThoughtSignatures())
	leg.Usage, leg.StopReason, leg.Output = tr.Usage, tr.StopReason, tr.Message()

	var upErr *translate.UpstreamError
	switch {
	case err == nil:
		return done(ledger.StatusOK, http.StatusOK, "")
	case errors.As(err, &upErr):
		return done(ledger.StatusUpstreamError, http.StatusOK, upErr.Message)
	case r.Context().Err() != nil:
		return done(ledger.StatusCanceled, http.StatusOK, "")
	default:
		// Headers are already sent; an error event lets Claude Code retry.
		_ = ew.write("error", errorJSON("api_error", err.Error()))
		return done(ledger.StatusError, http.StatusOK, err.Error())
	}
}

func translateStream(body io.Reader, ts *translate.Stream) error {
	events := sse.NewReader(body)
	for {
		ev, err := events.Next()
		if err == io.EOF || (err == nil && string(ev.Data) == "[DONE]") {
			return ts.Finish()
		}
		if err != nil {
			return err
		}
		if len(bytes.TrimSpace(ev.Data)) == 0 {
			continue
		}
		if err := ts.Chunk(ev.Data); err != nil {
			return err
		}
	}
}

// eventWriter serializes SSE writes from the translator and the keep-alive pinger.
type eventWriter struct {
	mu   sync.Mutex
	w    io.Writer
	rc   *http.ResponseController
	last time.Time
}

func newEventWriter(w http.ResponseWriter) *eventWriter {
	return &eventWriter{w: w, rc: http.NewResponseController(w), last: time.Now()}
}

func (e *eventWriter) write(name string, data []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, err := fmt.Fprintf(e.w, "event: %s\ndata: %s\n\n", name, data); err != nil {
		return err
	}
	e.last = time.Now()
	return e.rc.Flush()
}

// keepAlive writes a ping whenever the stream has been silent for interval,
// which covers providers that send nothing while the model reasons. The
// returned function stops the pinger and waits for it to exit.
func (e *eventWriter) keepAlive(interval time.Duration) func() {
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		ticker := time.NewTicker(interval / 3)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				e.mu.Lock()
				idle := time.Since(e.last) >= interval
				e.mu.Unlock()
				if idle {
					_ = e.write("ping", []byte(`{"type": "ping"}`))
				}
			}
		}
	})
	return func() {
		close(stop)
		wg.Wait()
	}
}
