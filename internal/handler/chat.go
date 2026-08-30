package handler

import (
	"context"
	"errors"
	"log"
	"strconv"

	"github.com/gin-gonic/gin"
	"vid-lens/internal/middleware"
	"vid-lens/internal/pkg/response"
	"vid-lens/internal/service"
)

type ChatHandler struct {
	chatSvc   *service.ChatService
	execution conversationExecutor
	ledgerSvc *service.EvidenceLedgerService
}

func (h *ChatHandler) SetEvidenceLedgerService(ledger *service.EvidenceLedgerService) {
	if h != nil {
		h.ledgerSvc = ledger
	}
}

func (h *ChatHandler) GetEvidenceLedger(c *gin.Context) {
	if h == nil || h.ledgerSvc == nil {
		response.InternalError(c, "证据账本服务不可用")
		return
	}
	view, err := h.ledgerSvc.GetRun(c.Request.Context(), middleware.GetUserID(c), c.Param("run_id"))
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if view == nil {
		response.Forbidden(c, "无权访问此证据账本或账本不存在")
		return
	}
	response.OK(c, view)
}

func (h *ChatHandler) CorrectEvidenceClaim(c *gin.Context) {
	if denyIfDemo(c, "更正 Agent Claim") {
		return
	}
	if h == nil || h.ledgerSvc == nil {
		response.InternalError(c, "证据账本服务不可用")
		return
	}
	var req service.EvidenceClaimCorrectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	claim, err := h.ledgerSvc.CorrectClaim(c.Request.Context(), middleware.GetUserID(c), c.Param("claim_id"), req)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if claim == nil {
		response.Forbidden(c, "无权更正此 Claim 或 Claim 不存在")
		return
	}
	response.OK(c, claim)
}

type conversationExecutor interface {
	Execute(ctx context.Context, req service.ConversationRequest) (service.ConversationResult, error)
	Stream(ctx context.Context, req service.ConversationRequest, sink service.ConversationStreamSink) (service.ConversationResult, error)
}

func NewChatHandler(chatSvc *service.ChatService, execution conversationExecutor) *ChatHandler {
	return &ChatHandler{chatSvc: chatSvc, execution: execution}
}

