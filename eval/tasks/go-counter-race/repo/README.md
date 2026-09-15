# metrics

In-process counters and gauges for services that do not want a full
Prometheus client dependency. The registry renders the Prometheus text format,
so the `/metrics` endpoint can be scraped as usual.

```go
reg := metrics.NewRegistry()
http.Handle("/", metrics.Instrument(reg, appHandler))
http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
	reg.WriteText(w)
})
```

`Instrument` records:

- `http_requests_total{code="2xx"}` (and `3xx`, `4xx`, `5xx`): completed requests
  by status class
- `http_requests_in_flight`: requests currently being served

All `Registry` methods are safe to call from many goroutines at the same time.
Metric names can include a label set in braces; `WriteText` sorts metrics by
name and prints one `# TYPE` line per metric family.
