package gateway

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/MrMirhan/intellyrouter/internal/guided"
	"github.com/MrMirhan/intellyrouter/internal/ledger"
	"github.com/MrMirhan/intellyrouter/internal/provider"
	"github.com/MrMirhan/intellyrouter/internal/store"
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

	if dec.Reason != "" && director.config.Type == provider.AnthropicSubscription && !s.viaClaudeCode(settings.Director, director) {
		// The director takes the review step itself, so the turn counts as reviewed;
		// otherwise every later passing step would hand the work to it again.
		if dec.Reason == guided.ReasonReview {
			st.Reviewed = true
		}
		s.guidedTurns.Put(key, st)
		leg := s.call(w, r, director, cr)
		leg.Role = ledger.RoleDirectorStep
		leg.Note = joinNote("director step: "+checkpoint, leg.Note)
		e.Legs = append(e.Legs, leg)
		e.Finish(leg.Status, leg.HTTPStatus, leg.Error)
		return
	}
	if dec.Reason != "" {
		guidance, approved, leg := s.consultDirector(r.Context(), director, cr.body, e.SessionID+"\x00"+e.AgentID, settings.Director, dec, st.Guidance, cr.capture)
		leg.Role = ledger.RoleDirector
		leg.Note = joinNote("checkpoint: "+checkpoint, leg.Note)
		if cr.capture {
			leg.Input = "Checkpoint: " + checkpoint
		}
		e.Legs = append(e.Legs, leg)
		if leg.Status == ledger.StatusOK {
			st.Guidance, st.GuidanceReason, st.GuidanceSent = guidance, dec.Reason, false
			st.Reviewed = st.Reviewed || (dec.Reason == guided.ReasonReview && approved)
		}
	}
	s.guidedTurns.Put(key, st)

	sizeKey := e.SessionID + "\x00" + e.AgentID
	tierIndex, fitNote, err := s.fittingTier(r.Context(), route.Tiers, dec.Tier, s.sizes.estimate(sizeKey, len(cr.body)), guided.HasImage(cr.body))
	if err != nil {
		fail(http.StatusServiceUnavailable, "api_error", "route models unavailable: "+err.Error())
		return
	}
	tier := route.Tiers[tierIndex]
	executor, err := s.resolve(r.Context(), tier.ModelID)
	if err != nil {
		fail(http.StatusServiceUnavailable, "api_error", fmt.Sprintf("tier %s unavailable: %v", tier.Label, err))
		return
	}
	body := cr.body
	note := "tier " + tier.Label
	added := ""
	if dec.Escalated {
		note += " (moved up after repeated failures)"
	}
	if fitNote != "" {
		note += "; " + fitNote
	}
	switch {
	case st.Guidance == "":
	case executor.subscription():
		note += "; guidance not added to a subscription request"
	default:
		if injected, err := guided.InjectGuidance(body, st.Guidance, st.GuidanceReason, st.GuidanceSent); err == nil {
			body, added = injected, st.Guidance
			note += "; guidance from " + st.GuidanceReason
			if !st.GuidanceSent {
				st.GuidanceSent = true
				s.guidedTurns.Put(key, st)
			}
		} else {
			note += "; guidance not added: " + err.Error()
		}
	}
	if st.Advice != "" && !executor.subscription() {
		if injected, err := guided.InjectAdvice(body, st.AdviceQuestion, st.Advice); err == nil {
			body = injected
			if added != "" {
				added += "\n\n"
			}
			added += st.Advice
			note += "; advisor answer from earlier in the turn"
		}
	}
	exec := clientRequest{body: body, stream: cr.stream, claudeAuth: cr.claudeAuth, capture: cr.capture}
	run := consultRun{executor: executor, key: key, state: st, note: note, guidance: added, legRole: ledger.RoleExecutor}
	if c := s.advisorFor(cr, executor); c != nil {
		run.consultant = *c
	} else if cr.advisor == nil && settings.Consult && cr.stream && turn.HasTools &&
		(director.config.Type != provider.AnthropicSubscription || s.viaClaudeCode(settings.Director, director)) && !executor.subscription() {
		run.consultant = consultant{role: guided.RoleDirector, target: director, maxCalls: settings.Director.MaxCallsPerTurn, director: settings.Director}
	}
	if run.role != "" {
		if withTool, ok := withConsultTool(body, run.role); ok {
			exec.body = withTool
			s.runWithConsult(w, r, e, exec, run)
			return
		}
	}
	legs := s.callWithFallback(w, r, route.Tiers, tierIndex, executor, sizeKey, func(t target) clientRequest {
		// Guidance is never added to a subscription request.
		if t.subscription() {
			return cr
		}
		return exec
	})
	legs[0].Note = joinNote(note, legs[0].Note)
	for i := range legs {
		legs[i].Role = ledger.RoleExecutor
		if cr.capture && legs[i].Model == executor.model.ModelID {
			legs[i].Input = added
		}
	}
	e.Legs = append(e.Legs, legs...)
	last := legs[len(legs)-1]
	e.Finish(last.Status, last.HTTPStatus, last.Error)
}

// maxQuestionsPerRequest limits the hidden continuations of one client request.
const maxQuestionsPerRequest = 3

// consultant answers the executor's hidden questions: a guided route's
// director or the route's advisor.
type consultant struct {
	role     string
	target   target
	effort   string
	maxCalls int
	// director holds the settings of a director.
	director guided.DirectorSettings
}

type consultRun struct {
	consultant
	executor target
	key      string
	state    guided.State
	note     string
	legRole  string
	// guidance is the director or advisor text already added to the executor's request.
	guidance string
}

