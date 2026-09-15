package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"intellyrouter/internal/escalate"
	"intellyrouter/internal/guided"
	"intellyrouter/internal/jsonbytes"
	"intellyrouter/internal/ledger"
	"intellyrouter/internal/provider"
	"intellyrouter/internal/store"
)

// routeAdvisor is the advisor model a route chose. The gateway answers the
// executor's questions with it on any provider, and Claude Code's own advisor
// tool uses it on steps that Anthropic runs.
type routeAdvisor struct {
	settings store.RouteAdvisor
	target   target
}

func isAnthropic(t target) bool {
	return t.config.Type == provider.Anthropic || t.config.Type == provider.AnthropicSubscription
}

// loadRouteAdvisor reads the route's advisor: the model, when the route has an
// available one, and whether the route turns advisors off.
func (s *Server) loadRouteAdvisor(ctx context.Context, route store.Route) (*routeAdvisor, bool) {
	adv, err := store.ParseRouteAdvisor(route.Settings)
	if err != nil {
		s.log.Warn("read route advisor", "route", route.Name, "err", err)
		return nil, false
	}
	if adv.Off || adv.ModelID == 0 {
		return nil, adv.Off
	}
	t, err := s.resolve(ctx, adv.ModelID)
	if err != nil {
		s.log.Warn("route advisor unavailable", "route", route.Name, "err", err)
		return nil, false
	}
	return &routeAdvisor{settings: adv, target: t}, false
}

// applyRouteAdvisor puts the route's advisor choice into Claude Code's advisor
// tool. Off removes the tool, a Claude model replaces the model Claude Code
// asks for, and a model on another provider removes the tool, because only
// Anthropic runs it. A request without an advisor tool stays as it is.
func applyRouteAdvisor(adv *routeAdvisor, off bool, body []byte) ([]byte, error) {
	switch {
	case off || adv != nil && !isAnthropic(adv.target):
		out, _, err := removeAdvisorTools(body, "")
		return out, err
	case adv != nil:
		return setAdvisorModel(body, adv.target.model.ModelID)
	}
	return body, nil
}

// advisorFor returns the route's advisor as the consultant of a step, or nil
// when the step runs without it: the executor is on a subscription, whose
// response the gateway cannot continue on its own; the request does not
// stream or has no tools; or Anthropic runs Claude Code's advisor tool with
// the route's advisor on this step.
func (s *Server) advisorFor(cr clientRequest, executor target) *consultant {
	adv := cr.advisor
	switch {
	case adv == nil || !cr.stream || executor.subscription() || !guided.HasTools(cr.body):
		return nil
	case isAnthropic(executor) && isAnthropic(adv.target) && hasAdvisorTool(cr.body):
		return nil
	case adv.target.config.Type == provider.AnthropicSubscription && s.claude == nil:
		return nil
	}
	return &consultant{role: guided.RoleAdvisor, target: adv.target, effort: adv.settings.Effort, maxCalls: adv.settings.CallLimit()}
}

func hasAdvisorTool(body []byte) bool {
	_, found, err := removeAdvisorTools(body, "")
	return err == nil && found
}

// runWithAdvisor serves a direct or escalate step with the route's advisor
// answering the executor's questions. It returns false, and writes nothing to
// the client, when the step runs without the advisor. An empty turnKey is read
// from the request.
func (s *Server) runWithAdvisor(w http.ResponseWriter, r *http.Request, e *ledger.Entry, cr clientRequest, executor target, turnKey, legRole, note string) bool {
	c := s.advisorFor(cr, executor)
	if c == nil {
		return false
	}
	if turnKey == "" {
		turn, err := escalate.Analyze(cr.body)
		if err != nil {
			return false
		}
		turnKey = turn.Key
	}
	key := e.SessionID + "\x00" + e.AgentID + "\x00" + turnKey
	st := s.guidedTurns.Get(key)
	body, added := cr.body, ""
	if st.Advice != "" {
		if injected, err := guided.InjectAdvice(body, st.AdviceQuestion, st.Advice); err == nil {
			body, added = injected, st.Advice
			note = joinNote(note, "advisor answer from earlier in the turn")
		}
	}
	body, ok := withConsultTool(body, guided.RoleAdvisor)
	if !ok {
		return false
	}
	s.runWithConsult(w, r, e, clientRequest{body: body, stream: true, claudeAuth: cr.claudeAuth, capture: cr.capture},
		consultRun{consultant: *c, executor: executor, key: key, state: st, note: note, guidance: added, legRole: legRole})
	return true
}

// withConsultTool adds the consult tool of a role to the executor's request.
// The route's advisor takes the place of Claude Code's own advisor tool.
func withConsultTool(body []byte, role string) ([]byte, bool) {
	if role == guided.RoleAdvisor {
		out, _, err := removeAdvisorTools(body, "")
		if err != nil {
			return nil, false
		}
		body = out
	}
	out, ok, err := guided.AddConsultTool(body, role)
	return out, err == nil && ok
}

// consultAdvisor asks the route's advisor the executor's question. An advisor
// on the subscription answers through Claude Code.
func (s *Server) consultAdvisor(ctx context.Context, c consultant, body []byte, session, question string, capture bool) (string, ledger.Leg) {
	ctx, cancel := context.WithTimeout(ctx, directorTimeout)
	defer cancel()
	if c.target.config.Type == provider.AnthropicSubscription {
		dec := guided.Decision{Reason: guided.ReasonQuestion, Detail: question}
		answer, _, leg := s.consultViaClaudeCode(ctx, c.target, body, guided.RoleAdvisor+"\x00"+session, c.effort, guided.AdvisorSystemPrompt(), dec, "", capture)
		return answer, leg
	}
	req, err := guided.AdvisorRequest(body, c.target.model.ModelID, c.effort, question)
	if err != nil {
		leg := c.target.newLeg()
		leg.Status, leg.Error = ledger.StatusError, err.Error()
		return "", leg
	}
	answer, _, leg := s.askModel(ctx, c.target, req, capture)
	return answer, leg
}

// setAdvisorModel changes the model of every advisor tool in the request.
func setAdvisorModel(body []byte, model string) ([]byte, error) {
	spans, err := jsonbytes.TopLevel(body)
	if err != nil {
		return nil, err
	}
	sp, ok := spans["tools"]
	if !ok {
		return body, nil
	}
	var tools []json.RawMessage
	if err := json.Unmarshal(body[sp.Start:sp.End], &tools); err != nil {
		return nil, err
	}
	name, err := json.Marshal(model)
	if err != nil {
		return nil, err
	}
	items := make([][]byte, len(tools))
	changed := false
	for i, tool := range tools {
		items[i] = tool
		var head struct {
			Type  string `json:"type"`
			Model string `json:"model"`
		}
		if json.Unmarshal(tool, &head) != nil || !strings.HasPrefix(head.Type, "advisor_") || head.Model == model {
			continue
		}
		if items[i], err = jsonbytes.SetField(tool, "model", name); err != nil {
			return nil, err
		}
		changed = true
	}
	if !changed {
		return body, nil
	}
	return jsonbytes.SetField(body, "tools", append(append([]byte{'['}, bytes.Join(items, []byte{','})...), ']'))
}
