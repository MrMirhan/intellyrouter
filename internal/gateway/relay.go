package gateway

import (
	"io"
	"mime"
	"net/http"
	"time"

	"intellyrouter/internal/ledger"
	"intellyrouter/internal/provider"
	"intellyrouter/internal/sse"
)

const maxResponseBytes = 64 << 20

var skipResponseHeaders = map[string]bool{
	"Connection":         true,
	"Keep-Alive":         true,
	"Proxy-Authenticate": true,
	"Proxy-Connection":   true,
	"Te":                 true,
	"Trailer":            true,
	"Transfer-Encoding":  true,
	"Upgrade":            true,
	"Content-Length":     true,
	"Content-Encoding":   true,
	"Set-Cookie":         true,
}

// forward sends body to an Anthropic-format upstream and relays the response
// to the client. It writes the client response in every case.
func (s *Server) forward(w http.ResponseWriter, r *http.Request, t target, body []byte) ledger.Leg {
	leg := ledger.Leg{Provider: t.provider.Name, Model: t.model.ModelID, Price: t.price()}
	start := time.Now()
	req, err := provider.NewAnthropicRequest(r.Context(), t.config, r.URL.Path, r.URL.RawQuery, body, r.Header)
	if err != nil {
		leg.Status, leg.HTTPStatus, leg.Error = ledger.StatusError, http.StatusInternalServerError, err.Error()
		s.internalError(w, "build upstream request", err)
		return leg
	}
	resp, err := s.client.Do(req)
	if err != nil {
		leg.Latency = time.Since(start)
		if r.Context().Err() != nil {
			leg.Status = ledger.StatusCanceled
			return leg
		}
		leg.Status, leg.HTTPStatus, leg.Error = ledger.StatusError, http.StatusBadGateway, "upstream request failed: "+err.Error()
		writeError(w, http.StatusBadGateway, "api_error", leg.Error)
		return leg
	}
	defer resp.Body.Close()

	var tr ledger.AnthropicTracker
	relayErr := relay(w, resp, &tr)
	leg.Latency = time.Since(start)
	leg.Usage, leg.StopReason, leg.HTTPStatus = tr.Usage, tr.StopReason, resp.StatusCode
	switch {
	case relayErr != nil && r.Context().Err() != nil:
		leg.Status = ledger.StatusCanceled
	case relayErr != nil:
		leg.Status, leg.Error = ledger.StatusError, relayErr.Error()
	case resp.StatusCode >= 300 || tr.Error != "":
		leg.Status, leg.Error = ledger.StatusUpstreamError, tr.Error
	default:
		leg.Status = ledger.StatusOK
	}
	return leg
}

// relay copies an upstream response to the client. Event streams are flushed
// as bytes arrive, pings included, and usage is read from the same bytes.
func relay(w http.ResponseWriter, resp *http.Response, tr *ledger.AnthropicTracker) error {
	for name, values := range resp.Header {
		if !skipResponseHeaders[name] {
			w.Header()[name] = values
		}
	}
	w.WriteHeader(resp.StatusCode)
	if resp.StatusCode == http.StatusOK && isEventStream(resp.Header) {
		events := sse.NewReader(io.TeeReader(resp.Body, flushWriter{w: w, rc: http.NewResponseController(w)}))
		for {
			ev, err := events.Next()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			tr.Event(ev.Name, ev.Data)
		}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return err
	}
	tr.Response(resp.StatusCode, b)
	_, err = w.Write(b)
	return err
}

func isEventStream(h http.Header) bool {
	mt, _, _ := mime.ParseMediaType(h.Get("Content-Type"))
	return mt == "text/event-stream"
}

type flushWriter struct {
	w  io.Writer
	rc *http.ResponseController
}

func (f flushWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	if err != nil {
		return n, err
	}
	return n, f.rc.Flush()
}
