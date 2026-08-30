package handler

import (
	"context"
	"log"
	"strconv"

	"github.com/gin-gonic/gin"
	"vid-lens/internal/ai"
	"vid-lens/internal/middleware"
	"vid-lens/internal/pkg/response"
	"vid-lens/internal/service"
)

type ChatHandler struct {
	chatSvc    *service.ChatService
	agentSvc   videoAgentAsker
	profileSvc *service.AIProfileService
	aiFactory  *ai.Factory
	ledgerSvc  *service.EvidenceLedgerService
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

type videoAgentAsker interface {
	Ask(ctx context.Context, req service.VideoAgentRequest, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile) (*service.VideoAgentResult, error)
}

type videoResearchAsker interface {
	AskResearch(ctx context.Context, req service.VideoResearchRequest, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile) (*service.VideoAgentResult, error)
}

type videoAgentStreamer interface {
	Stream(ctx context.Context, req service.VideoAgentStreamRequest, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile, emit func(service.AgentStreamEvent) error) (*service.VideoAgentResult, error)
}

func NewChatHandler(chatSvc *service.ChatService, profileSvc *service.AIProfileService, aiFactory *ai.Factory) *ChatHandler {
	var agentSvc videoAgentAsker
	if chatSvc != nil {
		agentSvc = service.NewVideoAgentService(chatSvc)
	}
	return &ChatHandler{chatSvc: chatSvc, agentSvc: agentSvc, profileSvc: profileSvc, aiFactory: aiFactory}
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

	profile, err := h.profileSvc.GetDefaultAIProfile(userID)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	embeddingClient, err := h.aiFactory.NewEmbeddingClient(*profile)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	chatClient, err := h.aiFactory.NewChatClient(*profile)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	result, err := h.chatSvc.AskWithMode(c.Request.Context(), service.ChatMode(req.Mode), userID, sessionID, req.Question, req.TopK, embeddingClient, chatClient, *profile)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.OK(c, result)
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
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	if h.agentSvc == nil {
		response.BadRequest(c, "agent 实验功能不可用")
		return
	}

	profile, err := h.profileSvc.GetDefaultAIProfile(userID)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	embeddingClient, err := h.aiFactory.NewEmbeddingClient(*profile)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	chatClient, err := h.aiFactory.NewChatClient(*profile)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if req.Mode == "research" {
		researcher, ok := h.agentSvc.(videoResearchAsker)
		if !ok {
			response.BadRequest(c, "Video Research Agent 实验功能不可用")
			return
		}
		result, err := researcher.AskResearch(c.Request.Context(), service.VideoResearchRequest{
			UserID: userID, SessionID: sessionID, Goal: req.Question, TopK: req.TopK,
		}, embeddingClient, chatClient, *profile)
		if err != nil {
			response.BadRequest(c, err.Error())
			return
		}
		response.OK(c, result)
		return
	}

	result, err := h.agentSvc.Ask(c.Request.Context(), service.VideoAgentRequest{
		UserID:    userID,
		SessionID: sessionID,
		Question:  req.Question,
		TopK:      req.TopK,
	}, embeddingClient, chatClient, *profile)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.OK(c, result)
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

	profile, err := h.profileSvc.GetDefaultAIProfile(userID)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	embeddingClient, err := h.aiFactory.NewEmbeddingClient(*profile)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	chatClient, err := h.aiFactory.NewChatClient(*profile)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	_, err = h.chatSvc.AskStreamWithMode(c.Request.Context(), service.ChatMode(req.Mode), userID, sessionID, req.Question, req.TopK, embeddingClient, chatClient, *profile, func(event service.ChatStreamEvent) error {
		c.SSEvent(event.Type, event.Data)
		c.Writer.Flush()
		return nil
	})
	if err != nil {
		log.Printf("chat stream failed: user_id=%d session_id=%d mode=%q err=%v", userID, sessionID, req.Mode, err)
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
	streamer, ok := h.agentSvc.(videoAgentStreamer)
	if !ok {
		response.BadRequest(c, "agent 流式功能不可用")
		return
	}

	profile, err := h.profileSvc.GetDefaultAIProfile(userID)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	embeddingClient, err := h.aiFactory.NewEmbeddingClient(*profile)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	chatClient, err := h.aiFactory.NewChatClient(*profile)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	runID := ""
	emit := func(event service.AgentStreamEvent) error {
		if err := c.Request.Context().Err(); err != nil {
			return err
		}
		if event.Type == service.AgentEventRunStart {
			if start, ok := event.Data.(service.AgentRunStartEvent); ok {
				runID = start.RunID
			}
		}
		c.SSEvent(event.Type, event.Data)
		c.Writer.Flush()
		return c.Request.Context().Err()
	}

	_, err = streamer.Stream(c.Request.Context(), service.VideoAgentStreamRequest{
		UserID: userID, SessionID: sessionID, Question: req.Question,
		TopK: req.TopK, Mode: req.Mode, AgentProfile: req.AgentProfile,
	}, embeddingClient, chatClient, *profile, emit)
	if err != nil {
		log.Printf("agent chat stream failed: user_id=%d session_id=%d mode=%q err=%v", userID, sessionID, req.Mode, err)
		if c.Request.Context().Err() == nil {
			c.SSEvent(service.AgentEventError, service.AgentErrorEvent{RunID: runID, Message: err.Error()})
			c.Writer.Flush()
		}
	}
}
