package store

import (
	"errors"
	"reflect"
	"testing"
)

func TestEvalRuns(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	run, err := s.CreateEvalRun(ctx, EvalRun{Status: EvalRunning, Mode: "key", Routes: []string{"a", "b"}, Tasks: []string{"t1"}, Parallel: 2, Total: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddEvalResult(ctx, run.ID, EvalResult{Task: "t1", Route: "b", Passed: true, CostUSD: 0.5, TestOutput: "ok"}); err != nil {
		t.Fatal(err)
	}

	runs, err := s.ListEvalRuns(ctx)
	if err != nil || len(runs) != 1 || runs[0].Done != 1 || runs[0].Results != nil || !reflect.DeepEqual(runs[0].Routes, []string{"a", "b"}) {
		t.Fatalf("ListEvalRuns = %+v, %v", runs, err)
	}

	other, err := s.CreateEvalRun(ctx, EvalRun{Status: EvalRunning, Mode: "key", Routes: []string{"a"}, Tasks: []string{"t1"}, Parallel: 1, Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishEvalRun(ctx, run.ID, EvalDone, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.FailInterruptedEvalRuns(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetEvalRun(ctx, run.ID)
	if err != nil || got.Status != EvalDone || got.FinishedAt == 0 || len(got.Results) != 1 || !got.Results[0].Passed || got.Results[0].TestOutput != "ok" {
		t.Fatalf("finished run = %+v, %v", got, err)
	}
	interrupted, _ := s.GetEvalRun(ctx, other.ID)
	if interrupted.Status != EvalFailed || interrupted.Error == "" {
		t.Fatalf("interrupted run = %+v", interrupted)
	}

	if err := s.DeleteEvalRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetEvalRun(ctx, run.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted run: err = %v", err)
	}
}
