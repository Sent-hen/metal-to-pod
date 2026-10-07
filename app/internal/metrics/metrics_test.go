package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func scrape(t *testing.T, r *Registry) string {
	t.Helper()
	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain; version=0.0.4") {
		t.Errorf("content-type = %q", ct)
	}
	return rec.Body.String()
}

func TestExpositionFormat(t *testing.T) {
	r := NewRegistry()
	c := r.NewCounterVec("requests_total", "Requests.", "route", "code")
	c.Inc("/a", "200")
	c.Inc("/a", "200")
	c.Inc("/b", "500")

	g := r.NewGaugeVec("build_info", "Build.", "version")
	g.Set(1, `v"1`)

	h := r.NewHistogramVec("latency_seconds", "Latency.", []float64{0.1, 0.5}, "route")
	h.Observe(0.05, "/a")
	h.Observe(0.1, "/a") // boundary value belongs to le="0.1"
	h.Observe(0.3, "/a")
	h.Observe(2, "/a")

	r.NewGaugeFunc("goroutines", "Goroutines.", func() float64 { return 7 })

	want := `# HELP requests_total Requests.
# TYPE requests_total counter
requests_total{route="/a",code="200"} 2
requests_total{route="/b",code="500"} 1
# HELP build_info Build.
# TYPE build_info gauge
build_info{version="v\"1"} 1
# HELP latency_seconds Latency.
# TYPE latency_seconds histogram
latency_seconds_bucket{route="/a",le="0.1"} 2
latency_seconds_bucket{route="/a",le="0.5"} 3
latency_seconds_bucket{route="/a",le="+Inf"} 4
latency_seconds_sum{route="/a"} 2.45
latency_seconds_count{route="/a"} 4
# HELP goroutines Goroutines.
# TYPE goroutines gauge
goroutines 7
`
	if got := scrape(t, r); got != want {
		t.Errorf("exposition mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestWrongLabelCountPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	NewRegistry().NewCounterVec("x_total", "x", "a").Inc("1", "2")
}
