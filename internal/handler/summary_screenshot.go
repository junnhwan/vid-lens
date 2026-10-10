package handler

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"vid-lens/internal/middleware"
)

func (h *MediaHandler) SummaryScreenshot(c *gin.Context) {
	taskID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || taskID <= 0 {
		c.Status(http.StatusBadRequest)
		return
	}
	object, err := h.svc.OpenSummaryScreenshot(c.Request.Context(), middleware.GetUserID(c), taskID, c.Param("ref"))
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	defer object.Close()
	info, err := object.Stat()
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Content-Type", "image/jpeg")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "private, no-store")
	http.ServeContent(c.Writer, c.Request, "summary-image.jpg", info.LastModified, object)
}
