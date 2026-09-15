package gateway

import (
	"strings"
	"sync"
	"time"
)

// signatureCache keeps Gemini thought signatures by tool_use ID. Claude Code
// sends tool calls back without them, and Gemini 3 rejects a replayed function
// call that has no signature.
type signatureCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]signatureEntry
}

type signatureEntry struct {
	signature string
	expires   time.Time
}

// maxSignatures is the size at which put removes expired entries.
const maxSignatures = 50_000

func newSignatureCache(ttl time.Duration) *signatureCache {
	return &signatureCache{ttl: ttl, entries: make(map[string]signatureEntry)}
}

func (c *signatureCache) get(toolUseID string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[toolUseID]
	if !ok || time.Now().After(e.expires) {
		return ""
	}
	return e.signature
}

func (c *signatureCache) put(signatures map[string]string) {
	if len(signatures) == 0 {
		return
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, sig := range signatures {
		c.entries[id] = signatureEntry{signature: sig, expires: now.Add(c.ttl)}
	}
	if len(c.entries) > maxSignatures {
		for id, e := range c.entries {
			if now.After(e.expires) {
				delete(c.entries, id)
			}
		}
	}
}

// needsThoughtSignatures reports whether a Chat Completions upstream is Gemini.
func needsThoughtSignatures(t target) bool {
	return strings.Contains(strings.ToLower(t.model.ModelID), "gemini") ||
		strings.Contains(t.provider.BaseURL, "googleapis.com")
}
