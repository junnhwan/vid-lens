package handler

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"vid-lens/internal/middleware"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/response"
	"vid-lens/internal/service"
)

type MemoryHandler struct {
	service *service.MemoryGovernanceService
}

func NewMemoryHandler(memory *service.MemoryGovernanceService) *MemoryHandler {
	return &MemoryHandler{service: memory}
}

func (h *MemoryHandler) List(c *gin.Context) {
	userID := middleware.GetUserID(c)
	scopeType := strings.TrimSpace(c.Query("scope_type"))
	scopeID := strings.TrimSpace(c.Query("scope_id"))
	if scopeType == "" {
		scopeType, scopeID = model.MemoryScopeUser, strconv.FormatInt(userID, 10)
	}
	if scopeID == "" {
		response.BadRequest(c, "scope_id 不能为空")
		return
	}
	items, err := h.service.List(c.Request.Context(), userID, service.MemoryScope{Type: scopeType, ID: scopeID})
	if err != nil {
		response.Forbidden(c, err.Error())
		return
	}
	response.OK(c, items)
}

func (h *MemoryHandler) Withdraw(c *gin.Context) {
	if err := h.service.Withdraw(c.Request.Context(), middleware.GetUserID(c), c.Param("memory_id")); err != nil {
		response.Forbidden(c, err.Error())
		return
	}
	response.OKWithMsg(c, "memory withdrawn", nil)
}

func (h *MemoryHandler) Delete(c *gin.Context) {
	if err := h.service.Delete(c.Request.Context(), middleware.GetUserID(c), c.Param("memory_id")); err != nil {
		response.Forbidden(c, err.Error())
		return
	}
	response.OKWithMsg(c, "memory deleted", nil)
}
