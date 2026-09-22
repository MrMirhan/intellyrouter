package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/MrMirhan/intellyrouter/internal/ledger"
	"github.com/MrMirhan/intellyrouter/internal/store"
)

// defaultTokensPerByte estimates a session's first request. JSON with code
// averages a little over three bytes per token; a high estimate only moves a
// step up one tier, while a low one costs a failed call.
const defaultTokensPerByte = 0.3

// maxSizeEntries is the size at which learn removes expired sessions.
const maxSizeEntries = 10_000

// sizeTracker learns each session's tokens per byte from the usage that
// upstreams report, so the gateway can estimate the next request's size.
type sizeTracker struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]sizeEntry
}

type sizeEntry struct {
	tokensPerByte float64
	expires       time.Time
}

func newSizeTracker(ttl time.Duration) *sizeTracker {
	return &sizeTracker{ttl: ttl, entries: make(map[string]sizeEntry)}
}

func (t *sizeTracker) estimate(key string, size int) int64 {
	ratio := defaultTokensPerByte
	t.mu.Lock()
	if e, ok := t.entries[key]; ok && time.Now().Before(e.expires) {
		ratio = e.tokensPerByte
	}
	t.mu.Unlock()
	return int64(float64(size) * ratio)
}

func (t *sizeTracker) learn(key string, size int, tokens int64) {
	if size <= 0 || tokens <= 0 {
		return
	}
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries[key] = sizeEntry{tokensPerByte: float64(tokens) / float64(size), expires: now.Add(t.ttl)}
	if len(t.entries) > maxSizeEntries {
		for k, e := range t.entries {
			if now.After(e.expires) {
				delete(t.entries, k)
			}
		}
	}
}

// fits reports whether about tokens fit the model's context window, with room
// for the reply. A model without a context size fits.
func fits(m store.Model, tokens int64) bool {
	return m.Context <= 0 || tokens <= m.Context*95/100
}

// acceptsImages reports whether the model takes image blocks. A combo row has
// no vision flag of its own, so any member that takes images makes it accept them.
func (s *Server) acceptsImages(ctx context.Context, m store.Model) bool {
	if m.Vision {
		return true
	}
	vision, err := s.store.ComboVision(ctx, m.ID)
	return err == nil && vision
}

// fittingTier returns the first tier from start whose model fits the request,
// and a note when it is not start. A tier qualifies when its context window
// holds the estimate and, when needsVision is true, it accepts images. When no
// tier qualifies, it returns the last tier.
func (s *Server) fittingTier(ctx context.Context, tiers []store.Tier, start int, estimate int64, needsVision bool) (int, string, error) {
	var first store.Model
	firstVision := false
	for i := start; i < len(tiers); i++ {
		m, err := s.store.GetModel(ctx, tiers[i].ModelID)
		if err != nil {
			return 0, "", err
		}
		vision := needsVision && s.acceptsImages(ctx, m)
		if i == start {
			first, firstVision = m, vision
		}
		if !fits(m, estimate) {
			continue
		}
		if needsVision && !vision {
			continue
		}
		if i == start {
			return i, "", nil
		}
		switch {
		case needsVision && !firstVision && !fits(first, estimate):
			return i, fmt.Sprintf("about %s tokens do not fit %s (%s); request has an image; moved to %s",
				kiloTokens(estimate), first.ModelID, kiloTokens(first.Context), tiers[i].Label), nil
		case needsVision && !firstVision:
			return i, fmt.Sprintf("request has an image; moved to %s", tiers[i].Label), nil
		case !fits(first, estimate):
			return i, fmt.Sprintf("about %s tokens do not fit %s (%s); moved to %s",
				kiloTokens(estimate), first.ModelID, kiloTokens(first.Context), tiers[i].Label), nil
		}
	}
	// No tier from start takes the image, so a lower tier that does serves this
	// request. The turn keeps its own tier.
	if needsVision {
		for i := start - 1; i >= 0; i-- {
			m, err := s.store.GetModel(ctx, tiers[i].ModelID)
			if err != nil {
				return 0, "", err
			}
			if fits(m, estimate) && s.acceptsImages(ctx, m) {
				return i, fmt.Sprintf("request has an image; moved down to %s", tiers[i].Label), nil
			}
		}
	}
	last := len(tiers) - 1
	lastModel, _ := s.store.GetModel(ctx, tiers[last].ModelID)
	switch {
	case needsVision && s.acceptsImages(ctx, lastModel):
		return last, fmt.Sprintf("request has an image; used %s", tiers[last].Label), nil
	case needsVision:
		return last, fmt.Sprintf("request has an image but no tier supports images; used %s", tiers[last].Label), nil
	}
	return last, fmt.Sprintf("about %s tokens fit no tier; used %s", kiloTokens(estimate), tiers[last].Label), nil
}

