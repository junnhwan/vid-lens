package handler

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"vid-lens/internal/artifact"
	"vid-lens/internal/middleware"
	"vid-lens/internal/service"
)

func (h *MediaHandler) RequestSourceRefresh(c *gin.Context) {
	if denyIfDemo(c, "刷新文字来源") {
		return
	}
	id, ok := summaryTaskID(c)
	if !ok {
		return
	}
	if len(c.Request.Header.Values("Idempotency-Key")) != 1 {
		artifactError(c, artifact.Err("invalid_idempotency_key", 400))
		return
	}
	var input service.SourceRefreshRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		artifactError(c, artifact.Err("invalid_source_refresh", 400))
		return
	}
	result, err := h.svc.RequestSourceRefresh(c.Request.Context(), middleware.GetUserID(c), id, c.GetHeader("Idempotency-Key"), input)
	artifactOK(c, http.StatusAccepted, result, err)
}
