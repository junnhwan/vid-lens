package handler

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"vid-lens/internal/middleware"
	"vid-lens/internal/repository"
)

func (h *SummaryRevisionHandler) BlockContext(c *gin.Context) {
	id, ok := summaryTaskID(c)
	if !ok {
		return
	}
	version, err := repository.ParseSummaryVersionRef(c.Query("version_ref"))
	if err != nil {
		artifactError(c, err)
		return
	}
	block, err := h.repos.SummaryBlockContext(c.Request.Context(), middleware.GetUserID(c), id, c.Param("block_id"), version)
	artifactOK(c, http.StatusOK, block, err)
}
