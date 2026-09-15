package batch

import (
	"context"
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// runWithDeadline fails the test if Run blocks, instead of hanging the suite.
func runWithDeadline(t *testing.T, run func() ([]int, error)) ([]int, error) {
	t.Helper()
	type outcome struct {
		res []int
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := run()
		done <- outcome{res, err}
	}()
	select {
	case o := <-done:
		return o.res, o.err
	case <-time.After(2 * time.Second):
		t.Fatalf("Run did not return within 2s (deadlock or blocked worker)")
		return nil, nil
	}
}

func seq(n int) []int {
	items := make([]int, n)
	for i := range items {
		items[i] = i
	}
	return items
}

func TestResultsKeepInputOrder(t *testing.T) {
	res, err := runWithDeadline(t, func() ([]int, error) {
		return Run(context.Background(), seq(50), 8, func(ctx context.Context, i int) (int, error) {
			for range (50 - i) % 7 {
				runtime.Gosched()
			}
			return i * i, nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range res {
		if v != i*i {
			t.Fatalf("res[%d] = %d, want %d", i, v, i*i)
		}
	}
	if len(res) != 50 {
		t.Fatalf("len(res) = %d, want 50", len(res))
	}
}

func TestConcurrencyLimit(t *testing.T) {
	var active, peak atomic.Int32
	_, err := runWithDeadline(t, func() ([]int, error) {
		return Run(context.Background(), seq(30), 3, func(ctx context.Context, i int) (int, error) {
			n := active.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			active.Add(-1)
			return i, nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if p := peak.Load(); p > 3 {
		t.Fatalf("%d calls ran at the same time, limit is 3", p)
	}
}

func TestZeroWorkersMeansOne(t *testing.T) {
	res, err := runWithDeadline(t, func() ([]int, error) {
		return Run(context.Background(), seq(3), 0, func(ctx context.Context, i int) (int, error) { return i + 1, nil })
	})
	if err != nil || len(res) != 3 || res[2] != 3 {
		t.Fatalf("res = %v, err = %v", res, err)
	}
}

func TestFirstErrorCancelsOtherCalls(t *testing.T) {
	errBoom := errors.New("boom")
	var started atomic.Int32
	fourStarted := make(chan struct{})

	res, err := runWithDeadline(t, func() ([]int, error) {
		return Run(context.Background(), seq(20), 4, func(ctx context.Context, i int) (int, error) {
			if started.Add(1) == 4 {
				close(fourStarted)
			}
			if i == 2 {
				<-fourStarted
				return 0, errBoom
			}
			<-ctx.Done()
			return 0, ctx.Err()
		})
	})
	if res != nil {
		t.Errorf("res = %v, want nil", res)
	}
	var itemErr *ItemError
	if !errors.As(err, &itemErr) || itemErr.Index != 2 {
		t.Fatalf("err = %v, want *ItemError for item 2", err)
	}
	if !errors.Is(err, errBoom) {
		t.Errorf("errors.Is(err, errBoom) = false for %v", err)
	}
	if err.Error() != "item 2: boom" {
		t.Errorf("err.Error() = %q", err.Error())
	}
	if n := started.Load(); n != 4 {
		t.Errorf("%d calls started, want 4: no new calls may start after the first error", n)
	}
}

func TestRunWaitsForStartedCalls(t *testing.T) {
	var started atomic.Int32
	var slowFinished atomic.Bool
	bothStarted := make(chan struct{})

	_, err := runWithDeadline(t, func() ([]int, error) {
		return Run(context.Background(), seq(2), 2, func(ctx context.Context, i int) (int, error) {
			if started.Add(1) == 2 {
				close(bothStarted)
			}
			<-bothStarted
			if i == 0 {
				return 0, errors.New("bad input")
			}
			// This call does not watch ctx, like a syscall that cannot be interrupted.
			time.Sleep(50 * time.Millisecond)
			slowFinished.Store(true)
			return 1, nil
		})
	})
	if err == nil {
		t.Fatal("want an error")
	}
	if !slowFinished.Load() {
		t.Fatal("Run returned while a started call was still running")
	}
}

func TestParentCancelledBeforeRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32
	res, err := runWithDeadline(t, func() ([]int, error) {
		return Run(ctx, seq(5), 2, func(ctx context.Context, i int) (int, error) {
			calls.Add(1)
			return i, nil
		})
	})
	if !errors.Is(err, context.Canceled) || res != nil {
		t.Fatalf("res = %v, err = %v, want nil, context.Canceled", res, err)
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("%d calls started with a cancelled context", n)
	}
}

func TestParentCancelledDuringRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var started atomic.Int32
	twoStarted := make(chan struct{})
	go func() {
		<-twoStarted
		cancel()
	}()

	res, err := runWithDeadline(t, func() ([]int, error) {
		return Run(ctx, seq(10), 2, func(ctx context.Context, i int) (int, error) {
			if started.Add(1) == 2 {
				close(twoStarted)
			}
			<-ctx.Done()
			return 0, ctx.Err()
		})
	})
	if !errors.Is(err, context.Canceled) || res != nil {
		t.Fatalf("res = %v, err = %v, want nil, context.Canceled", res, err)
	}
	if n := started.Load(); n != 2 {
		t.Fatalf("%d calls started, want 2", n)
	}
}
