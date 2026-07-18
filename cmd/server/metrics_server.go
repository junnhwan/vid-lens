package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultMetricsAddress = "127.0.0.1:19090"

func metricsListenAddress() string {
	address := strings.TrimSpace(os.Getenv("VIDLENS_METRICS_ADDR"))
	if address == "" {
		return defaultMetricsAddress
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		slog.Warn("invalid metrics listen address; using loopback default", "address", address)
		return defaultMetricsAddress
	}
	remote := host == "" || host == "0.0.0.0" || host == "::"
	if remote && !strings.EqualFold(strings.TrimSpace(os.Getenv("VIDLENS_METRICS_ALLOW_REMOTE")), "true") {
		slog.Warn("remote metrics binding rejected; set VIDLENS_METRICS_ALLOW_REMOTE=true only behind a firewall")
		return defaultMetricsAddress
	}
	return address
}

func newMetricsServer(metrics http.Handler) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics)
	return &http.Server{
		Addr:              metricsListenAddress(),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

const metricsShutdownTimeout = 5 * time.Second

// serveMetrics keeps the admin listener tied to the process runtime context.
// It intentionally exposes only /metrics and shuts down before the process exits.
func serveMetrics(ctx context.Context, metrics http.Handler) error {
	server := newMetricsServer(metrics)
	serveErr := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErr <- err
	}()
	slog.Info("metrics admin listener started", "address", server.Addr)

	select {
	case err := <-serveErr:
		if err != nil {
			slog.Error("metrics admin listener stopped", "error", err)
		}
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), metricsShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return err
	}
	return <-serveErr
}