// largerTier returns the first tier after idx whose model has a larger or an
// unknown context window than the model that overflowed.
func (s *Server) largerTier(ctx context.Context, tiers []store.Tier, idx int, failed store.Model) (int, target, bool) {
	for i := idx + 1; i < len(tiers); i++ {
		t, err := s.resolve(ctx, tiers[i].ModelID)
		if err != nil {
			continue
		}
		if t.model.Context <= 0 || failed.Context <= 0 || t.model.Context > failed.Context {
			return i, t, true
		}
	}
	return 0, target{}, false
}

// callWithFallback calls the tier's model. When the upstream rejects the
// prompt as too long for its context window, the client does not see that
// error: the gateway sends the request to the next larger tier. It returns
// every leg, the last one being the call that answered the client.
func (s *Server) callWithFallback(w http.ResponseWriter, r *http.Request, tiers []store.Tier, idx int, t target, sizeKey string, request func(target) clientRequest) []ledger.Leg {
	var legs []ledger.Leg
	for {
		cr := request(t)
		held := newErrorBuffer(w)
		leg := s.call(held, r, t, cr)
		if len(legs) > 0 {
			leg.Note = joinNote("tier "+tiers[idx].Label+" after a context overflow", leg.Note)
		}
		if leg.Status == ledger.StatusOK {
			s.sizes.learn(sizeKey, len(cr.body), leg.Usage.Input+leg.Usage.CacheRead+leg.Usage.CacheWrite)
		}
		if held.overflow() {
			if next, nextTarget, ok := s.largerTier(r.Context(), tiers, idx, t.model); ok {
				leg.Note = joinNote(leg.Note, "prompt too long for "+t.model.ModelID+"; sent to "+tiers[next].Label)
				legs = append(legs, leg)
				idx, t = next, nextTarget
				continue
			}
		}
		held.release()
		return append(legs, leg)
	}
}

var contextOverflow = regexp.MustCompile(`(?i)prompt is too long|context (?:length|window)|maximum context|too many (?:input )?tokens|input is too long|reduce the length`)

// modelUnavailable matches a provider reporting the model itself as down,
// such as "Error from provider (Console): Upstream request failed: Model is
// unavailable." It arrives as a 400 from some providers, so it needs its own
// check next to contextOverflow: the request is not too big, the model just
// cannot answer right now, and another combo member usually can.
var modelUnavailable = regexp.MustCompile(`(?i)model is unavailable`)

// errorBuffer holds responses so the gateway can decide whether to send them
// to the client. An error response (status >= 400) is held until release().
// A 2xx response is also held, so the gateway can fail over to another combo
// member when the upstream returned a truncated stream, a refusal, or any
// other shape that is not a usable answer.
type errorBuffer struct {
	w      http.ResponseWriter
	rc     *http.ResponseController
	header http.Header
	status int
	passed bool
	body   bytes.Buffer
	// heldGood is the 2xx response body when the upstream wrote one but the
	// gateway has not yet decided whether to release it.
	heldGood []byte
	// watchdogArmed marks a held buffer whose caller wants the relay to fail
	// over when the upstream produces no visible content for maxComboHold.
	watchdogArmed bool
}

func newErrorBuffer(w http.ResponseWriter) *errorBuffer {
	return &errorBuffer{w: w, rc: http.NewResponseController(w), header: make(http.Header)}
}

