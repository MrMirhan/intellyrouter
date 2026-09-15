package metrics

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestWriteText(t *testing.T) {
	reg := NewRegistry()
	reg.Inc("jobs_total")
	reg.Inc("jobs_total")
	reg.Inc(`http_requests_total{code="5xx"}`)
	reg.Inc(`http_requests_total{code="2xx"}`)
	reg.AddGauge("queue_depth", 5)
	reg.AddGauge("queue_depth", -2)

	var b strings.Builder
	if err := reg.WriteText(&b); err != nil {
		t.Fatal(err)
	}
	want := `# TYPE http_requests_total counter
http_requests_total{code="2xx"} 1
http_requests_total{code="5xx"} 1
# TYPE jobs_total counter
jobs_total 2
# TYPE queue_depth gauge
queue_depth 3
`
	if b.String() != want {
		t.Errorf("WriteText:\n%s\nwant:\n%s", b.String(), want)
	}
}

func TestCountersAreExactUnderConcurrency(t *testing.T) {
	const workers, perWorker = 16, 2000
	reg := NewRegistry()
	start := make(chan struct{})
	done := make(chan struct{})

	var readers sync.WaitGroup
	readers.Go(func() {
		for {
			select {
			case <-done:
				return
			default:
				reg.WriteText(io.Discard)
				reg.Counter("total")
			}
		}
	})

	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			<-start
			for i := range perWorker {
				reg.Inc("total")
				reg.Inc(fmt.Sprintf("shard_%d", (w+i)%4))
				reg.AddGauge("balance", 1)
				reg.AddGauge("balance", -1)
			}
		})
	}
	close(start)
	wg.Wait()
	close(done)
	readers.Wait()

	if got := reg.Counter("total"); got != workers*perWorker {
		t.Errorf("total = %d, want %d", got, workers*perWorker)
	}
	for s := range 4 {
		name := fmt.Sprintf("shard_%d", s)
		if got := reg.Counter(name); got != workers*perWorker/4 {
			t.Errorf("%s = %d, want %d", name, got, workers*perWorker/4)
		}
	}
	if got := reg.Gauge("balance"); got != 0 {
		t.Errorf("balance = %d, want 0", got)
	}
}

func TestInstrumentUnderLoad(t *testing.T) {
	reg := NewRegistry()
	app := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/missing":
			http.NotFound(w, r)
		case "/boom":
			w.WriteHeader(http.StatusInternalServerError)
			w.WriteHeader(http.StatusOK)
		default:
			io.WriteString(w, "ok")
		}
	})
	h := Instrument(reg, app)
	paths := []string{"/", "/missing", "/boom"}

	var wg sync.WaitGroup
	for g := range 12 {
		wg.Go(func() {
			for i := range 150 {
				path := paths[(g+i)%len(paths)]
				h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
			}
		})
	}
	wg.Wait()

	for code, want := range map[string]int64{"2xx": 600, "4xx": 600, "5xx": 600} {
		name := `http_requests_total{code="` + code + `"}`
		if got := reg.Counter(name); got != want {
			t.Errorf("%s = %d, want %d", name, got, want)
		}
	}
	if got := reg.Gauge(inFlight); got != 0 {
		t.Errorf("%s = %d after all requests finished, want 0", inFlight, got)
	}
}
