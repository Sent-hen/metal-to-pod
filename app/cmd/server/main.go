// Command server runs the delivery-promise service.
//
// Configuration is environment-driven (12-factor) so the same image runs in
// every environment and the Deployment manifest is the single place to see how
// it is configured:
//
//	PORT                  listen port (default 8080)
//	NETWORK_CONFIG        fulfillment network JSON (default /etc/delivery-promise/network.json)
//	DRAIN_SECONDS         how long to fail readiness before shutting down (default 5)
//	FAULT_ERROR_RATE      0..1 fraction of /v1 requests to fail   (drills only)
//	FAULT_LATENCY_MS      latency added to /v1 requests           (drills only)
//	SIMULATE_BAD_RELEASE  "true" = readiness never passes         (drills only)
//
// With -check-config PATH (or "-" for stdin) it validates a network file and
// exits; the pipeline uses this to gate config changes before deploying them.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Sent-hen/metal-to-pod/app/internal/api"
	"github.com/Sent-hen/metal-to-pod/app/internal/promise"
)

// Stamped at build time: -ldflags "-X main.version=... -X main.commit=...".
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	checkConfig := flag.String("check-config", "", "validate a network config file (\"-\" for stdin) and exit")
	flag.Parse()

	if *checkConfig != "" {
		if err := validate(*checkConfig); err != nil {
			fmt.Fprintln(os.Stderr, "config invalid:", err)
			os.Exit(1)
		}
		fmt.Println("config ok")
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("commit", commit)
	if err := run(logger); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func validate(path string) error {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return err
	}
	_, err = promise.Parse(data)
	return err
}

func run(logger *slog.Logger) error {
	cfgPath := env("NETWORK_CONFIG", "/etc/delivery-promise/network.json")
	network, err := promise.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load network config %s: %w", cfgPath, err)
	}

	faults := api.Faults{
		ErrorRate:  envFloat(logger, "FAULT_ERROR_RATE", 0),
		Latency:    time.Duration(envFloat(logger, "FAULT_LATENCY_MS", 0)) * time.Millisecond,
		BadRelease: env("SIMULATE_BAD_RELEASE", "false") == "true",
	}
	if faults != (api.Faults{}) {
		logger.Warn("fault injection enabled", "error_rate", faults.ErrorRate,
			"latency", faults.Latency.String(), "bad_release", faults.BadRelease)
	}

	srv := api.New(api.Config{
		Network: network,
		Build:   api.Build{Version: version, Commit: commit},
		Faults:  faults,
		Logger:  logger,
	})

	httpServer := &http.Server{
		Addr:              ":" + env("PORT", "8080"),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- httpServer.ListenAndServe() }()
	srv.SetReady(true)
	logger.Info("listening", "addr", httpServer.Addr, "version", version,
		"dcs", len(network.DCs), "lanes", len(network.Lanes))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	// Graceful shutdown. Kubernetes sends SIGTERM and removes the pod from
	// Service endpoints at roughly the same time, but endpoint removal takes a
	// moment to reach every node's kube-proxy rules. Failing readiness and
	// continuing to serve for DRAIN_SECONDS covers that gap, so in-flight and
	// late-arriving requests are not dropped during a rolling update.
	drain := time.Duration(envFloat(logger, "DRAIN_SECONDS", 5) * float64(time.Second))
	logger.Info("SIGTERM received, draining", "drain", drain.String())
	srv.SetReady(false)
	time.Sleep(drain)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	logger.Info("shutdown complete")
	return nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envFloat(logger *slog.Logger, key string, def float64) float64 {
	raw := env(key, "")
	if raw == "" {
		return def
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		logger.Warn("ignoring unparseable env var", "key", key, "value", raw)
		return def
	}
	return v
}
