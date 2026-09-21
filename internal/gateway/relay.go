package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/MrMirhan/intellyrouter/internal/ledger"
	"github.com/MrMirhan/intellyrouter/internal/provider"
	"github.com/MrMirhan/intellyrouter/internal/sse"
	"github.com/MrMirhan/intellyrouter/internal/store"
)

const maxResponseBytes = 64 << 20

// errStreamTruncated reports a stream that ended without its message_stop
// event. The client saw part of an answer and has no way to know it was cut
// off; the combo moves to the next member when no content reached it.
var errStreamTruncated = errors.New("upstream stream ended without message_stop")

// streamReleaser is implemented by writers that hold a 2xx response while the
// upstream streams it. The relay calls releaseOnContent when the first
// content delta arrives, so the held bytes are flushed and subsequent bytes
// are passed straight through.
type streamReleaser interface{ releaseOnContent() }

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
func (s *Server) forward(w http.ResponseWriter, r *http.Request, t target, body []byte, claudeAuth string, capture bool) ledger.Leg {
	leg := t.newLeg()
	start := time.Now()
	body, adapted, toolNames := s.compat.apply(t.model.ID, body)
	var resp *http.Response
	for {
		req, err := provider.NewAnthropicRequest(r.Context(), t.config, r.URL.Path, r.URL.RawQuery, body, r.Header, claudeAuth)
		if err != nil {
			leg.Status, leg.HTTPStatus, leg.Error = ledger.StatusError, http.StatusInternalServerError, err.Error()
			s.internalError(w, "build upstream request", err)
			return leg
		}
		resp, err = s.client.Do(req)
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
		if resp.StatusCode != http.StatusBadRequest || len(adapted) >= maxAdaptations {
			break
		}
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		next, adaptation, learnNames, ok := s.compat.learn(t.model.ID, errBody, body)
		if !ok {
			resp.Body = io.NopCloser(bytes.NewReader(errBody))
			break
		}
		body, adapted = next, append(adapted, adaptation)
		if learnNames != nil {
			toolNames = learnNames
		}
	}
	defer resp.Body.Close()
	if len(adapted) > 0 {
		leg.Note = "adapted for " + t.model.ModelID + ": " + strings.Join(adapted, ", ")
	}

	// A provider with a lower tool-name limit saw shortened names, but the
	// client only knows the long ones, so the response needs them back.
	relayTo := w
	if len(toolNames) > 0 {
		restore := newNameRestorer(w, toolNames)
		defer func() { _ = restore.Flush() }()
		relayTo = &restoringWriter{ResponseWriter: w, body: restore}
	}

	tr := ledger.AnthropicTracker{Capture: capture}
	relayErr := relay(relayTo, resp, &tr)
	leg.Latency = time.Since(start)
	if t.config.Type == provider.AnthropicSubscription {
		s.saveRateLimits(context.WithoutCancel(r.Context()), resp.Header)
	}
	leg.Usage, leg.StopReason, leg.HTTPStatus = tr.Usage, tr.StopReason, resp.StatusCode
	leg.Advisors = tr.Advisors
	leg.Output = tr.Message()
	switch {
	case relayErr != nil && r.Context().Err() != nil:
		leg.Status = ledger.StatusCanceled
	case errors.Is(relayErr, errStreamTruncated):
		leg.Status, leg.Error = ledger.StatusUpstreamError, relayErr.Error()
		failHeld(w, relayErr.Error())
	case relayErr != nil:
		leg.Status, leg.Error = ledger.StatusError, relayErr.Error()
	case resp.StatusCode >= 300 || tr.Error != "":
		leg.Status, leg.Error = ledger.StatusUpstreamError, tr.Error
	case !tr.Usable():
		// The upstream returned a 2xx that is not an answer: truncated stream,
		// empty body, or a refusal with no content. Combo retries on the next
		// member; direct routes pass the upstream's 200 through.
		if tr.Refused {
			leg.Error = "refusal: " + tr.RefusalDetail
		} else if !tr.Completed {
			leg.Error = "upstream stream ended without message_stop"
		} else {
			leg.Error = "upstream returned an empty response"
		}
		leg.Status = ledger.StatusUpstreamError
		failHeld(w, leg.Error)
	default:
		leg.Status = ledger.StatusOK
	}
	// Releasing a held response is the caller's job: a combo decides between
	// the next member and the client, and the context fitter decides between
	// a smaller tier and the client. Releasing here would take that away.
	return leg
}

// failHeld turns a writer's held 2xx into an error, so a combo's retryable()
// reports true and the next member gets the request. It is a no-op for a
// writer that does not hold responses, or one that already passed content
// through: the client has already seen part of an answer.
func failHeld(w http.ResponseWriter, reason string) {
	if f, ok := w.(interface{ fail(string) }); ok {
		f.fail(reason)
	}
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
				if !tr.Completed && tr.Error == "" {
					return errStreamTruncated
				}
				return nil
			}
			if err != nil {
				return err
			}
			tr.Event(ev.Name, ev.Data)
			// Once part of the answer has reached the client, another member
			// can no longer take over: the client would see two answers spliced.
			if tr.Content {
				if r, ok := w.(streamReleaser); ok {
					r.releaseOnContent()
				}
			}
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

// saveRateLimits keeps the latest subscription rate-limit headers for the dashboard.
func (s *Server) saveRateLimits(ctx context.Context, h http.Header) {
	limits := make(map[string]string)
	for name, values := range h {
		if lower := strings.ToLower(name); strings.HasPrefix(lower, "anthropic-ratelimit-") && len(values) > 0 {
			limits[lower] = values[0]
		}
	}
	if len(limits) == 0 {
		return
	}
	b, err := json.Marshal(struct {
		CapturedAt int64             `json:"captured_at"`
		Headers    map[string]string `json:"headers"`
	}{time.Now().UnixMilli(), limits})
	if err == nil {
		err = s.store.SetSetting(ctx, store.SubscriptionLimitsSetting, string(b))
	}
	if err != nil {
		s.log.Warn("save subscription rate limits", "err", err)
	}
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
