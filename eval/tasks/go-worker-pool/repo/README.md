# batch

Bounded fan-out for batch jobs: thumbnail generation, webhook redelivery,
bulk imports.

```go
thumbs, err := batch.Run(ctx, images, 8, func(ctx context.Context, img Image) (Thumb, error) {
	return resize(ctx, img)
})
```

`Run` calls the function for every item with at most `workers` calls running at
the same time (a value below 1 means 1). The contract:

- On success, results are in the same order as the items.
- The first failing call stops the batch. The context passed to the calls
  that are still running is cancelled, and no new calls are started. `Run`
  returns a `*batch.ItemError` for that first failure. `errors.Is` and
  `errors.As` see the original error through it. Errors that the other calls
  return because of the cancellation are ignored.
- If the parent context is cancelled, no new calls are started and `Run`
  returns the parent's `ctx.Err()`.
- `Run` returns only after every call it started has returned, so no goroutine
  outlives it.
- When `Run` returns an error, the result slice is nil.
