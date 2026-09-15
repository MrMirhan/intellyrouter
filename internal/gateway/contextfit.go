package gateway

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"net/http"
	"regexp"
	"sync"
	"time"

	"intellyrouter/internal/ledger"
	"intellyrouter/internal/store"
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

// fittingTier returns the first tier from start whose model fits about
// estimate tokens, and a note when it is not start. When no tier fits, it
// returns the last tier.
func (s *Server) fittingTier(ctx context.Context, tiers []store.Tier, start int, estimate int64) (int, string, error) {
	var first store.Model
	for i := start; i < len(tiers); i++ {
		m, err := s.store.GetModel(ctx, tiers[i].ModelID)
		if err != nil {
			return 0, "", err
		}
		if i == start {
			first = m
		}
		if !fits(m, estimate) {
			continue
		}
		if i == start {
			return i, "", nil
		}
		return i, fmt.Sprintf("about %s tokens do not fit %s (%s); moved to %s",
			kiloTokens(estimate), first.ModelID, kiloTokens(first.Context), tiers[i].Label), nil
	}
	last := len(tiers) - 1
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

// errorBuffer passes a successful response through and holds an error
// response, so the caller can retry before the client sees the error.
type errorBuffer struct {
	w      http.ResponseWriter
	rc     *http.ResponseController
	header http.Header
	status int
	passed bool
	body   bytes.Buffer
}

func newErrorBuffer(w http.ResponseWriter) *errorBuffer {
	return &errorBuffer{w: w, rc: http.NewResponseController(w), header: make(http.Header)}
}

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
	if code < http.StatusBadRequest {
		maps.Copy(b.w.Header(), b.header)
		b.w.WriteHeader(code)
		b.passed = true
	}
}

func (b *errorBuffer) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.WriteHeader(http.StatusOK)
	}
	if b.passed {
		return b.w.Write(p)
	}
	// Error bodies are short; the limit keeps a broken upstream from filling memory.
	if b.body.Len() < 1<<20 {
		b.body.Write(p)
	}
	return len(p), nil
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

// retryable reports whether another model may answer the request that got the
// held error: the upstream failed, limited the rate, or rejected the key, the
// model, or the prompt's length. A request the upstream found invalid fails
// the same way everywhere.
func (b *errorBuffer) retryable() bool {
	if b.passed || b.status == 0 {
		return false
	}
	switch b.status {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge:
		return b.overflow()
	case http.StatusUnprocessableEntity:
		return false
	}
	return b.status > http.StatusBadRequest
}

// release sends a held error to the client.
func (b *errorBuffer) release() {
	if b.passed || b.status == 0 {
		return
	}
	maps.Copy(b.w.Header(), b.header)
	b.w.WriteHeader(b.status)
	_, _ = b.w.Write(b.body.Bytes())
}

func kiloTokens(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%dK", n/1000)
}
