package gateway

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"intellyrouter/internal/guided"
	"intellyrouter/internal/ledger"
	"intellyrouter/internal/provider"
	"intellyrouter/internal/store"
)

// A director with high effort can think for a while before it answers.
const directorTimeout = 3 * time.Minute

// guided lets the executor tiers do the work and asks the director model for
// guidance at checkpoints. The guidance is added to the executor's request, so
// Claude Code never sees it. The gateway cannot call a subscription model on
// its own, so a subscription director takes the checkpoint step itself.
func (s *Server) guided(w http.ResponseWriter, r *http.Request, route store.Route, cr clientRequest, e *ledger.Entry) {
	fail := func(status int, typ, msg string) {
		e.Finish(ledger.StatusError, status, msg)
		writeError(w, status, typ, msg)
	}
	settings, err := guided.ParseSettings(route.Settings)
	if err != nil {
		fail(http.StatusInternalServerError, "api_error", fmt.Sprintf("route %s has invalid settings: %v", route.Name, err))
		return
	}
	if len(route.Tiers) == 0 {
		fail(http.StatusServiceUnavailable, "api_error", fmt.Sprintf("route %s has no executor models", route.Name))
		return
	}
	director, err := s.resolve(r.Context(), settings.Director.ModelID)
	if err != nil {
		fail(http.StatusServiceUnavailable, "api_error", "director model unavailable: "+err.Error())
		return
	}
	turn, err := guided.Analyze(cr.body)
	if err != nil {
		fail(http.StatusBadRequest, "invalid_request_error", "cannot read messages: "+err.Error())
		return
	}
	// Savings compare the executors' tokens with the director doing all the work.
	ref := director.price()
	e.Reference = &ref

	key := e.SessionID + "\x00" + e.AgentID + "\x00" + turn.Key
	st := s.guidedTurns.Get(key)
	var dec guided.Decision
	if turn.HasTools {
		dec = settings.Plan(turn, &st, len(route.Tiers))
	}
	checkpoint := dec.Reason
	if dec.Detail != "" {
		checkpoint += " (" + dec.Detail + ")"
	}

	if dec.Reason != "" && director.config.Type == provider.AnthropicSubscription {
		s.guidedTurns.Put(key, st)
		leg := s.call(w, r, director, cr)
		leg.Role = ledger.RoleDirector
		leg.Note = joinNote("director step: "+checkpoint, leg.Note)
		e.Legs = append(e.Legs, leg)
		e.Finish(leg.Status, leg.HTTPStatus, leg.Error)
		return
	}
	if dec.Reason != "" {
		guidance, approved, leg := s.consultDirector(r.Context(), director, cr.body, settings.Director, dec, st.Guidance)
		leg.Role = ledger.RoleDirector
		leg.Note = joinNote("checkpoint: "+checkpoint, leg.Note)
		e.Legs = append(e.Legs, leg)
		if leg.Status == ledger.StatusOK {
			st.Guidance, st.GuidanceReason = guidance, dec.Reason
			st.Reviewed = st.Reviewed || (dec.Reason == guided.ReasonReview && approved)
		}
	}
	s.guidedTurns.Put(key, st)

	tier := route.Tiers[dec.Tier]
	executor, err := s.resolve(r.Context(), tier.ModelID)
	if err != nil {
		fail(http.StatusServiceUnavailable, "api_error", fmt.Sprintf("tier %s unavailable: %v", tier.Label, err))
		return
	}
	body := cr.body
	note := "tier " + tier.Label
	if dec.Escalated {
		note += " (moved up after repeated failures)"
	}
	switch {
	case st.Guidance == "":
	case executor.config.Type == provider.AnthropicSubscription:
		note += "; guidance not added to a subscription request"
	default:
		if injected, err := guided.InjectGuidance(body, st.Guidance, st.GuidanceReason); err == nil {
			body = injected
			note += "; guidance from " + st.GuidanceReason
		} else {
			note += "; guidance not added: " + err.Error()
		}
	}
	exec := clientRequest{body: body, stream: cr.stream, claudeAuth: cr.claudeAuth}
	if settings.Consult && cr.stream && turn.HasTools &&
		director.config.Type != provider.AnthropicSubscription && executor.config.Type != provider.AnthropicSubscription {
		if withTool, added, err := guided.AddConsultTool(body); err == nil && added {
			exec.body = withTool
			s.runWithConsult(w, r, e, exec, consultRun{settings: settings, director: director, executor: executor, key: key, state: st, note: note})
			return
		}
	}
	leg := s.call(w, r, executor, exec)
	leg.Role = ledger.RoleExecutor
	leg.Note = joinNote(note, leg.Note)
	e.Legs = append(e.Legs, leg)
	e.Finish(leg.Status, leg.HTTPStatus, leg.Error)
}

