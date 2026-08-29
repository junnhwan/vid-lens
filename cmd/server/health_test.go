package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestLivenessHandlerAlwaysReportsProcessAlive(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/healthz", livenessHandler())

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d, want %d", response.Code, http.StatusOK)
	}
}

func TestReadinessHandlerRejectsRequiredDependencyFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/readyz", readinessHandler([]dependencyCheck{
		{Name: "database", Required: true, Check: func(context.Context) error { return errors.New("down") }},
		{Name: "vector", Required: false, Check: func(context.Context) error { return nil }},
	}, time.Second))

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	if body := response.Body.String(); !strings.Contains(body, `"status":"not_ready"`) {
		t.Fatalf("body=%s", body)
	}
}

func TestReadinessHandlerReportsOptionalDependencyAsDegraded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/readyz", readinessHandler([]dependencyCheck{
		{Name: "database", Required: true, Check: func(context.Context) error { return nil }},
		{Name: "vector", Required: false, Check: func(context.Context) error { return errors.New("disabled") }},
	}, time.Second))

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d, want %d", response.Code, http.StatusOK)
	}
	if body := response.Body.String(); !strings.Contains(body, `"status":"degraded"`) {
		t.Fatalf("body=%s", body)
	}
}
