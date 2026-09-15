package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"

	"intellyrouter/internal/claudecli"
	"intellyrouter/internal/guided"
	"intellyrouter/internal/ledger"
	"intellyrouter/internal/provider"
)

const (
	// maxClaudeCodeConsults limits the Claude Code processes that run at the
	// same time.
	maxClaudeCodeConsults = 2
	// claudeCodeIdle closes a director conversation nobody consulted for this
	// long; Claude Code keeps its prompt cache for one hour.
	claudeCodeIdle = time.Hour
	// Claude Code opens a context window larger than 200K only for a model
	// name with this suffix.
	claudeCodeLongContext = "[1m]"
)

// claudeCode asks subscription directors through the Claude Code CLI on this
// machine. Claude Code uses the user's own login; the gateway only starts the
// process and reads its answer.
type claudeCode struct {
	binary string
	dir    string
	slots  chan struct{}

	mu       sync.Mutex
	sessions map[string]*claudeSession
}

// claudeSession is one director conversation in Claude Code for one executor
// session. The director reads each transcript block once: later consults
// resume the conversation with only the new blocks, and Claude Code reads the
// rest from its prompt cache.
type claudeSession struct {
	mu     sync.Mutex
	id     string
	setup  [32]byte
	blocks [][32]byte
	used   time.Time // guarded by claudeCode.mu
}

// UseClaudeCode lets guided routes ask a subscription director through the
// Claude Code binary. Director conversations run in dir.
func (s *Server) UseClaudeCode(binary, dir string) {
	s.claude = &claudeCode{binary: binary, dir: dir, slots: make(chan struct{}, maxClaudeCodeConsults), sessions: map[string]*claudeSession{}}
}

func (s *Server) viaClaudeCode(ds guided.DirectorSettings, director target) bool {
	return ds.ClaudeCode && s.claude != nil && director.config.Type == provider.AnthropicSubscription
}

// session returns the director conversation for key and closes idle ones.
func (c *claudeCode) session(key string) *claudeSession {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, cs := range c.sessions {
		if now.Sub(cs.used) > claudeCodeIdle && cs.mu.TryLock() {
			cs.close()
			cs.mu.Unlock()
			delete(c.sessions, k)
		}
	}
	cs := c.sessions[key]
	if cs == nil {
		cs = &claudeSession{}
		c.sessions[key] = cs
	}
	cs.used = now
	return cs
}

func (c *claudeCode) touch(cs *claudeSession) {
	c.mu.Lock()
	cs.used = time.Now()
	c.mu.Unlock()
}

func (cs *claudeSession) close() {
	if cs.id != "" {
		_ = claudecli.RemoveSession(cs.id)
	}
	cs.id, cs.blocks = "", nil
}

// resumeFrom is the first transcript block the director has not read, or 0
// when the conversation cannot continue: none exists, or the transcript no
// longer starts with what the director read.
func (cs *claudeSession) resumeFrom(setup [32]byte, blocks [][32]byte) int {
	if cs.id == "" || setup != cs.setup || len(blocks) < len(cs.blocks) {
		return 0
	}
	for i, h := range cs.blocks {
		if blocks[i] != h {
			// The last block can change when the next response joins it; the
			// director reads it again.
			if i == len(cs.blocks)-1 {
				return i
			}
			return 0
		}
	}
	return len(cs.blocks)
}

func transcriptHashes(t guided.Transcript) ([32]byte, [][32]byte) {
	blocks := make([][32]byte, len(t.Blocks))
	for i, b := range t.Blocks {
		blocks[i] = sha256.Sum256([]byte(b.Role + "\x00" + b.Text))
	}
	return sha256.Sum256([]byte(t.Setup)), blocks
}

// consultViaClaudeCode asks a director or an advisor on the subscription
// through the Claude Code CLI and records the call as a subscription leg. key
// names the conversation: the role and the executor session.
func (s *Server) consultViaClaudeCode(ctx context.Context, asked target, body []byte, key, effort, system string, dec guided.Decision, previous string, capture bool) (string, bool, ledger.Leg) {
	leg := asked.newLeg()
	leg.Note = "via Claude Code"
	t, err := guided.ReadTranscript(body)
	if err != nil {
		leg.Status, leg.Error = ledger.StatusError, err.Error()
		return "", false, leg
	}
	model := asked.model.ModelID
	if asked.model.Context > 200_000 {
		model += claudeCodeLongContext
	}
	cs := s.claude.session(key + "\x00" + model)
	cs.mu.Lock()
	defer cs.mu.Unlock()
	select {
	case s.claude.slots <- struct{}{}:
		defer func() { <-s.claude.slots }()
	case <-ctx.Done():
		leg.Status, leg.Error = ledger.StatusCanceled, ctx.Err().Error()
		return "", false, leg
	}

	setup, blocks := transcriptHashes(t)
	opts := claudecli.Options{
		Binary: s.claude.binary, Dir: s.claude.dir, Model: model, Effort: effort,
		AppendSystemPrompt: system,
	}
	start := time.Now()
	var res claudecli.Result
	from := cs.resumeFrom(setup, blocks)
	if from > 0 {
		// The conversation already holds the previous guidance.
		opts.SessionID, opts.Resume, opts.Input = cs.id, true, t.Prompt(from, dec.Reason, dec.Detail, "")
		if res, err = claudecli.Run(ctx, opts); err == nil {
			leg.Note += ", resumed"
		}
	}
	if from == 0 || err != nil && ctx.Err() == nil {
		cs.close()
		cs.id = uuid.NewString()
		opts.SessionID, opts.Resume, opts.Input = cs.id, false, t.Prompt(0, dec.Reason, dec.Detail, previous)
		res, err = claudecli.Run(ctx, opts)
	}
	leg.Latency = time.Since(start)
	if err != nil {
		cs.close()
		leg.Status, leg.Error = ledger.StatusUpstreamError, err.Error()
		return "", false, leg
	}
	cs.setup, cs.blocks = setup, blocks
	s.claude.touch(cs)

	u := res.Usage
	leg.Usage = ledger.Usage{Input: u.Input, Output: u.Output, CacheRead: u.CacheRead, CacheWrite: u.CacheWrite, CacheWrite1h: u.CacheWrite1h}
	if capture {
		leg.Output, _ = json.Marshal(map[string]any{
			"type": "message", "role": "assistant", "model": asked.model.ModelID, "stop_reason": "end_turn",
			"content": []map[string]string{{"type": "text", "text": res.Text}},
		})
	}
	guidance, approved, err := guided.GuidanceText(res.Text)
	if err != nil {
		leg.Status, leg.Error = ledger.StatusError, err.Error()
		return "", false, leg
	}
	leg.Status = ledger.StatusOK
	return guidance, approved, leg
}
