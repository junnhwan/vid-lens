package handler

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"vid-lens/internal/artifact"
	"vid-lens/internal/middleware"
	"vid-lens/internal/service"
)

func (h *MediaHandler) RequestSummaryVisualRetry(c *gin.Context) {
	id, ok := summaryTaskID(c)
	if !ok {
		return
	}
	var input service.SummaryVisualRetryRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		artifactError(c, artifact.Err("invalid_visual_retry_request", 400))
		return
	}
	if len(c.Request.Header.Values("Idempotency-Key")) != 1 {
		artifactError(c, artifact.Err("invalid_idempotency_key", 400))
		return
	}
	result, err := h.svc.RequestSummaryVisualRetry(c.Request.Context(), middleware.GetUserID(c), id, c.GetHeader("Idempotency-Key"), input)
	artifactOK(c, http.StatusAccepted, result, err)
}
