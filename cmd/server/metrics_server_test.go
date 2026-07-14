package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
