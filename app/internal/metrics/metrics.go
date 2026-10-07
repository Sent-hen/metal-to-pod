// Package metrics is a deliberately small implementation of the Prometheus
// text exposition format (counters, gauges, histograms with labels).
//
// A real service would import prometheus/client_golang. This one keeps the app
// dependency-free and, in the spirit of the project, makes the wire format
// Prometheus scrapes visible instead of hidden behind a library.
package metrics

import (
	"bufio"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Registry holds metrics in registration order.
type Registry struct {
	mu      sync.Mutex
	metrics []writer
}

type writer interface {
	write(w *bufio.Writer)
}

func NewRegistry() *Registry { return &Registry{} }

func (r *Registry) register(m writer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metrics = append(r.metrics, m)
}

// Handler serves the registry in Prometheus text format.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		bw := bufio.NewWriter(w)
		r.mu.Lock()
		ms := append([]writer(nil), r.metrics...)
		r.mu.Unlock()
		for _, m := range ms {
			m.write(bw)
		}
		bw.Flush()
	})
}

// vec is the shared label bookkeeping for counter/gauge/histogram vectors.
type vec struct {
	name, help, kind string
	labels           []string
}

func (v *vec) key(values []string) string {
	if len(values) != len(v.labels) {
		panic(fmt.Sprintf("metric %s: got %d label values, want %d", v.name, len(values), len(v.labels)))
	}
	return strings.Join(values, "\xff")
}

func (v *vec) header(w *bufio.Writer) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", v.name, v.help, v.name, v.kind)
}

// labelString renders {a="x",b="y"} plus optional extra pairs (used for le).
func (v *vec) labelString(key string, extra ...string) string {
	var values []string
	if len(v.labels) > 0 {
		values = strings.Split(key, "\xff")
	}
	var parts []string
	for i, l := range v.labels {
		parts = append(parts, l+`="`+escape(values[i])+`"`)
	}
	for i := 0; i+1 < len(extra); i += 2 {
		parts = append(parts, extra[i]+`="`+escape(extra[i+1])+`"`)
	}
	if len(parts) == 0 {
		return ""
	}
	return "{" + strings.Join(parts, ",") + "}"
}

var escaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

func escape(s string) string { return escaper.Replace(s) }

func formatFloat(f float64) string {
	switch {
	case math.IsInf(f, 1):
		return "+Inf"
	case math.IsInf(f, -1):
		return "-Inf"
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// CounterVec is a monotonically increasing value per label set.
type CounterVec struct {
	vec
	mu     sync.Mutex
	values map[string]float64
}

func (r *Registry) NewCounterVec(name, help string, labels ...string) *CounterVec {
	c := &CounterVec{vec: vec{name: name, help: help, kind: "counter", labels: labels}, values: map[string]float64{}}
	r.register(c)
	return c
}

func (c *CounterVec) Inc(labelValues ...string) { c.Add(1, labelValues...) }

func (c *CounterVec) Add(delta float64, labelValues ...string) {
	if delta < 0 {
		panic("counter " + c.name + ": negative increment")
	}
	k := c.key(labelValues)
	c.mu.Lock()
	c.values[k] += delta
	c.mu.Unlock()
}

func (c *CounterVec) write(w *bufio.Writer) {
	c.header(w)
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, k := range sortedKeys(c.values) {
		fmt.Fprintf(w, "%s%s %s\n", c.name, c.labelString(k), formatFloat(c.values[k]))
	}
}

// GaugeVec is a value that can go up and down per label set.
type GaugeVec struct {
	vec
	mu     sync.Mutex
	values map[string]float64
}

func (r *Registry) NewGaugeVec(name, help string, labels ...string) *GaugeVec {
	g := &GaugeVec{vec: vec{name: name, help: help, kind: "gauge", labels: labels}, values: map[string]float64{}}
	r.register(g)
	return g
}

func (g *GaugeVec) Set(v float64, labelValues ...string) {
	k := g.key(labelValues)
	g.mu.Lock()
	g.values[k] = v
	g.mu.Unlock()
}

func (g *GaugeVec) write(w *bufio.Writer) {
	g.header(w)
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, k := range sortedKeys(g.values) {
		fmt.Fprintf(w, "%s%s %s\n", g.name, g.labelString(k), formatFloat(g.values[k]))
	}
}

// GaugeFunc samples a value at scrape time (e.g. goroutine count).
type GaugeFunc struct {
	vec
	fn func() float64
}

func (r *Registry) NewGaugeFunc(name, help string, fn func() float64) {
	r.register(&GaugeFunc{vec: vec{name: name, help: help, kind: "gauge"}, fn: fn})
}

func (g *GaugeFunc) write(w *bufio.Writer) {
	g.header(w)
	fmt.Fprintf(w, "%s %s\n", g.name, formatFloat(g.fn()))
}

// HistogramVec counts observations into cumulative buckets per label set.
type HistogramVec struct {
	vec
	buckets []float64 // upper bounds, ascending, without +Inf
	mu      sync.Mutex
	series  map[string]*histogram
}

type histogram struct {
	counts []uint64 // non-cumulative, one per bucket, plus overflow for +Inf
	sum    float64
	count  uint64
}

func (r *Registry) NewHistogramVec(name, help string, buckets []float64, labels ...string) *HistogramVec {
	if !sort.Float64sAreSorted(buckets) {
		panic("histogram " + name + ": buckets must be sorted")
	}
	h := &HistogramVec{
		vec:     vec{name: name, help: help, kind: "histogram", labels: labels},
		buckets: buckets,
		series:  map[string]*histogram{},
	}
	r.register(h)
	return h
}

func (h *HistogramVec) Observe(v float64, labelValues ...string) {
	k := h.key(labelValues)
	i := sort.SearchFloat64s(h.buckets, v) // first bucket with upper bound >= v
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.series[k]
	if !ok {
		s = &histogram{counts: make([]uint64, len(h.buckets)+1)}
		h.series[k] = s
	}
	s.counts[i]++
	s.sum += v
	s.count++
}

func (h *HistogramVec) write(w *bufio.Writer) {
	h.header(w)
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, k := range sortedKeys(h.series) {
		s := h.series[k]
		var cumulative uint64
		for i, ub := range h.buckets {
			cumulative += s.counts[i]
			fmt.Fprintf(w, "%s_bucket%s %d\n", h.name, h.labelString(k, "le", formatFloat(ub)), cumulative)
		}
		fmt.Fprintf(w, "%s_bucket%s %d\n", h.name, h.labelString(k, "le", "+Inf"), s.count)
		fmt.Fprintf(w, "%s_sum%s %s\n", h.name, h.labelString(k), formatFloat(s.sum))
		fmt.Fprintf(w, "%s_count%s %d\n", h.name, h.labelString(k), s.count)
	}
}
