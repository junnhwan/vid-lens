package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"vid-lens/internal/artifact"
	"vid-lens/internal/middleware"
	"vid-lens/internal/service"
)

type SummaryGenerationHandler struct {
	svc *service.SummaryGenerationReadService
}

func NewSummaryGenerationHandler(svc *service.SummaryGenerationReadService) *SummaryGenerationHandler {
	return &SummaryGenerationHandler{svc}
}
func (h *SummaryGenerationHandler) Latest(c *gin.Context) {
	id, ok := summaryTaskID(c)
	if !ok {
		return
	}
	view, err := h.svc.Latest(c.Request.Context(), middleware.GetUserID(c), id)
	artifactOK(c, http.StatusOK, view, err)
}
func (h *SummaryGenerationHandler) Events(c *gin.Context) {
	id, ok := summaryTaskID(c)
	if !ok {
		return
	}
	after := int64(0)
	limit := 100
	for _, key := range []string{"after_seq", "limit"} {
		if len(c.QueryArray(key)) > 1 {
			artifactError(c, artifact.Err("invalid_request", 400))
			return
		}
	}
	if raw, ok := c.GetQuery("after_seq"); ok {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 0 {
			artifactError(c, artifact.Err("invalid_request", 400))
			return
		}
		after = value
	}
	if raw, ok := c.GetQuery("limit"); ok {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 {
			artifactError(c, artifact.Err("invalid_request", 400))
			return
		}
		limit = value
	}
	page, err := h.svc.Events(c.Request.Context(), middleware.GetUserID(c), id, c.Param("generation_id"), after, limit)
	artifactOK(c, http.StatusOK, page, err)
}
