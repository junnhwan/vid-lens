package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"vid-lens/internal/config"
	"vid-lens/internal/handler"
	"vid-lens/internal/middleware"
	"vid-lens/internal/service"
)

// The stream route carries its credential in the query string because browser
// media elements cannot set an Authorization header. A stale session token in
// the request headers must not be able to shadow it, which only holds while the
// route stays outside the JWT group.
func TestStreamRouteAuthenticatesWithoutSessionToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := newServerRouter(
		config.Config{},
		serverHandlers{media: handler.NewMediaHandler(service.NewMediaService(nil, nil, nil, nil, config.UploadConfig{}, config.ToolsConfig{}, config.JWTConfig{}))},
		middleware.NewRateLimiter(nil, 10, 10),
		nil,
	)

	// An invalid token still reaches the handler and is rejected by the
	// credential check, not by the JWT middleware.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/media/task/31/stream?token=invalid", nil)
	router.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("stream route is inside the JWT group: got 401, want the credential check to decide")
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for an invalid credential", rec.Code)
	}

	// The same request with a stale session header must behave identically.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/media/task/31/stream?token=invalid", nil)
	req.Header.Set("Authorization", "Bearer stale-session-token")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status with stale session header = %d, want 404", rec.Code)
	}
}

func TestStreamRouteRejectsMalformedTaskID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := newServerRouter(
		config.Config{},
		serverHandlers{media: handler.NewMediaHandler(service.NewMediaService(nil, nil, nil, nil, config.UploadConfig{}, config.ToolsConfig{}, config.JWTConfig{}))},
		middleware.NewRateLimiter(nil, 10, 10),
		nil,
	)

	for _, path := range []string{"/api/v1/media/task/abc/stream?token=x", "/api/v1/media/task/0/stream?token=x"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, want 400", path, rec.Code)
		}
	}
}