// maxQuestionsPerRequest limits the hidden continuations of one client request.
const maxQuestionsPerRequest = 3

const (
	answerLimit       = "The director cannot answer more questions in this turn. Continue with your best judgment."
	answerUnavailable = "The director is not available now. Continue with your best judgment."
)

type consultRun struct {
	settings guided.Settings
	director target
	executor target
	key      string
	state    guided.State
	note     string
}

// runWithConsult calls the executor with the ask_director tool. The gateway
// answers each question with the director and continues the executor's
// response, so Claude Code receives one message without the hidden exchange.
func (s *Server) runWithConsult(w http.ResponseWriter, r *http.Request, e *ledger.Entry, cr clientRequest, run consultRun) {
	cw := newConsultWriter(w)
	defer cw.keepAlive(pingInterval)()
	body, note := cr.body, run.note
	var leg ledger.Leg
	for questions := 0; ; questions++ {
		leg = s.call(cw, r, run.executor, clientRequest{body: body, stream: true, claudeAuth: cr.claudeAuth})
		leg.Role = ledger.RoleExecutor
		leg.Note = joinNote(note, leg.Note)
		e.Legs = append(e.Legs, leg)
		call, ok := cw.endSegment()
		if !ok {
			break
		}
		answer := answerLimit
		if questions < maxQuestionsPerRequest && run.state.DirectorCalls < run.settings.Director.MaxCallsPerTurn {
			answer = s.answerQuestion(r.Context(), e, body, call, &run)
		}
		if call.clientTools {
			// Claude Code runs the other tools; the answer reaches the executor as guidance next time.
			cw.release()
			break
		}
		next, err := guided.AppendConsultAnswer(body, call.text, call.question, answer)
		if err != nil {
			cw.fail("cannot continue after the director's answer: " + err.Error())
			leg.Status, leg.Error = ledger.StatusError, err.Error()
			break
		}
		body = next
		note = run.note + "; continued after the director's answer"
		cw.continueSegment()
	}
	e.Finish(leg.Status, leg.HTTPStatus, leg.Error)
}

// answerQuestion asks the director an executor's question and keeps the answer
// as guidance for the rest of the turn.
func (s *Server) answerQuestion(ctx context.Context, e *ledger.Entry, body []byte, call consultCall, run *consultRun) string {
	run.state.DirectorCalls++
	defer func() { s.guidedTurns.Put(run.key, run.state) }()
	transcript := body
	if call.text != "" {
		if withText, err := guided.AppendAssistantText(body, call.text); err == nil {
			transcript = withText
		}
	}
	dec := guided.Decision{Reason: guided.ReasonQuestion, Detail: call.question}
	guidance, _, leg := s.consultDirector(ctx, run.director, transcript, run.settings.Director, dec, run.state.Guidance)
	leg.Role = ledger.RoleDirector
	leg.Note = joinNote("question: "+clip(call.question, 300), leg.Note)
	e.Legs = append(e.Legs, leg)
	if leg.Status != ledger.StatusOK {
		return answerUnavailable
	}
	run.state.Guidance, run.state.GuidanceReason = guidance, guided.ReasonQuestion
	return guidance
}

// consultDirector asks the director for guidance. A failed call leaves the
// previous guidance in place, so the executor keeps working.
func (s *Server) consultDirector(ctx context.Context, director target, body []byte, ds guided.DirectorSettings, dec guided.Decision, previous string) (string, bool, ledger.Leg) {
	ctx, cancel := context.WithTimeout(ctx, directorTimeout)
	defer cancel()
	leg := director.newLeg()
	req, err := guided.DirectorRequest(body, director.model.ModelID, ds, dec.Reason, dec.Detail, previous)
	if err != nil {
		leg.Status, leg.Error = ledger.StatusError, err.Error()
		return "", false, leg
	}
	start := time.Now()
	message, usage, err := s.complete(ctx, director, req)
	leg.Latency, leg.Usage = time.Since(start), usage
	if err != nil {
		leg.Status, leg.Error = ledger.StatusUpstreamError, err.Error()
		return "", false, leg
	}
	guidance, approved, err := guided.ParseGuidance(message)
	if err != nil {
		leg.Status, leg.Error = ledger.StatusError, err.Error()
		return "", false, leg
	}
	leg.Status = ledger.StatusOK
	return guidance, approved, leg
}

func joinNote(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "; " + b
}
