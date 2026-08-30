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
	"vid-lens/internal/middleware"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/response"
	"vid-lens/internal/service"
)

type fakeConversationExecution struct {
	result         *service.VideoAgentResult
	researchResult *service.VideoAgentResult
	funnelResult   *service.VideoAgentResult
	streamResult   *service.VideoAgentResult
	events         []service.ConversationStreamEvent
	err            error
	researchErr    error
	funnelErr      error
	streamErr      error
	request        service.ConversationRequest
	streamRequest  service.ConversationRequest
	canceled       bool
}

func (s *fakeConversationExecution) Execute(_ context.Context, req service.ConversationRequest) (service.ConversationResult, error) {
	s.request = req
	switch req.Mode {
	case "research":
		return service.ConversationResult{Agent: s.researchResult}, s.researchErr
	case string(service.VideoAgentEvidenceFunnelTemplate):
		return service.ConversationResult{Agent: s.funnelResult}, s.funnelErr
	default:
		return service.ConversationResult{Agent: s.result}, s.err
	}
}

func (s *fakeConversationExecution) Stream(ctx context.Context, req service.ConversationRequest, emit service.ConversationStreamSink) (service.ConversationResult, error) {
	s.streamRequest = req
	if s.canceled {
		<-ctx.Done()
		return service.ConversationResult{}, ctx.Err()
	}
	for _, event := range s.events {
		if err := emit(event); err != nil {
			return service.ConversationResult{}, err
		}
	}
	if s.streamErr != nil {
		return service.ConversationResult{}, s.streamErr
	}
	return service.ConversationResult{Agent: s.streamResult}, nil
}

func TestChatHandlerAskAgentReturnsAgenticResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	execution := &fakeConversationExecution{result: &service.VideoAgentResult{
		MessageID: 12,
		Answer:    "agent answer",
		Template:  string(service.VideoAgentSummarizeTopic),
		Citations: []service.Citation{{CitationID: "C1", ChunkID: 1, ChunkIndex: 2, Content: "citation"}},
		Trace:     []service.VideoAgentStep{{Name: "search topic", Tool: service.VideoAgentToolSearchTranscript, OutputRef: "citations:1"}},
		Model:     "chat-model",
	}}
	handler := NewChatHandler(nil, execution)

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
	if execution.request.Kind != service.ConversationKindAgent || execution.request.UserID != 7 || execution.request.SessionID != 22 || execution.request.TopK != 3 {
		t.Fatalf("agent request = %+v", execution.request)
	}
}

func TestChatHandlerAskAgentResearchModeUsesResearchPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	execution := &fakeConversationExecution{researchResult: &service.VideoAgentResult{
		Answer:   "research answer",
		Template: string(service.VideoAgentResearchTemplate),
	}}
	handler := NewChatHandler(nil, execution)

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
	if execution.request.UserID != 7 || execution.request.SessionID != 22 || execution.request.Question != "请研究 owner 校验的证据" || execution.request.TopK != 4 || execution.request.RunID != "resume-run-1" || execution.request.Mode != "research" {
		t.Fatalf("research request = %+v", execution.request)
	}
}

func TestChatHandlerAskAgentEvidenceFunnelModeUsesBoundedPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	execution := &fakeConversationExecution{funnelResult: &service.VideoAgentResult{Answer: "validated", Template: string(service.VideoAgentEvidenceFunnelTemplate)}}
	handler := NewChatHandler(nil, execution)
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
	if execution.request.UserID != 7 || execution.request.SessionID != 22 || execution.request.Question != "核验 owner" || execution.request.TopK != 3 || execution.request.RunID != "funnel-run" || execution.request.Mode != string(service.VideoAgentEvidenceFunnelTemplate) {
		t.Fatalf("funnel request = %+v", execution.request)
	}
}

func TestChatHandlerAskAgentRequiresAuthOnRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewChatHandler(nil, nil)
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
	handler := NewChatHandler(nil, nil)
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
	execution := &fakeConversationExecution{err: errors.New("当前视频尚未构建 RAG 索引")}
	handler := NewChatHandler(nil, execution)

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
	execution := &fakeConversationExecution{
		streamResult: &service.VideoAgentResult{MessageID: 21, Answer: "agent answer"},
		events: []service.ConversationStreamEvent{
			{Type: service.AgentEventRunStart, Data: service.AgentRunStartEvent{RunID: "run-1", Mode: service.AgentStreamMode, ScopeType: model.ChatScopeVideo, TaskID: 9}},
			{Type: service.AgentEventAnswer, Data: "agent answer"},
			{Type: service.AgentEventCitations, Data: []service.Citation{}},
			{Type: service.AgentEventDone, Data: service.AgentDoneEvent{RunID: "run-1", MessageID: 21}},
		},
	}
	h := NewChatHandler(nil, execution)

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
	if execution.streamRequest.Kind != service.ConversationKindAgent || execution.streamRequest.UserID != 7 || execution.streamRequest.SessionID != 22 || execution.streamRequest.Question != "总结一下" || execution.streamRequest.TopK != 3 || execution.streamRequest.Mode != "agent" || execution.streamRequest.AgentProfile != "default" {
		t.Fatalf("stream request = %+v", execution.streamRequest)
	}
}

func TestChatHandlerAskAgentStreamWritesErrorEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	execution := &fakeConversationExecution{
		events:    []service.ConversationStreamEvent{{Type: service.AgentEventRunStart, Data: service.AgentRunStartEvent{RunID: "run-error", Mode: service.AgentStreamMode, ScopeType: model.ChatScopeVideo}}},
		streamErr: errors.New("agent backend failed"),
	}
	h := NewChatHandler(nil, execution)

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

	h := NewChatHandler(service.NewChatService(repos, nil, service.ChatConfig{}), nil)
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
