// Package api exposes the delivery-promise service over HTTP, along with the
// operational endpoints the platform depends on: liveness, readiness, metrics
// and build version.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"runtime"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/Sent-hen/metal-to-pod/app/internal/metrics"
	"github.com/Sent-hen/metal-to-pod/app/internal/promise"
)

// Build identifies the running artifact. The pipeline stamps Commit with the
// git SHA, and the smoke test checks it to prove the new version is serving.
type Build struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

// Faults are deliberate failure modes for reliability drills. They are off by
// default and toggled through environment variables in the Deployment.
type Faults struct {
	ErrorRate  float64       // fraction of /v1 requests that return 500
	Latency    time.Duration // added to every /v1 request
	BadRelease bool          // readiness never passes: a release that must be rolled back
}

type Config struct {
	Network *promise.Network
	Build   Build
	Faults  Faults
	Logger  *slog.Logger
	Now     func() time.Time // injectable clock for tests
}

type Server struct {
	cfg   Config
	ready atomic.Bool

	registry     *metrics.Registry
	httpRequests *metrics.CounterVec
	httpDuration *metrics.HistogramVec
	quotes       *metrics.CounterVec
	transitDays  *metrics.HistogramVec
}

func New(cfg Config) *Server {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	reg := metrics.NewRegistry()
	s := &Server{
		cfg:      cfg,
		registry: reg,
		httpRequests: reg.NewCounterVec("http_requests_total",
			"HTTP requests by route pattern, method and status code.", "route", "method", "code"),
		httpDuration: reg.NewHistogramVec("http_request_duration_seconds",
			"HTTP request latency by route pattern.",
			[]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.3, 0.5, 1, 2.5}, "route"),
		quotes: reg.NewCounterVec("promise_quotes_total",
			"Delivery promises issued, by service level and selected DC.", "service_level", "dc"),
		transitDays: reg.NewHistogramVec("promise_transit_business_days",
			"Promised transit time in business days, by service level.",
			[]float64{1, 2, 3, 4, 5, 7}, "service_level"),
	}
	reg.NewGaugeVec("app_build_info", "Build metadata; value is always 1.", "version", "commit").
		Set(1, cfg.Build.Version, cfg.Build.Commit)
	reg.NewGaugeFunc("app_goroutines", "Number of goroutines.", func() float64 { return float64(runtime.NumGoroutine()) })
	return s
}

// SetReady flips the readiness probe. main sets it true once the network
// config is loaded and false on SIGTERM so Kubernetes drains traffic first.
func (s *Server) SetReady(ready bool) { s.ready.Store(ready) }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /readyz", s.readyz)
	mux.Handle("GET /metrics", s.registry.Handler())
	mux.HandleFunc("GET /version", s.version)
	mux.Handle("GET /v1/network", s.withFaults(http.HandlerFunc(s.network)))
	mux.Handle("POST /v1/promise", s.withFaults(http.HandlerFunc(s.promise)))
	return s.instrument(mux)
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Write([]byte("ok\n"))
}

func (s *Server) readyz(w http.ResponseWriter, _ *http.Request) {
	switch {
	case s.cfg.Faults.BadRelease:
		http.Error(w, "not ready: SIMULATE_BAD_RELEASE is set", http.StatusServiceUnavailable)
	case !s.ready.Load():
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	default:
		w.Write([]byte("ready\n"))
	}
}

func (s *Server) version(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.cfg.Build)
}

func (s *Server) network(w http.ResponseWriter, _ *http.Request) {
	n := s.cfg.Network
	writeJSON(w, http.StatusOK, map[string]any{
		"distribution_centers": n.DCs,
		"regions":              n.Regions(),
		"service_levels":       n.ServiceLevels,
	})
}

func (s *Server) promise(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var req promise.Request
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}

	q, err := s.cfg.Network.Quote(req, s.cfg.Now())
	var ve *promise.ValidationError
	switch {
	case errors.As(err, &ve):
		writeError(w, http.StatusUnprocessableEntity, ve.Error())
		return
	case err != nil:
		s.cfg.Logger.Error("quote failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	s.quotes.Inc(q.ServiceLevel, q.Selected.DC)
	s.transitDays.Observe(float64(q.Selected.TransitDays), q.ServiceLevel)
	writeJSON(w, http.StatusOK, q)
}

// withFaults injects latency and errors for reliability drills.
func (s *Server) withFaults(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f := s.cfg.Faults
		if f.Latency > 0 {
			time.Sleep(f.Latency)
		}
		if f.ErrorRate > 0 && rand.Float64() < f.ErrorRate {
			writeError(w, http.StatusInternalServerError, "injected fault (FAULT_ERROR_RATE)")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.code = code
	r.ResponseWriter.WriteHeader(code)
}

// instrument records RED metrics (rate, errors, duration) and an access log.
// The route label is the mux pattern ("POST /v1/promise"), never the raw URL
// path, so a scanner hitting random URLs cannot explode metric cardinality.
func (s *Server) instrument(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, code: http.StatusOK}
		mux.ServeHTTP(rec, r)
		elapsed := time.Since(start)

		route := r.Pattern // set by ServeMux on the matched request (Go 1.23+)
		if route == "" {
			route = "unmatched"
		}
		s.httpRequests.Inc(route, r.Method, strconv.Itoa(rec.code))
		s.httpDuration.Observe(elapsed.Seconds(), route)

		switch route {
		case "GET /healthz", "GET /readyz", "GET /metrics":
			return // probe and scrape traffic would drown the access log
		}
		s.cfg.Logger.Info("request",
			"method", r.Method, "path", r.URL.Path, "route", route,
			"status", rec.code, "duration_ms", float64(elapsed.Microseconds())/1000)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
