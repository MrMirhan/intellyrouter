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

type comboJSON struct {
	ID       int64   `json:"id"`
	Name     string  `json:"name"`
	Strategy string  `json:"strategy"`
	Enabled  bool    `json:"enabled"`
	Members  []int64 `json:"members"`
}

func toComboJSON(c store.Combo) comboJSON {
	members := c.Members
	if members == nil {
		members = []int64{}
	}
	return comboJSON{ID: c.ID, Name: c.Name, Strategy: c.Strategy, Enabled: c.Enabled, Members: members}
}

type comboInput struct {
	Name     *string  `json:"name"`
	Strategy *string  `json:"strategy"`
	Enabled  *bool    `json:"enabled"`
	Members  *[]int64 `json:"members"`
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
		c.Members = *in.Members
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
	for i, id := range c.Members {
		if slices.Contains(c.Members[:i], id) {
			return errors.New("a model can be in a combo only once")
		}
		m, err := a.store.GetModel(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("model %d does not exist", id)
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
