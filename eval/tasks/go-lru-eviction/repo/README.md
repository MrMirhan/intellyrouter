# lru

A small generic least-recently-used cache for Go.

```go
c := lru.New[string, []byte](128, func(key string, _ []byte) {
	log.Printf("evicted %s", key)
})
c.Put("a", data)
if v, ok := c.Get("a"); ok {
	// ...
}
```

`Get` and `Put` count as a use of the key. `Peek` reads a value without
changing its position. When the cache is full, `Put` evicts the entry that was
used least recently and calls the eviction callback.

The cache is not safe for concurrent use. Wrap it with a mutex if you share it
between goroutines.

Run the tests with `go test ./...`.
