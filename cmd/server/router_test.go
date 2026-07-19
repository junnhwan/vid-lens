package main

import (
	"testing"

	"github.com/gin-gonic/gin"
	"vid-lens/internal/config"
	"vid-lens/internal/handler"
)

func TestNewServerRouterRegistersCoreRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := newServerRouter(config.Config{
		JWT: config.JWTConfig{Secret: "test-secret"},
	}, serverHandlers{
		user:     &handler.UserHandler{},
		profiles: &handler.AIProfileHandler{},
		rag:      &handler.RAGHandler{},
		chat:     &handler.ChatHandler{},
		media:    &handler.MediaHandler{},
	}, nil, nil)

	if router == nil {
		t.Fatal("newServerRouter() returned nil")
	}

	want := map[string]string{
		"GET /healthz":               "health endpoint",
		"GET /readyz":                "readiness endpoint",
		"POST /api/v1/user/register": "public registration",
		"POST /api/v1/chat/sessions/:session_id/messages": "chat message",
		"POST /api/v1/media/upload-chunk":                 "upload chunk",
		"GET /api/v1/media/check-upload":                  "check uploaded chunks",
		"POST /api/v1/media/merge-chunks":                 "merge uploaded chunks",
	}
	registered := make(map[string]struct{}, len(router.Routes()))
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = struct{}{}
	}
	for route, description := range want {
		if _, ok := registered[route]; !ok {
			t.Errorf("missing %s route %s", description, route)
		}
	}
	for _, removed := range []string{
		"POST /api/v1/media/upload-sessions",
		"GET /api/v1/media/upload-sessions/:session_id",
		"PUT /api/v1/media/upload-sessions/:session_id/chunks/:index",
		"POST /api/v1/media/upload-sessions/:session_id/complete",
	} {
		if _, ok := registered[removed]; ok {
			t.Errorf("PostgreSQL upload-session route is still registered: %s", removed)
		}
	}
}
