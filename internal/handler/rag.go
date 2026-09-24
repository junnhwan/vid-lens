package handler

import (
	"context"
	"errors"
	"strconv"
	"time"

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
	if denyIfDemo(c, "触发索引") {
		return
	}
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
	current, err := h.indexSvc.GetTaskIndexStatus(c.Request.Context(), userID, taskID, *profile)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if current.Status == "queued" || current.Status == "indexing" {
		response.OK(c, current)
		return
	}
	embeddingClient, err := h.aiFactory.NewEmbeddingClient(*profile)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	// The build is synchronous today, but leaving the detail page must not
	// cancel an already claimed projection and leave its persisted status stale.
	buildCtx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), time.Hour)
	defer cancel()
	result, err := h.indexSvc.BuildTaskIndex(buildCtx, userID, taskID, embeddingClient, *profile)
	if err != nil {
		if errors.Is(err, service.ErrRAGIndexAlreadyBuilding) {
			current, statusErr := h.indexSvc.GetTaskIndexStatus(c.Request.Context(), userID, taskID, *profile)
			if statusErr == nil {
				response.OK(c, current)
				return
			}
		}
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
