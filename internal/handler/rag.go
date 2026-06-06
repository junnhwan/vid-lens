package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"vid-lens/internal/ai"
	"vid-lens/internal/middleware"
	"vid-lens/internal/pkg/response"
	"vid-lens/internal/service"
)

type RAGHandler struct {
	indexSvc   *service.RAGIndexService
	profileSvc *service.AIProfileService
	aiFactory  *ai.Factory
}

func NewRAGHandler(indexSvc *service.RAGIndexService, profileSvc *service.AIProfileService, aiFactory *ai.Factory) *RAGHandler {
	return &RAGHandler{indexSvc: indexSvc, profileSvc: profileSvc, aiFactory: aiFactory}
}

func (h *RAGHandler) BuildTaskIndex(c *gin.Context) {
	userID := middleware.GetUserID(c)
	taskID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || taskID <= 0 {
		response.BadRequest(c, "任务 ID 错误")
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

	result, err := h.indexSvc.BuildTaskIndex(c.Request.Context(), userID, taskID, embeddingClient, *profile)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.OK(c, result)
}

func (h *RAGHandler) GetTaskIndexStatus(c *gin.Context) {
	userID := middleware.GetUserID(c)
	taskID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || taskID <= 0 {
		response.BadRequest(c, "任务 ID 错误")
		return
	}

	profile, err := h.profileSvc.GetDefaultAIProfile(userID)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	result, err := h.indexSvc.GetTaskIndexStatus(c.Request.Context(), userID, taskID, *profile)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.OK(c, result)
}
