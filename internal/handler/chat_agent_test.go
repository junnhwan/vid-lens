package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/ai"
	"vid-lens/internal/middleware"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/response"
	"vid-lens/internal/pkg/secret"
	"vid-lens/internal/repository"
	"vid-lens/internal/service"
)

type fakeVideoAgentService struct {
	result *service.VideoAgentResult
	err    error
	req    service.VideoAgentRequest
}

func (s *fakeVideoAgentService) Ask(_ context.Context, req service.VideoAgentRequest, _ ai.EmbeddingClient, _ ai.ChatClient, _ ai.Profile) (*service.VideoAgentResult, error) {
	s.req = req
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

func TestChatHandlerAskAgentReturnsAgenticResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	profileSvc := newChatHandlerProfileServiceForTest(t)
	if _, err := profileSvc.Create(7, validHandlerAIProfileRequest()); err != nil {
		t.Fatalf("Create profile: %v", err)
	}
	agent := &fakeVideoAgentService{result: &service.VideoAgentResult{
		MessageID: 12,
		Answer:    "agent answer",
		Template:  string(service.VideoAgentSummarizeTopic),
		Citations: []service.Citation{{CitationID: "C1", ChunkID: 1, ChunkIndex: 2, Content: "citation"}},
		Trace:     []service.VideoAgentStep{{Name: "search topic", Tool: service.VideoAgentToolSearchTranscript, OutputRef: "citations:1"}},
		Model:     "chat-model",
	}}
	handler := NewChatHandler(nil, profileSvc, ai.NewFactory())
	handler.agentSvc = agent

	router := gin.New()
	router.POST("/chat/sessions/:session_id/messages/agent", func(c *gin.Context) {
		c.Set("userID", int64(7))
		handler.AskAgent(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/chat/sessions/22/messages/agent", bytes.NewBufferString(`{"question":"总结一下 owner 风险","top_k":3}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Code int `json:"code"`
		Data struct {
			MessageID int64                    `json:"message_id"`
			Answer    string                   `json:"answer"`
			Template  string                   `json:"template"`
			Citations []service.Citation       `json:"citations"`
			Trace     []service.VideoAgentStep `json:"trace"`
			Model     string                   `json:"model"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body.Code != 200 || body.Data.MessageID != 12 || body.Data.Template != string(service.VideoAgentSummarizeTopic) || len(body.Data.Trace) != 1 {
		t.Fatalf("body = %+v", body)
	}
	if agent.req.UserID != 7 || agent.req.SessionID != 22 || agent.req.TopK != 3 {
		t.Fatalf("agent request = %+v", agent.req)
	}
}

func TestChatHandlerAskAgentRequiresAuthOnRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewChatHandler(nil, nil, ai.NewFactory())
	router.POST("/api/v1/chat/sessions/:session_id/messages/agent", middleware.JWTAuth("secret"), handler.AskAgent)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/sessions/22/messages/agent", bytes.NewBufferString(`{"question":"q"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body = %s", rec.Code, rec.Body.String())
	}
}

func TestChatHandlerAskAgentReturnsClearRAGMissingError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	profileSvc := newChatHandlerProfileServiceForTest(t)
	if _, err := profileSvc.Create(7, validHandlerAIProfileRequest()); err != nil {
		t.Fatalf("Create profile: %v", err)
	}
	handler := NewChatHandler(nil, profileSvc, ai.NewFactory())
	handler.agentSvc = &fakeVideoAgentService{err: errors.New("当前视频尚未构建 RAG 索引")}

	router := gin.New()
	router.POST("/chat/sessions/:session_id/messages/agent", func(c *gin.Context) {
		c.Set("userID", int64(7))
		handler.AskAgent(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/chat/sessions/22/messages/agent", bytes.NewBufferString(`{"question":"总结一下"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body response.Response
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body.Message != "当前视频尚未构建 RAG 索引" {
		t.Fatalf("message = %q", body.Message)
	}
}

func newChatHandlerProfileServiceForTest(t *testing.T) *service.AIProfileService {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.UserAIProfile{}); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	repos := repository.NewRepositories(db)
	codec, err := secret.NewCodec("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	return service.NewAIProfileService(repos.AIProfile, codec, nil)
}
