package metrics

import (
	"fmt"
	"net/http"
)

const inFlight = "http_requests_in_flight"

// Instrument wraps next and records request counts by status class and the
// number of in-flight requests.
func Instrument(reg *Registry, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reg.AddGauge(inFlight, 1)
		defer reg.AddGauge(inFlight, -1)

		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		reg.Inc(fmt.Sprintf(`http_requests_total{code="%dxx"}`, sw.status/100))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.status = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(p []byte) (int, error) {
	w.wroteHeader = true
	return w.ResponseWriter.Write(p)
}
