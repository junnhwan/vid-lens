package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"vid-lens/internal/middleware"
	"vid-lens/internal/pkg/response"
	"vid-lens/internal/service"
)

func (h *AIProfileHandler) HostedStatus(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	status, err := h.svc.GetHostedStatus(middleware.GetUserID(c))
	if err != nil {
		response.InternalError(c, "读取 Free API 状态失败")
		return
	}
	response.OK(c, status)
}

func (h *AIProfileHandler) ActivateHosted(c *gin.Context) {
	if denyIfDemo(c, "启用 Free API") {
		return
	}
	profile, err := h.svc.ActivateHosted(middleware.GetUserID(c))
	if err != nil {
		if errors.Is(err, service.ErrHostedAIUnavailable) {
			response.Fail(c, http.StatusServiceUnavailable, "Free API 暂不可用，请稍后重试或使用自己的 API 配置")
		} else {
			response.InternalError(c, "启用 Free API 失败")
		}
		return
	}
	response.OK(c, profile)
}

func (h *AIProfileHandler) HostedAdmin(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if !h.svc.CanManageHosted(middleware.GetUserID(c)) || isDemoUser(c) {
		response.Forbidden(c, "仅作者可管理 Free API")
		return
	}
	config, err := h.svc.GetHostedAdmin(middleware.GetUserID(c))
	if err != nil {
		response.InternalError(c, "读取 Free API 配置失败")
		return
	}
	response.OK(c, config)
}

func (h *AIProfileHandler) SaveHostedAdmin(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if !h.svc.CanManageHosted(middleware.GetUserID(c)) || isDemoUser(c) {
		response.Forbidden(c, "仅作者可管理 Free API")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024)
	var req service.HostedAIAdminRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		response.BadRequest(c, "Free API 配置格式错误")
		return
	}
	config, err := h.svc.SaveHostedAdmin(middleware.GetUserID(c), req)
	if err != nil {
		// Validation must never echo submitted credentials or provider responses.
		response.BadRequest(c, "保存失败，请检查五项模型、HTTPS 地址、密钥和向量维度是否完整且与服务器一致")
		return
	}
	response.OK(c, config)
}
