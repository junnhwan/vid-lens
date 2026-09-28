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
		user:           &handler.UserHandler{},
		profiles:       &handler.AIProfileHandler{},
		rag:            &handler.RAGHandler{},
		chat:           &handler.ChatHandler{},
		feedback:       &handler.ChatFeedbackHandler{},
		media:          &handler.MediaHandler{},
		knowledgeBases: &handler.KnowledgeBaseHandler{},
		memory:         &handler.MemoryHandler{},
		artifacts:      &handler.ArtifactHandler{},
	}, nil, nil)

	if router == nil {
		t.Fatal("newServerRouter() returned nil")
	}

	want := map[string]string{
		"POST /api/v1/artifact-runs":                                             "background artifact generation",
		"GET /api/v1/artifact-runs/:id/events":                                   "durable artifact events",
		"POST /api/v1/artifacts/:id/edit-runs":                                   "bounded artifact agent edit",
		"GET /api/v1/artifact-edit-runs/:id":                                     "durable artifact edit run",
		"GET /api/v1/artifact-edit-runs/:id/events":                              "durable artifact edit events",
		"POST /api/v1/artifact-edit-runs/:id/cancel":                             "explicit artifact edit cancellation",
		"GET /api/v1/artifact-edit-operations/:id":                               "artifact edit diff",
		"POST /api/v1/artifact-edit-operations/:id/apply":                        "apply artifact edit proposal",
		"POST /api/v1/artifact-edit-operations/:id/undo":                         "safe artifact edit undo",
		"PATCH /api/v1/artifacts/:id":                                            "versioned artifact edits",
		"GET /api/v1/sources/:manifest_id/evidence/:evidence_id":                 "authorized evidence",
		"GET /api/v1/tasks":                                                      "task projection",
		"GET /healthz":                                                           "health endpoint",
		"GET /readyz":                                                            "readiness endpoint",
		"POST /api/v1/user/register":                                             "public registration",
		"GET /api/v1/chat/feedback/candidates":                                   "owner feedback candidates",
		"PUT /api/v1/chat/sessions/:session_id/messages/:message_id/feedback":    "save feedback",
		"GET /api/v1/chat/sessions/:session_id/messages/:message_id/feedback":    "read feedback",
		"DELETE /api/v1/chat/sessions/:session_id/messages/:message_id/feedback": "clear feedback",
		"POST /api/v1/chat/sessions/:session_id/messages":                        "chat message",
		"POST /api/v1/chat/sessions/:session_id/messages/agent/stream":           "agent chat stream",
		"POST /api/v1/media/upload-chunk":                                        "upload chunk",
		"GET /api/v1/media/check-upload":                                         "check uploaded chunks",
		"POST /api/v1/media/merge-chunks":                                        "merge uploaded chunks",
		"PATCH /api/v1/media/task/:id":                                           "update video title",
		"POST /api/v1/knowledge-bases":                                           "create knowledge base",
		"GET /api/v1/knowledge-bases":                                            "list knowledge bases",
		"GET /api/v1/knowledge-bases/:id":                                        "get knowledge base",
		"PATCH /api/v1/knowledge-bases/:id":                                      "update knowledge base",
		"DELETE /api/v1/knowledge-bases/:id":                                     "delete knowledge base",
		"POST /api/v1/knowledge-bases/:id/videos":                                "add knowledge base video",
		"DELETE /api/v1/knowledge-bases/:id/videos/:task_id":                     "remove knowledge base video",
		"GET /api/v1/memories":                                                   "list memories",
		"POST /api/v1/memories/:memory_id/withdraw":                              "withdraw memory",
		"DELETE /api/v1/memories/:memory_id":                                     "delete memory",
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
