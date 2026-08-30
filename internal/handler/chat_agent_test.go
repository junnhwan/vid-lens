package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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
	result         *service.VideoAgentResult
	researchResult *service.VideoAgentResult
	funnelResult   *service.VideoAgentResult
	err            error
	researchErr    error
	funnelErr      error
	req            service.VideoAgentRequest
	researchReq    service.VideoResearchRequest
	funnelReq      service.EvidenceFunnelRequest
}

type fakeVideoAgentStreamService struct {
	result   *service.VideoAgentResult
	events   []service.AgentStreamEvent
	err      error
	request  service.VideoAgentStreamRequest
	canceled bool
}

func (s *fakeVideoAgentStreamService) Ask(context.Context, service.VideoAgentRequest, ai.EmbeddingClient, ai.ChatClient, ai.Profile) (*service.VideoAgentResult, error) {
	return nil, errors.New("unexpected legacy agent call")
}

func (s *fakeVideoAgentStreamService) Stream(ctx context.Context, req service.VideoAgentStreamRequest, _ ai.EmbeddingClient, _ ai.ChatClient, _ ai.Profile, emit func(service.AgentStreamEvent) error) (*service.VideoAgentResult, error) {
	s.request = req
	if s.canceled {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	for _, event := range s.events {
		if err := emit(event); err != nil {
			return nil, err
		}
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

func (s *fakeVideoAgentService) Ask(_ context.Context, req service.VideoAgentRequest, _ ai.EmbeddingClient, _ ai.ChatClient, _ ai.Profile) (*service.VideoAgentResult, error) {
	s.req = req
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

func (s *fakeVideoAgentService) AskResearch(_ context.Context, req service.VideoResearchRequest, _ ai.EmbeddingClient, _ ai.ChatClient, _ ai.Profile) (*service.VideoAgentResult, error) {
	s.researchReq = req
	if s.researchErr != nil {
		return nil, s.researchErr
	}
	return s.researchResult, nil
}

func (s *fakeVideoAgentService) AskEvidenceFunnel(_ context.Context, req service.EvidenceFunnelRequest, _ ai.EmbeddingClient, _ ai.ChatClient, _ ai.Profile) (*service.VideoAgentResult, error) {
	s.funnelReq = req
	if s.funnelErr != nil {
		return nil, s.funnelErr
	}
	return s.funnelResult, nil
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

func TestChatHandlerAskAgentResearchModeUsesResearchPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	profileSvc := newChatHandlerProfileServiceForTest(t)
	if _, err := profileSvc.Create(7, validHandlerAIProfileRequest()); err != nil {
		t.Fatalf("Create profile: %v", err)
	}
	agent := &fakeVideoAgentService{researchResult: &service.VideoAgentResult{
		Answer:   "research answer",
		Template: string(service.VideoAgentResearchTemplate),
	}}
	handler := NewChatHandler(nil, profileSvc, ai.NewFactory())
	handler.agentSvc = agent

	router := gin.New()
	router.POST("/chat/sessions/:session_id/messages/agent", func(c *gin.Context) {
		c.Set("userID", int64(7))
		handler.AskAgent(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/chat/sessions/22/messages/agent", bytes.NewBufferString(`{"question":"请研究 owner 校验的证据","mode":"research","top_k":4,"run_id":"resume-run-1"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if agent.researchReq.UserID != 7 || agent.researchReq.SessionID != 22 || agent.researchReq.Goal != "请研究 owner 校验的证据" || agent.researchReq.TopK != 4 || agent.researchReq.RunID != "resume-run-1" {
		t.Fatalf("research request = %+v", agent.researchReq)
	}
	if agent.req.Question != "" {
		t.Fatalf("legacy agent path should not be called: %+v", agent.req)
	}
}

func TestChatHandlerAskAgentEvidenceFunnelModeUsesBoundedPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	profileSvc := newChatHandlerProfileServiceForTest(t)
	if _, err := profileSvc.Create(7, validHandlerAIProfileRequest()); err != nil {
		t.Fatalf("Create profile: %v", err)
	}
	agent := &fakeVideoAgentService{funnelResult: &service.VideoAgentResult{Answer: "validated", Template: string(service.VideoAgentEvidenceFunnelTemplate)}}
	handler := NewChatHandler(nil, profileSvc, ai.NewFactory())
	handler.agentSvc = agent
	router := gin.New()
	router.POST("/chat/sessions/:session_id/messages/agent", func(c *gin.Context) {
		c.Set("userID", int64(7))
		handler.AskAgent(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/chat/sessions/22/messages/agent", bytes.NewBufferString(`{"question":"核验 owner","mode":"evidence_funnel","top_k":3,"run_id":"funnel-run"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if agent.funnelReq.UserID != 7 || agent.funnelReq.SessionID != 22 || agent.funnelReq.Goal != "核验 owner" || agent.funnelReq.TopK != 3 || agent.funnelReq.RunID != "funnel-run" {
		t.Fatalf("funnel request = %+v", agent.funnelReq)
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

func TestChatHandlerAskAgentStreamRequiresAuthOnRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewChatHandler(nil, nil, ai.NewFactory())
	router.POST("/api/v1/chat/sessions/:session_id/messages/agent/stream", middleware.JWTAuth("secret"), handler.AskAgentStream)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/sessions/22/messages/agent/stream", bytes.NewBufferString(`{"question":"q","mode":"agent"}`))
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

func TestChatHandlerAskAgentStreamWritesEventsAndForwardsRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	profileSvc := newChatHandlerProfileServiceForTest(t)
	if _, err := profileSvc.Create(7, validHandlerAIProfileRequest()); err != nil {
		t.Fatalf("Create profile: %v", err)
	}
	streamer := &fakeVideoAgentStreamService{
		result: &service.VideoAgentResult{MessageID: 21, Answer: "agent answer"},
		events: []service.AgentStreamEvent{
			{Type: service.AgentEventRunStart, Data: service.AgentRunStartEvent{RunID: "run-1", Mode: service.AgentStreamMode, ScopeType: model.ChatScopeVideo, TaskID: 9}},
			{Type: service.AgentEventAnswer, Data: "agent answer"},
			{Type: service.AgentEventCitations, Data: []service.Citation{}},
			{Type: service.AgentEventDone, Data: service.AgentDoneEvent{RunID: "run-1", MessageID: 21}},
		},
	}
	h := NewChatHandler(nil, profileSvc, ai.NewFactory())
	h.agentSvc = streamer

	router := gin.New()
	router.POST("/chat/sessions/:session_id/messages/agent/stream", func(c *gin.Context) {
		c.Set("userID", int64(7))
		h.AskAgentStream(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/chat/sessions/22/messages/agent/stream", bytes.NewBufferString(`{"question":"总结一下","top_k":3,"mode":"agent","agent_profile":"default"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("status=%d content-type=%q body=%s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	body := rec.Body.String()
	for _, fragment := range []string{
		"event:run_start", `"run_id":"run-1"`, "event:answer", "event:citations", "event:done",
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("stream body missing %q: %s", fragment, body)
		}
	}
	if streamer.request.UserID != 7 || streamer.request.SessionID != 22 || streamer.request.Question != "总结一下" || streamer.request.TopK != 3 || streamer.request.Mode != "agent" || streamer.request.AgentProfile != "default" {
		t.Fatalf("stream request = %+v", streamer.request)
	}
}

func TestChatHandlerAskAgentStreamWritesErrorEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	profileSvc := newChatHandlerProfileServiceForTest(t)
	if _, err := profileSvc.Create(7, validHandlerAIProfileRequest()); err != nil {
		t.Fatalf("Create profile: %v", err)
	}
	streamer := &fakeVideoAgentStreamService{
		events: []service.AgentStreamEvent{{Type: service.AgentEventRunStart, Data: service.AgentRunStartEvent{RunID: "run-error", Mode: service.AgentStreamMode, ScopeType: model.ChatScopeVideo}}},
		err:    errors.New("agent backend failed"),
	}
	h := NewChatHandler(nil, profileSvc, ai.NewFactory())
	h.agentSvc = streamer

	router := gin.New()
	router.POST("/chat/sessions/:session_id/messages/agent/stream", func(c *gin.Context) {
		c.Set("userID", int64(7))
		h.AskAgentStream(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/chat/sessions/22/messages/agent/stream", bytes.NewBufferString(`{"question":"失败测试","mode":"agent"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "event:error") || !strings.Contains(rec.Body.String(), "agent backend failed") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestChatHandlerListMessagesReturnsAgentSnapshotVerbatim(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, repos, _ := newKnowledgeBaseHandlerServiceTestEnv(t)
	task := createKnowledgeBaseHandlerTask(t, repos, 7, "agent-snapshot")
	session := &model.ChatSession{UserID: 7, TaskID: task.ID, ScopeType: model.ChatScopeVideo}
	if err := repos.Chat.CreateSession(session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	snapshot := `{"version":1,"run_id":"run-1","mode":"agent","steps":[],"citations":[]}`
	if err := repos.Chat.CreateMessage(&model.ChatMessage{SessionID: session.ID, UserID: 7, Role: "assistant", Content: "answer", RetrievalSnapshot: &snapshot}); err != nil {
		t.Fatalf("create message: %v", err)
	}

	h := NewChatHandler(service.NewChatService(repos, nil, service.ChatConfig{}), nil, nil)
	r := gin.New()
	r.GET("/chat/sessions/:session_id/messages", withTestUser(7), h.ListMessages)
	rec := serveKnowledgeBaseRequest(r, http.MethodGet, "/chat/sessions/"+strconv.FormatInt(session.ID, 10)+"/messages", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data []model.ChatMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	if len(body.Data) != 1 || body.Data[0].RetrievalSnapshot == nil || *body.Data[0].RetrievalSnapshot != snapshot {
		t.Fatalf("messages = %+v", body.Data)
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
