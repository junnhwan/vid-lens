package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"vid-lens/internal/middleware"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/response"
	"vid-lens/internal/service"
)

type MemoryHandler struct {
	service       *service.MemoryGovernanceService
	policyService *service.MemoryPolicyService
}

func NewMemoryHandler(memory *service.MemoryGovernanceService, policy ...*service.MemoryPolicyService) *MemoryHandler {
	handler := &MemoryHandler{service: memory}
	if len(policy) > 0 {
		handler.policyService = policy[0]
	}
	return handler
}

func (h *MemoryHandler) GetPreference(c *gin.Context) {
	if h == nil || h.policyService == nil {
		response.InternalError(c, "memory policy service unavailable")
		return
	}
	view, err := h.policyService.GetPreference(c.Request.Context(), middleware.GetUserID(c))
	if err != nil {
		response.InternalError(c, "查询长期记忆偏好失败")
		return
	}
	response.OK(c, view)
}

func (h *MemoryHandler) UpdatePreference(c *gin.Context) {
	if h == nil || h.policyService == nil {
		response.InternalError(c, "memory policy service unavailable")
		return
	}
	var req struct {
		Enabled         *bool  `json:"enabled" binding:"required"`
		ExpectedVersion *int64 `json:"expected_version" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil || req.ExpectedVersion == nil || *req.ExpectedVersion < 0 {
		response.BadRequest(c, "参数错误: enabled 和非负 expected_version 必填")
		return
	}
	view, err := h.policyService.UpdatePreference(c.Request.Context(), middleware.GetUserID(c), *req.Enabled, *req.ExpectedVersion)
	if errors.Is(err, service.ErrMemoryPolicyVersionConflict) {
		response.Fail(c, http.StatusConflict, "长期记忆偏好已被其他请求修改，请重新读取")
		return
	}
	if err != nil {
		response.InternalError(c, "修改长期记忆偏好失败")
		return
	}
	response.OK(c, view)
}

func (h *MemoryHandler) GetSessionPolicy(c *gin.Context) {
	if h == nil || h.policyService == nil {
		response.InternalError(c, "memory policy service unavailable")
		return
	}
	sessionID, err := strconv.ParseInt(c.Param("session_id"), 10, 64)
	if err != nil || sessionID <= 0 {
		response.BadRequest(c, "会话 ID 错误")
		return
	}
	view, err := h.policyService.GetSessionPolicy(c.Request.Context(), middleware.GetUserID(c), sessionID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Forbidden(c, "会话不存在或无权限")
		return
	}
	if err != nil {
		response.InternalError(c, "查询会话长期记忆策略失败")
		return
	}
	response.OK(c, view)
}

func (h *MemoryHandler) UpdateSessionPolicy(c *gin.Context) {
	if h == nil || h.policyService == nil {
		response.InternalError(c, "memory policy service unavailable")
		return
	}
	sessionID, err := strconv.ParseInt(c.Param("session_id"), 10, 64)
	if err != nil || sessionID <= 0 {
		response.BadRequest(c, "会话 ID 错误")
		return
	}
	var req struct {
		Policy          string `json:"policy" binding:"required"`
		ExpectedVersion *int64 `json:"expected_version" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.ExpectedVersion == nil || *req.ExpectedVersion < 0 {
		response.BadRequest(c, "参数错误: policy 和非负 expected_version 必填")
		return
	}
	view, err := h.policyService.UpdateSessionPolicy(c.Request.Context(), middleware.GetUserID(c), sessionID, req.Policy, *req.ExpectedVersion)
	if errors.Is(err, service.ErrMemoryPolicyVersionConflict) {
		response.Fail(c, http.StatusConflict, "会话长期记忆策略已被其他请求修改，请重新读取")
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Forbidden(c, "会话不存在或无权限")
		return
	}
	if err != nil {
		if !service.ValidSessionMemoryPolicy(req.Policy) {
			response.BadRequest(c, "memory policy 必须为 inherit、enabled 或 disabled")
			return
		}
		response.InternalError(c, "修改会话长期记忆策略失败")
		return
	}
	response.OK(c, view)
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
