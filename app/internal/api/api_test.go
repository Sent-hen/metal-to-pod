package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Sent-hen/metal-to-pod/app/internal/promise"
)

func newTestServer(t *testing.T, faults Faults) (*Server, *httptest.Server) {
	t.Helper()
	n, err := promise.Load("../promise/testdata/network.json")
	if err != nil {
		t.Fatal(err)
	}
	s := New(Config{
		Network: n,
		Build:   Build{Version: "test", Commit: "abc123"},
		Faults:  faults,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:     func() time.Time { return time.Date(2026, 9, 29, 15, 0, 0, 0, time.UTC) },
	})
	s.SetReady(true)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, ts
}

func do(t *testing.T, method, url, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestPromiseEndpoint(t *testing.T) {
	_, ts := newTestServer(t, Faults{})
	code, body := do(t, "POST", ts.URL+"/v1/promise", `{"region":"south","service_level":"ground","weight_lb":4}`)
	if code != 200 {
		t.Fatalf("status %d: %s", code, body)
	}
	var q promise.Quote
	if err := json.Unmarshal([]byte(body), &q); err != nil {
		t.Fatal(err)
	}
	if q.Selected.DC != "DAL" || q.Selected.DeliveryDate != "2026-09-30" {
		t.Errorf("selected = %+v", q.Selected)
	}
}

func TestPromiseErrors(t *testing.T) {
	_, ts := newTestServer(t, Faults{})
	cases := []struct {
		body string
		want int
	}{
		{`not json`, 400},
		{`{"region":"south","service_level":"ground","weight_lb":4,"colour":"red"}`, 400},
		{`{"region":"mars","service_level":"ground","weight_lb":4}`, 422},
		{`{"region":"south","service_level":"ground","weight_lb":900}`, 422},
	}
	for _, c := range cases {
		if code, body := do(t, "POST", ts.URL+"/v1/promise", c.body); code != c.want {
			t.Errorf("%s -> %d (%s), want %d", c.body, code, body, c.want)
		}
	}
	if code, _ := do(t, "GET", ts.URL+"/v1/promise", ""); code != 405 {
		t.Errorf("GET /v1/promise -> %d, want 405", code)
	}
}

func TestReadinessLifecycle(t *testing.T) {
	s, ts := newTestServer(t, Faults{})
	if code, _ := do(t, "GET", ts.URL+"/readyz", ""); code != 200 {
		t.Fatalf("ready server -> %d", code)
	}
	s.SetReady(false) // what main does on SIGTERM
	if code, _ := do(t, "GET", ts.URL+"/readyz", ""); code != 503 {
		t.Fatalf("draining server -> %d, want 503", code)
	}
	if code, _ := do(t, "GET", ts.URL+"/healthz", ""); code != 200 {
		t.Fatalf("liveness must stay green while draining, got %d", code)
	}
}

func TestBadReleaseNeverReady(t *testing.T) {
	_, ts := newTestServer(t, Faults{BadRelease: true})
	if code, _ := do(t, "GET", ts.URL+"/readyz", ""); code != 503 {
		t.Fatalf("bad release -> %d, want 503", code)
	}
}

func TestInjectedErrors(t *testing.T) {
	_, ts := newTestServer(t, Faults{ErrorRate: 1})
	if code, _ := do(t, "GET", ts.URL+"/v1/network", ""); code != 500 {
		t.Fatalf("got %d, want injected 500", code)
	}
	if code, _ := do(t, "GET", ts.URL+"/healthz", ""); code != 200 {
		t.Fatalf("faults must not touch probes, got %d", code)
	}
}

func TestVersionAndMetrics(t *testing.T) {
	_, ts := newTestServer(t, Faults{})
	if _, body := do(t, "GET", ts.URL+"/version", ""); !strings.Contains(body, `"commit":"abc123"`) {
		t.Errorf("version body = %s", body)
	}
	do(t, "POST", ts.URL+"/v1/promise", `{"region":"west","service_level":"two_day","weight_lb":1}`)
	do(t, "GET", ts.URL+"/no/such/path/12345", "")

	_, body := do(t, "GET", ts.URL+"/metrics", "")
	for _, want := range []string{
		`http_requests_total{route="POST /v1/promise",method="POST",code="200"} 1`,
		`http_requests_total{route="unmatched",method="GET",code="404"} 1`,
		`promise_quotes_total{service_level="two_day",dc="RNO"} 1`,
		`app_build_info{version="test",commit="abc123"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics missing %q", want)
		}
	}
	if strings.Contains(body, "12345") {
		t.Error("raw URL path leaked into metric labels")
	}
}
