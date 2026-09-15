// Package batch runs a function over many items with bounded concurrency.
package batch

import "context"

type result[R any] struct {
	index int
	value R
	err   error
}

// Run calls fn for each item using at most workers concurrent calls and
// returns the results in input order. See the README for the error contract.
func Run[T, R any](ctx context.Context, items []T, workers int, fn func(context.Context, T) (R, error)) ([]R, error) {
	if workers < 1 {
		workers = 1
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan int)
	results := make(chan result[R])

	go func() {
		for i := range items {
			jobs <- i
		}
		close(jobs)
	}()

	for range workers {
		go func() {
			for i := range jobs {
				v, err := fn(ctx, items[i])
				results <- result[R]{index: i, value: v, err: err}
			}
		}()
	}

	out := make([]R, len(items))
	for range items {
		r := <-results
		if r.err != nil {
			return nil, &ItemError{Index: r.index, Err: r.err}
		}
		out[r.index] = r.value
	}
	return out, nil
}