// armHoldWatchdog asks the relay to fail over when the held buffer has not
// released any visible content within maxComboHold. Combo non-last members
// use it; the last member has nobody to fail over to, so a slow-but-eventual
// answer must not be cut off.
func (b *errorBuffer) armHoldWatchdog() { b.watchdogArmed = true }

// armed is the getter the relay uses to decide whether to arm the hold
// watchdog on this writer.
func (b *errorBuffer) armed() bool { return b.watchdogArmed }

func (b *errorBuffer) Header() http.Header {
	if b.passed {
		return b.w.Header()
	}
	return b.header
}

func (b *errorBuffer) WriteHeader(code int) {
	if b.status != 0 {
		return
	}
	b.status = code
	if code >= http.StatusBadRequest {
		return
	}
	// 2xx: hold. releaseGood() sends it; releaseBad() converts it to an error
	// so the combo can retry the next member.
}

func (b *errorBuffer) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.WriteHeader(http.StatusOK)
	}
	if b.passed {
		return b.w.Write(p)
	}
	if b.status >= http.StatusBadRequest {
		// Error bodies are short; the limit keeps a broken upstream from filling memory.
		if b.body.Len() < 1<<20 {
			b.body.Write(p)
		}
		return len(p), nil
	}
	// 2xx: append to the held buffer.
	b.heldGood = append(b.heldGood, p...)
	return len(p), nil
}

// releaseOnContent commits a held 2xx response once the first content delta
// has arrived: the client has seen part of an answer, so no other combo
// member can take over from here.
func (b *errorBuffer) releaseOnContent() { b.release() }

// release sends the held response to the client: the 2xx body if the gateway
// is done trying other members, or the held error otherwise.
func (b *errorBuffer) release() {
	if b.passed || b.status == 0 {
		return
	}
	maps.Copy(b.w.Header(), b.header)
	b.w.WriteHeader(b.status)
	if b.status < http.StatusBadRequest {
		_, _ = b.w.Write(b.heldGood)
	} else {
		_, _ = b.w.Write(b.body.Bytes())
	}
	b.passed = true
}

// fail turns a held 2xx into an error with the given reason, so retryable()
// reports true and the combo tries the next member. Call it only while the
// response is still held (status < 400, not yet released).
func (b *errorBuffer) fail(reason string) {
	if b.passed || b.status >= http.StatusBadRequest {
		return
	}
	b.heldGood = nil
	b.status = http.StatusBadGateway
	b.header.Set("Content-Type", "application/json")
	b.body.Reset()
	b.body.WriteString(`{"type":"error","error":{"type":"api_error","message":"`)
	b.body.WriteString(reason)
	b.body.WriteString(`"}}`)
}

// FlushError lets relay flush a passed-through stream.
func (b *errorBuffer) FlushError() error {
	if b.passed {
		return b.rc.Flush()
	}
	return nil
}

// overflow reports whether the held error says that the prompt is too long.
func (b *errorBuffer) overflow() bool {
	return !b.passed && (b.status == http.StatusBadRequest || b.status == http.StatusRequestEntityTooLarge) &&
		contextOverflow.Match(b.body.Bytes())
}

// retryable reports whether another model may answer the request. The upstream
// may have failed with a retryable status, or returned a 2xx that the gateway
// has already converted into an error via fail.
func (b *errorBuffer) retryable() bool {
	if b.passed || b.status == 0 {
		return false
	}
	switch b.status {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge:
		return b.overflow() || b.modelUnavailable() || b.providerServerError()
	case http.StatusUnprocessableEntity:
		return false
	}
	return b.status > http.StatusBadRequest
}

func (b *errorBuffer) modelUnavailable() bool {
	return modelUnavailable.Match(b.body.Bytes())
}

// providerServerError reads the error body as JSON and reports whether its
// `error.type` says the failure was the provider's own, not a problem with
// the request. Such a body can carry a 400 status that no other rule matches.
func (b *errorBuffer) providerServerError() bool {
	var body struct {
		Type  string `json:"type"`
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(b.body.Bytes(), &body) != nil {
		return false
	}
	return body.Type == "server_error" || body.Error.Type == "server_error"
}

func kiloTokens(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%dK", n/1000)
}