func (h *ChatHandler) CreateSession(c *gin.Context) {
	userID := middleware.GetUserID(c)
	var req struct {
		TaskID          int64  `json:"task_id"`
		ScopeType       string `json:"scope_type"`
		KnowledgeBaseID int64  `json:"knowledge_base_id"`
		Title           string `json:"title"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	session, err := h.chatSvc.CreateScopedSession(userID, service.CreateChatSessionRequest{TaskID: req.TaskID, ScopeType: req.ScopeType, KnowledgeBaseID: req.KnowledgeBaseID, Title: req.Title})
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.OK(c, session)
}

func (h *ChatHandler) ListSessions(c *gin.Context) {
	userID := middleware.GetUserID(c)
	taskID, _ := strconv.ParseInt(c.Query("task_id"), 10, 64)
	knowledgeBaseID, _ := strconv.ParseInt(c.Query("knowledge_base_id"), 10, 64)
	sessions, err := h.chatSvc.ListSessionsWithFilter(userID, service.ListChatSessionsFilter{TaskID: taskID, KnowledgeBaseID: knowledgeBaseID, ScopeType: c.Query("scope_type")})
	if err != nil {
		response.InternalError(c, "查询会话失败")
		return
	}
	response.OK(c, sessions)
}

func (h *ChatHandler) ListMessages(c *gin.Context) {
	userID := middleware.GetUserID(c)
	sessionID, err := strconv.ParseInt(c.Param("session_id"), 10, 64)
	if err != nil || sessionID <= 0 {
		response.BadRequest(c, "会话 ID 错误")
		return
	}
	messages, err := h.chatSvc.ListMessages(userID, sessionID)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.OK(c, messages)
}

func (h *ChatHandler) DeleteSession(c *gin.Context) {
	userID := middleware.GetUserID(c)
	sessionID, err := strconv.ParseInt(c.Param("session_id"), 10, 64)
	if err != nil || sessionID <= 0 {
		response.BadRequest(c, "会话 ID 错误")
		return
	}
	if err := h.chatSvc.DeleteSession(userID, sessionID); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.OK(c, gin.H{"deleted": true})
}

func (h *ChatHandler) Ask(c *gin.Context) {
	userID := middleware.GetUserID(c)
	sessionID, err := strconv.ParseInt(c.Param("session_id"), 10, 64)
	if err != nil || sessionID <= 0 {
		response.BadRequest(c, "会话 ID 错误")
		return
	}

	var req struct {
		Question string `json:"question" binding:"required"`
		TopK     int    `json:"top_k"`
		Mode     string `json:"mode"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	if h.execution == nil {
		response.BadRequest(c, "chat service unavailable")
		return
	}
	result, err := h.execution.Execute(c.Request.Context(), service.ConversationRequest{
		Kind: service.ConversationKindChat, UserID: userID, SessionID: sessionID,
		Question: req.Question, TopK: req.TopK, Mode: req.Mode,
	})
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.OK(c, result.Payload())
}

// AskAgent handles the experimental tool-loop video QA path. mode=research
// opts into the bounded goal-driven runner; the default remains the template
// baseline for comparison and fallback.
func (h *ChatHandler) AskAgent(c *gin.Context) {
	userID := middleware.GetUserID(c)
	sessionID, err := strconv.ParseInt(c.Param("session_id"), 10, 64)
	if err != nil || sessionID <= 0 {
		response.BadRequest(c, "会话 ID 错误")
		return
	}

	var req struct {
		Question string `json:"question" binding:"required"`
		TopK     int    `json:"top_k"`
		Mode     string `json:"mode"`
		RunID    string `json:"run_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	if h.execution == nil {
		response.BadRequest(c, "agent 实验功能不可用")
		return
	}
	result, err := h.execution.Execute(c.Request.Context(), service.ConversationRequest{
		Kind: service.ConversationKindAgent, UserID: userID, SessionID: sessionID,
		Question: req.Question, TopK: req.TopK, Mode: req.Mode, RunID: req.RunID,
	})
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.OK(c, result.Payload())
}

func (h *ChatHandler) AskStream(c *gin.Context) {
	userID := middleware.GetUserID(c)
	sessionID, err := strconv.ParseInt(c.Param("session_id"), 10, 64)
	if err != nil || sessionID <= 0 {
		response.BadRequest(c, "会话 ID 错误")
		return
	}

	var req struct {
		Question string `json:"question" binding:"required"`
		TopK     int    `json:"top_k"`
		Mode     string `json:"mode"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	if h.execution == nil {
		response.BadRequest(c, "chat service unavailable")
		return
	}
	started := false
	_, err = h.execution.Stream(c.Request.Context(), service.ConversationRequest{
		Kind: service.ConversationKindChat, UserID: userID, SessionID: sessionID,
		Question: req.Question, TopK: req.TopK, Mode: req.Mode,
	}, func(event service.ConversationStreamEvent) error {
		startConversationSSE(c)
		started = true
		c.SSEvent(event.Type, event.Data)
		c.Writer.Flush()
		return c.Request.Context().Err()
	})
	if err != nil {
		var preparation *service.ConversationPreparationError
		if !started && errors.As(err, &preparation) {
			response.BadRequest(c, err.Error())
			return
		}
		log.Printf("chat stream failed: user_id=%d session_id=%d mode=%q err=%v", userID, sessionID, req.Mode, err)
		startConversationSSE(c)
		c.SSEvent("error", gin.H{"message": err.Error()})
		c.Writer.Flush()
	}
}

// AskAgentStream exposes the bounded, single-video template Agent as SSE.
// Research mode and knowledge-base scope stay on their existing guarded paths.
func (h *ChatHandler) AskAgentStream(c *gin.Context) {
	userID := middleware.GetUserID(c)
	sessionID, err := strconv.ParseInt(c.Param("session_id"), 10, 64)
	if err != nil || sessionID <= 0 {
		response.BadRequest(c, "会话 ID 错误")
		return
	}

	var req struct {
		Question     string `json:"question" binding:"required"`
		TopK         int    `json:"top_k"`
		Mode         string `json:"mode"`
		AgentProfile string `json:"agent_profile"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	if h.execution == nil {
		response.BadRequest(c, "agent 流式功能不可用")
		return
	}

	runID := ""
	started := false
	emit := func(event service.ConversationStreamEvent) error {
		if err := c.Request.Context().Err(); err != nil {
			return err
		}
		startConversationSSE(c)
		started = true
		if event.Type == service.AgentEventRunStart {
			if start, ok := event.Data.(service.AgentRunStartEvent); ok {
				runID = start.RunID
			}
		}
		c.SSEvent(event.Type, event.Data)
		c.Writer.Flush()
		return c.Request.Context().Err()
	}

	_, err = h.execution.Stream(c.Request.Context(), service.ConversationRequest{
		Kind: service.ConversationKindAgent, UserID: userID, SessionID: sessionID, Question: req.Question,
		TopK: req.TopK, Mode: req.Mode, AgentProfile: req.AgentProfile,
	}, emit)
	if err != nil {
		var preparation *service.ConversationPreparationError
		if !started && errors.As(err, &preparation) {
			response.BadRequest(c, err.Error())
			return
		}
		log.Printf("agent chat stream failed: user_id=%d session_id=%d mode=%q err=%v", userID, sessionID, req.Mode, err)
		if c.Request.Context().Err() == nil {
			startConversationSSE(c)
			c.SSEvent(service.AgentEventError, service.AgentErrorEvent{RunID: runID, Message: err.Error()})
			c.Writer.Flush()
		}
	}
}

func startConversationSSE(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
}
