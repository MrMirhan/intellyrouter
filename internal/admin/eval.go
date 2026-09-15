package admin

import (
	"errors"
	"net/http"

	"intellyrouter/internal/eval"
	"intellyrouter/internal/store"
)

type evalRunJSON struct {
	ID         int64          `json:"id"`
	CreatedAt  int64          `json:"created_at"`
	FinishedAt int64          `json:"finished_at"`
	Status     string         `json:"status"`
	Mode       string         `json:"mode"`
	Routes     []string       `json:"routes"`
	Tasks      []string       `json:"tasks"`
	Parallel   int            `json:"parallel"`
	Total      int            `json:"total"`
	Done       int            `json:"done"`
	Error      string         `json:"error"`
	Summaries  []eval.Summary `json:"summaries,omitempty"`
	Results    []eval.Result  `json:"results,omitempty"`
}

func toEvalRunJSON(r store.EvalRun) evalRunJSON {
	out := evalRunJSON{
		ID: r.ID, CreatedAt: r.CreatedAt, FinishedAt: r.FinishedAt, Status: r.Status, Mode: r.Mode,
		Routes: r.Routes, Tasks: r.Tasks, Parallel: r.Parallel, Total: r.Total, Done: r.Done, Error: r.Error,
	}
	for _, e := range r.Results {
		out.Results = append(out.Results, eval.Result{
			Task: e.Task, Route: e.Route, Passed: e.Passed, DurationMS: e.DurationMS,
			Requests: e.Requests, EscalatedRequests: e.EscalatedRequests,
			CostUSD: e.CostUSD, SubscriptionValueUSD: e.SubscriptionValueUSD,
			APITokens: e.APITokens, SubscriptionTokens: e.SubscriptionTokens,
			ClaudeOutput: e.ClaudeOutput, TestOutput: e.TestOutput, Error: e.Error,
		})
	}
	if len(out.Results) > 0 {
		out.Summaries = eval.Summarize(out.Results)
	}
	return out
}

func (a *API) getEvalTasks(w http.ResponseWriter, _ *http.Request) {
	type taskJSON struct {
		ID         string `json:"id"`
		Language   string `json:"language"`
		Difficulty string `json:"difficulty"`
		Prompt     string `json:"prompt"`
	}
	resp := struct {
		TasksDir   string     `json:"tasks_dir"`
		ClaudePath string     `json:"claude_path"`
		Error      string     `json:"error"`
		Tasks      []taskJSON `json:"tasks"`
	}{TasksDir: a.eval.TasksDir(), Tasks: []taskJSON{}}
	resp.ClaudePath, _ = a.eval.ClaudePath()
	tasks, err := a.eval.Tasks()
	switch {
	case err != nil:
		resp.Error = err.Error()
	case len(tasks) == 0:
		resp.Error = "no tasks found in " + a.eval.TasksDir()
	}
	for _, t := range tasks {
		resp.Tasks = append(resp.Tasks, taskJSON{ID: t.ID, Language: t.Language, Difficulty: t.Difficulty, Prompt: t.Prompt})
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) listEvalRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := a.store.ListEvalRuns(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	out := make([]evalRunJSON, 0, len(runs))
	for _, run := range runs {
		out = append(out, toEvalRunJSON(run))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) createEvalRun(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Routes   []string `json:"routes"`
		TaskIDs  []string `json:"task_ids"`
		Mode     string   `json:"mode"`
		Parallel int      `json:"parallel"`
		Confirm  bool     `json:"confirm"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !in.Confirm {
		writeError(w, http.StatusBadRequest, "confirm is required: a run lets models run code on this machine and spends API credit or plan limits")
		return
	}
	run, err := a.eval.Start(r.Context(), eval.StartRequest{Routes: in.Routes, TaskIDs: in.TaskIDs, Mode: eval.Mode(in.Mode), Parallel: in.Parallel})
	switch {
	case errors.Is(err, eval.ErrBusy):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, eval.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		a.fail(w, err)
	default:
		writeJSON(w, http.StatusCreated, toEvalRunJSON(run))
	}
}

func (a *API) getEvalRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	run, err := a.store.GetEvalRun(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toEvalRunJSON(run))
}

func (a *API) cancelEvalRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := a.eval.Cancel(id); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteEvalRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	run, err := a.store.GetEvalRun(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return
	}
	if run.Status == store.EvalRunning {
		writeError(w, http.StatusConflict, "cancel the run before you delete it")
		return
	}
	if err := a.store.DeleteEvalRun(r.Context(), id); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
