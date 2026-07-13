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
}

type videoAgentAsker interface {
	Ask(ctx context.Context, req service.VideoAgentRequest, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile) (*service.VideoAgentResult, error)
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
		TaskID int64  `json:"task_id" binding:"required"`
		Title  string `json:"title"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	session, err := h.chatSvc.CreateSession(userID, req.TaskID, req.Title)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.OK(c, session)
}

func (h *ChatHandler) ListSessions(c *gin.Context) {
	userID := middleware.GetUserID(c)
	taskID, _ := strconv.ParseInt(c.Query("task_id"), 10, 64)
	sessions, err := h.chatSvc.ListSessions(userID, taskID)
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
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	if h.agentSvc == nil {
		response.BadRequest(c, "agent 服务不可用")
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