func (run *consultRun) callsLeft() bool {
	if run.role == guided.RoleAdvisor {
		return run.state.AdvisorCalls < run.maxCalls
	}
	return run.state.DirectorCalls < run.maxCalls
}

// runWithConsult calls the executor with the consult tool of the run's role.
// The gateway answers each question and continues the executor's response, so
// Claude Code receives one message without the hidden exchange.
func (s *Server) runWithConsult(w http.ResponseWriter, r *http.Request, e *ledger.Entry, cr clientRequest, run consultRun) {
	cw := newConsultWriter(w, guided.ToolName(run.role))
	defer cw.keepAlive(pingInterval)()
	body, note, added := cr.body, run.note, run.guidance
	var leg ledger.Leg
	for questions := 0; ; questions++ {
		leg = s.call(cw, r, run.executor, clientRequest{body: body, stream: true, claudeAuth: cr.claudeAuth, capture: cr.capture})
		leg.Role = run.legRole
		leg.Note = joinNote(note, leg.Note)
		if cr.capture {
			leg.Input = added
		}
		e.Legs = append(e.Legs, leg)
		call, ok := cw.endSegment()
		if !ok {
			break
		}
		answer := fmt.Sprintf("The %s cannot answer more questions in this turn. Continue with your best judgment.", run.role)
		answered := questions < maxQuestionsPerRequest && run.callsLeft()
		if answered {
			answer = s.answerQuestion(r.Context(), e, body, call, &run, cr.capture)
		}
		if call.clientTools {
			// Claude Code runs the other tools; the answer reaches the executor in its next request.
			cw.release()
			break
		}
		next, err := guided.AppendConsultAnswer(body, run.role, call.text, call.question, answer)
		if err == nil && !answered {
			// Without the tool, the executor has to finish the response.
			next, err = guided.RemoveConsultTool(next, run.role)
		}
		if err != nil {
			cw.fail(fmt.Sprintf("cannot continue after the %s's answer: %v", run.role, err))
			leg.Status, leg.Error = ledger.StatusError, err.Error()
			break
		}
		body, added = next, answer
		note = joinNote(run.note, fmt.Sprintf("continued after the %s's answer", run.role))
		cw.continueSegment()
	}
	e.Finish(leg.Status, leg.HTTPStatus, leg.Error)
}

// answerQuestion asks the run's director or advisor an executor's question and
// keeps the answer for the rest of the turn.
func (s *Server) answerQuestion(ctx context.Context, e *ledger.Entry, body []byte, call consultCall, run *consultRun, capture bool) string {
	defer func() { s.guidedTurns.Put(run.key, run.state) }()
	transcript := body
	if call.text != "" {
		if withText, err := guided.AppendAssistantText(body, call.text); err == nil {
			transcript = withText
		}
	}
	session := e.SessionID + "\x00" + e.AgentID
	var answer string
	var leg ledger.Leg
	if run.role == guided.RoleAdvisor {
		run.state.AdvisorCalls++
		answer, leg = s.consultAdvisor(ctx, run.consultant, transcript, session, call.question, capture)
		leg.Role = ledger.RoleAdvisor
	} else {
		run.state.DirectorCalls++
		dec := guided.Decision{Reason: guided.ReasonQuestion, Detail: call.question}
		answer, _, leg = s.consultDirector(ctx, run.target, transcript, session, run.director, dec, run.state.Guidance, capture)
		leg.Role = ledger.RoleDirector
	}
	leg.Note = joinNote("question: "+clip(call.question, 300), leg.Note)
	if capture {
		leg.Input = call.question
	}
	e.Legs = append(e.Legs, leg)
	if leg.Status != ledger.StatusOK {
		return fmt.Sprintf("The %s is not available now. Continue with your best judgment.", run.role)
	}
	if run.role == guided.RoleAdvisor {
		run.state.Advice, run.state.AdviceQuestion = answer, call.question
	} else {
		run.state.Guidance, run.state.GuidanceReason, run.state.GuidanceSent = answer, guided.ReasonQuestion, false
	}
	return answer
}

// consultDirector asks the director for guidance. A failed call leaves the
// previous guidance in place, so the executor keeps working.
func (s *Server) consultDirector(ctx context.Context, director target, body []byte, session string, ds guided.DirectorSettings, dec guided.Decision, previous string, capture bool) (string, bool, ledger.Leg) {
	ctx, cancel := context.WithTimeout(ctx, directorTimeout)
	defer cancel()
	if s.viaClaudeCode(ds, director) {
		return s.consultViaClaudeCode(ctx, director, body, guided.RoleDirector+"\x00"+session, ds.Effort, guided.DirectorSystemPrompt(), dec, previous, capture)
	}
	req, err := guided.DirectorRequest(body, director.model.ModelID, ds, dec.Reason, dec.Detail, previous)
	if err != nil {
		leg := director.newLeg()
		leg.Status, leg.Error = ledger.StatusError, err.Error()
		return "", false, leg
	}
	return s.askModel(ctx, director, req, capture)
}

// askModel sends a consult request to a model on an API key and reads the answer.
func (s *Server) askModel(ctx context.Context, t target, req []byte, capture bool) (string, bool, ledger.Leg) {
	start := time.Now()
	message, usage, served, err := s.complete(ctx, t, req)
	leg := served.newLeg()
	leg.Latency, leg.Usage = time.Since(start), usage
	if capture {
		leg.Output = message
	}
	if err != nil {
		leg.Status, leg.Error = ledger.StatusUpstreamError, err.Error()
		return "", false, leg
	}
	answer, approved, err := guided.ParseGuidance(message)
	if err != nil {
		leg.Status, leg.Error = ledger.StatusError, err.Error()
		return "", false, leg
	}
	leg.Status = ledger.StatusOK
	return answer, approved, leg
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
