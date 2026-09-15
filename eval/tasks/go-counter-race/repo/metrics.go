// Package metrics is a small in-process metrics registry that renders the
// Prometheus text format.
package metrics

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"sync"
)

// Registry holds counters and gauges by name.
type Registry struct {
	mu       sync.RWMutex
	counters map[string]*int64
	gauges   map[string]*int64
}

func NewRegistry() *Registry {
	return &Registry{
		counters: make(map[string]*int64),
		gauges:   make(map[string]*int64),
	}
}

// Inc adds one to the counter name.
func (r *Registry) Inc(name string) {
	r.Add(name, 1)
}

// Add adds delta to the counter name, creating it at zero if needed.
func (r *Registry) Add(name string, delta int64) {
	*r.cell(r.counters, name) += delta
}

// AddGauge adds delta (which may be negative) to the gauge name.
func (r *Registry) AddGauge(name string, delta int64) {
	*r.cell(r.gauges, name) += delta
}

// Counter returns the current value of the counter name.
func (r *Registry) Counter(name string) int64 {
	return r.value(r.counters, name)
}

// Gauge returns the current value of the gauge name.
func (r *Registry) Gauge(name string) int64 {
	return r.value(r.gauges, name)
}

func (r *Registry) cell(m map[string]*int64, name string) *int64 {
	r.mu.RLock()
	c, ok := m[name]
	r.mu.RUnlock()
	if !ok {
		r.mu.Lock()
		c = new(int64)
		m[name] = c
		r.mu.Unlock()
	}
	return c
}

func (r *Registry) value(m map[string]*int64, name string) int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if c, ok := m[name]; ok {
		return *c
	}
	return 0
}

// WriteText writes all metrics in the Prometheus text exposition format.
func (r *Registry) WriteText(w io.Writer) error {
	r.mu.RLock()
	var b strings.Builder
	writeFamily(&b, "counter", r.counters)
	writeFamily(&b, "gauge", r.gauges)
	r.mu.RUnlock()
	_, err := io.WriteString(w, b.String())
	return err
}

func writeFamily(b *strings.Builder, kind string, m map[string]*int64) {
	lastBase := ""
	for _, name := range slices.Sorted(maps.Keys(m)) {
		base, _, _ := strings.Cut(name, "{")
		if base != lastBase {
			fmt.Fprintf(b, "# TYPE %s %s\n", base, kind)
			lastBase = base
		}
		fmt.Fprintf(b, "%s %d\n", name, *m[name])
	}
}
