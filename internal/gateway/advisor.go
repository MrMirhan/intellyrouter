package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"intellyrouter/internal/jsonbytes"
	"intellyrouter/internal/store"
)

// applyRouteAdvisor puts the route's advisor choice into the request. Claude
// Code adds the advisor tool; the route can switch its model or remove it. A
// request without an advisor tool stays as it is.
func (s *Server) applyRouteAdvisor(ctx context.Context, route store.Route, body []byte) []byte {
	adv, err := store.ParseRouteAdvisor(route.Settings)
	if err != nil {
		s.log.Warn("read route advisor", "route", route.Name, "err", err)
		return body
	}
	var out []byte
	switch {
	case adv.Off:
		out, _, err = removeAdvisorTools(body, "")
	case adv.ModelID != 0:
		var m store.Model
		if m, err = s.store.GetModel(ctx, adv.ModelID); err == nil {
			out, err = setAdvisorModel(body, m.ModelID)
		}
	default:
		return body
	}
	if err != nil {
		s.log.Warn("apply route advisor", "route", route.Name, "err", err)
		return body
	}
	return out
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
