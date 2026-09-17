package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/MrMirhan/intellyrouter/internal/store"
)

var comboNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

var comboStrategies = []string{store.ComboFallback, store.ComboRoundRobin, store.ComboLeastUsed}

// maxComboWeight bounds a member's share. Weights are relative, so a wide
// spread only makes the small members unreachable in practice.
const maxComboWeight = 1000

type comboMemberJSON struct {
	ModelID int64 `json:"model_id"`
	Weight  int   `json:"weight"`
}

// comboMemberInput is a member in a request. Weight is a pointer so that a
// member sent without one takes the default, while an explicit 0 is refused
// rather than quietly turned into a share the caller did not ask for.
type comboMemberInput struct {
	ModelID int64 `json:"model_id"`
	Weight  *int  `json:"weight"`
}

type comboJSON struct {
	ID       int64             `json:"id"`
	Name     string            `json:"name"`
	Strategy string            `json:"strategy"`
	Enabled  bool              `json:"enabled"`
	Members  []comboMemberJSON `json:"members"`
}

func toComboJSON(c store.Combo) comboJSON {
	members := make([]comboMemberJSON, 0, len(c.Members))
	for _, m := range c.Members {
		members = append(members, comboMemberJSON{ModelID: m.ModelID, Weight: m.Weight})
	}
	return comboJSON{ID: c.ID, Name: c.Name, Strategy: c.Strategy, Enabled: c.Enabled, Members: members}
}

type comboInput struct {
	Name     *string             `json:"name"`
	Strategy *string             `json:"strategy"`
	Enabled  *bool               `json:"enabled"`
	Members  *[]comboMemberInput `json:"members"`
}

func (in comboInput) apply(c *store.Combo) {
	if in.Name != nil {
		c.Name = strings.TrimSpace(*in.Name)
	}
	if in.Strategy != nil {
		c.Strategy = *in.Strategy
	}
	if in.Enabled != nil {
		c.Enabled = *in.Enabled
	}
	if in.Members != nil {
		c.Members = make([]store.ComboMember, 0, len(*in.Members))
		for _, m := range *in.Members {
			// A member sent without a weight gets an equal share.
			weight := 1
			if m.Weight != nil {
				weight = *m.Weight
			}
			c.Members = append(c.Members, store.ComboMember{ModelID: m.ModelID, Weight: weight})
		}
	}
}

func (a *API) validateCombo(ctx context.Context, c store.Combo) error {
	switch {
	case !comboNamePattern.MatchString(c.Name):
		return errors.New("name may only contain letters, digits, \".\", \"_\" and \"-\", and must start with a letter or digit")
	case !slices.Contains(comboStrategies, c.Strategy):
		return fmt.Errorf("strategy must be one of %s", strings.Join(comboStrategies, ", "))
	case len(c.Members) == 0:
		return errors.New("a combo needs at least one model")
	}
	for i, member := range c.Members {
		if slices.ContainsFunc(c.Members[:i], func(o store.ComboMember) bool { return o.ModelID == member.ModelID }) {
			return errors.New("a model can be in a combo only once")
		}
		if member.Weight < 1 || member.Weight > maxComboWeight {
			return fmt.Errorf("weight must be between 1 and %d", maxComboWeight)
		}
		m, err := a.store.GetModel(ctx, member.ModelID)
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("model %d does not exist", member.ModelID)
		}
		if err != nil {
			return err
		}
		p, err := a.store.GetProvider(ctx, m.ProviderID)
		if err != nil {
			return err
		}
		if p.Type == store.ComboProviderType {
			return fmt.Errorf("%s is a combo; a combo cannot contain another combo", m.ModelID)
		}
	}
	return nil
}

func (a *API) listCombos(w http.ResponseWriter, r *http.Request) {
	cs, err := a.store.ListCombos(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	out := make([]comboJSON, 0, len(cs))
	for _, c := range cs {
		out = append(out, toComboJSON(c))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) createCombo(w http.ResponseWriter, r *http.Request) {
	var in comboInput
	if !decode(w, r, &in) {
		return
	}
	c := store.Combo{Strategy: store.ComboFallback, Enabled: true}
	in.apply(&c)
	if err := a.validateCombo(r.Context(), c); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	c, err := a.store.CreateCombo(r.Context(), c)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toComboJSON(c))
}

func (a *API) updateCombo(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	c, err := a.store.GetCombo(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return
	}
	var in comboInput
	if !decode(w, r, &in) {
		return
	}
	wasEnabled := c.Enabled
	in.apply(&c)
	if err := a.validateCombo(r.Context(), c); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if wasEnabled && !c.Enabled && !a.checkUnused(w, r.Context(), c) {
		return
	}
	if err := a.store.UpdateCombo(r.Context(), c); err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toComboJSON(c))
}

func (a *API) deleteCombo(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	c, err := a.store.GetCombo(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return
	}
	if !a.checkUnused(w, r.Context(), c) {
		return
	}
	if err := a.store.DeleteCombo(r.Context(), id); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// checkUnused writes a conflict when a route uses the combo.
func (a *API) checkUnused(w http.ResponseWriter, ctx context.Context, c store.Combo) bool {
	routes, err := a.routesUsingModel(ctx, c.ID)
	if err != nil {
		a.fail(w, err)
		return false
	}
	if len(routes) > 0 {
		writeError(w, http.StatusConflict, "combo is used by routes: "+strings.Join(routes, ", "))
		return false
	}
	return true
}
