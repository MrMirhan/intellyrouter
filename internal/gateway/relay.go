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
	"sync"
	"sync/atomic"
	"time"

	"github.com/MrMirhan/intellyrouter/internal/ledger"
	"github.com/MrMirhan/intellyrouter/internal/provider"
	"github.com/MrMirhan/intellyrouter/internal/sse"
	"github.com/MrMirhan/intellyrouter/internal/store"
	"github.com/MrMirhan/intellyrouter/internal/translate"
)

const maxResponseBytes = 64 << 20

// MaxComboHold caps how long a held writer may stream without producing any
// visible content. A 9router leg that thinks slowly, or a wrapper that
// buffers the whole reply, can otherwise hold the buffer for minutes while
// the client sees nothing. Exported so tests can shorten it.
var MaxComboHold = 60 * time.Second

// errStreamTruncated reports a stream that ended without its message_stop
// event. The client saw part of an answer and has no way to know it was cut
// off; the combo moves to the next member when no content reached it.
var errStreamTruncated = errors.New("upstream stream ended without message_stop")

// errStreamStalled reports a stream the watchdog closed because the held
// writer produced no visible content before MaxComboHold. Treated like
// truncation: the combo moves to the next member.
var errStreamStalled = errors.New("upstream produced no answer before the hold deadline")

// streamReleaser is implemented by writers that hold a 2xx response while the
// upstream streams it. The relay calls releaseOnContent when the first
// content delta arrives, so the held bytes are flushed and subsequent bytes
// are passed straight through.
type streamReleaser interface{ releaseOnContent() }

// streamFailer lets the relay convert a held 2xx into a retryable error when
// the upstream stalls past the hold deadline, so the combo can try the next
// member instead of letting the client wait in silence.
type streamFailer interface{ fail(string) }

// streamArmer reports whether the held writer wants the relay to fail over
// when the upstream produces no visible content within MaxComboHold. Combo
// non-last members arm it; the last member does not.
type streamArmer interface{ armed() bool }

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
	relayErr := relay(relayTo, resp, &tr, t.model.ModelID)
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
	case errors.Is(relayErr, errStreamTruncated), errors.Is(relayErr, errStreamStalled):
		reason := relayErr.Error()
		leg.Status, leg.Error = ledger.StatusUpstreamError, reason
		failHeld(w, reason)
	case relayErr != nil:
		leg.Status, leg.Error = ledger.StatusError, relayErr.Error()
	case resp.StatusCode >= 300 || tr.Error != "":
		leg.Status, leg.Error = ledger.StatusUpstreamError, tr.Error
		// A 200 stream that ends with an `error` event should not reach the
		// client as a half-broken answer. If the buffer is still held, turn
		// the held 2xx into a 502 so the combo retries the next member.
		failHeld(w, leg.Error)
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
// When the writer holds the response (a combo member or a tier under the
// context fitter) the relay arms a watchdog: if no visible content has
// reached the client within maxComboHold, the body is closed so the read
// unblocks and the held response is converted to a retryable 502.
func relay(w http.ResponseWriter, resp *http.Response, tr *ledger.AnthropicTracker, model string) error {
	for name, values := range resp.Header {
		if !skipResponseHeaders[name] {
			w.Header()[name] = values
		}
	}
	if resp.StatusCode != http.StatusOK || !isEventStream(resp.Header) {
		b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		if err != nil {
			return err
		}
		// Some Anthropic-shaped endpoints answer a non-streamed request in the
		// OpenAI Chat Completions shape instead. Response rejects a body without
		// choices, so a real Anthropic message is left untouched.
		if resp.StatusCode == http.StatusOK {
			if conv, cerr := translate.Response(b, model); cerr == nil {
				b = conv
			}
		}
		w.WriteHeader(resp.StatusCode)
		tr.Response(resp.StatusCode, b)
		_, err = w.Write(b)
		return err
	}
	w.WriteHeader(resp.StatusCode)
	events := sse.NewReader(io.TeeReader(resp.Body, flushWriter{w: w, rc: http.NewResponseController(w)}))

	// Idle watchdog for held writers. The timer only sets an atomic flag and
	// asks the body to close: the synchronous read loop below sees both, the
	// watchdog never touches the held buffer or the relay loop, so there are
	// no shared-state races. Only combo non-last members arm it; the last
	// member has nobody to fail over to, so a slow-but-eventual answer must
	// not be cut off.
	var stalled atomic.Bool
	var closeOnce sync.Once
	closeBody := func() { closeOnce.Do(func() { resp.Body.Close() }) }
	var timer *time.Timer
	armed := false
	if a, ok := w.(streamArmer); ok && a.armed() {
		armed = true
		timer = time.AfterFunc(MaxComboHold, func() {
			stalled.Store(true)
			closeBody()
		})
	}
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		ev, err := events.Next()
		if err == io.EOF {
			if !tr.Completed && tr.Error == "" {
				if stalled.Load() {
					return errStreamStalled
				}
				return errStreamTruncated
			}
			return nil
		}
		if err != nil {
			if stalled.Load() {
				return errStreamStalled
			}
			return err
		}
		tr.Event(ev.Name, ev.Data)
		// Once part of the answer has reached the client, another member can
		// no longer take over: the client would see two answers spliced.
		// tr.Content only turns true on a visible (text/tool) delta — the
		// tracker gates thinking and signature deltas out — so a thinking-
		// only stream that errors mid-flight still fails over.
		if tr.Content {
			if r, ok := w.(streamReleaser); ok {
				r.releaseOnContent()
			}
			if timer != nil {
				timer.Stop()
				timer = nil
			}
		} else if armed && timer != nil {
			// Each event resets the idle deadline so a long-thinking stream
			// keeps its buffer alive as long as events keep arriving.
			timer.Reset(MaxComboHold)
		}
	}
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
