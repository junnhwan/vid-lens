package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"vid-lens/internal/ai"
	"vid-lens/internal/middleware"
	"vid-lens/internal/pkg/response"
	"vid-lens/internal/service"
)

type ChatHandler struct {
	chatSvc    *service.ChatService
	profileSvc *service.AIProfileService
	aiFactory  *ai.Factory
}

func NewChatHandler(chatSvc *service.ChatService, profileSvc *service.AIProfileService, aiFactory *ai.Factory) *ChatHandler {
	return &ChatHandler{chatSvc: chatSvc, profileSvc: profileSvc, aiFactory: aiFactory}
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

	result, err := h.chatSvc.Ask(c.Request.Context(), userID, sessionID, req.Question, req.TopK, embeddingClient, chatClient, *profile)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.OK(c, result)
}
