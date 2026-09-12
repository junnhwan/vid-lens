package handler

import (
	"github.com/gin-gonic/gin"
	"strconv"
	"vid-lens/internal/middleware"
	"vid-lens/internal/pkg/response"
)

func (h *ChatHandler) ListRunHistory(c *gin.Context) {
	sessionID, err := strconv.ParseInt(c.Param("session_id"), 10, 64)
	if err != nil || sessionID <= 0 || h.chatSvc == nil {
		response.BadRequest(c, "会话 ID 错误")
		return
	}
	runs, err := h.chatSvc.ListUnfinishedRunHistory(c.Request.Context(), middleware.GetUserID(c), sessionID)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.OK(c, runs)
}
