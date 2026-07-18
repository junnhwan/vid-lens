package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewMetricsServerUsesLoopbackAdminListenerAndOnlyMetricsRoute(t *testing.T) {
	t.Setenv("VIDLENS_METRICS_ADDR", "")
	server := newMetricsServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	if server.Addr != "127.0.0.1:19090" {
		t.Fatalf("addr=%q", server.Addr)
	}
	metrics := httptest.NewRecorder()
	server.Handler.ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if metrics.Code != http.StatusNoContent {
		t.Fatalf("metrics status=%d", metrics.Code)
	}
	business := httptest.NewRecorder()
	server.Handler.ServeHTTP(business, httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil))
	if business.Code != http.StatusNotFound {
		t.Fatalf("business status=%d", business.Code)
	}
}

func TestMetricsAddressRejectsWildcardBindingWithoutExplicitOptIn(t *testing.T) {
	t.Setenv("VIDLENS_METRICS_ADDR", "0.0.0.0:19090")
	t.Setenv("VIDLENS_METRICS_ALLOW_REMOTE", "")
	if got := metricsListenAddress(); got != "127.0.0.1:19090" {
		t.Fatalf("addr=%q", got)
	}
	t.Setenv("VIDLENS_METRICS_ALLOW_REMOTE", "true")
	if got := metricsListenAddress(); got != "0.0.0.0:19090" {
		t.Fatalf("opt-in addr=%q", got)
	}
}

func TestServeMetricsStopsWhenContextIsCanceled(t *testing.T) {
	t.Setenv("VIDLENS_METRICS_ADDR", "127.0.0.1:0")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- serveMetrics(ctx, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveMetrics returned error during shutdown: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("metrics server did not stop after context cancellation")
	}
}
