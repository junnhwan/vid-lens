package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"vid-lens/internal/middleware"
	"vid-lens/internal/pkg/response"
	"vid-lens/internal/service"
)

type AIProfileHandler struct {
	svc *service.AIProfileService
}

func NewAIProfileHandler(svc *service.AIProfileService) *AIProfileHandler {
	return &AIProfileHandler{svc: svc}
}

func (h *AIProfileHandler) List(c *gin.Context) {
	userID := middleware.GetUserID(c)
	profiles, err := h.svc.List(userID)
	if err != nil {
		response.InternalError(c, "查询 AI 配置失败")
		return
	}
	response.OK(c, profiles)
}

func (h *AIProfileHandler) Create(c *gin.Context) {
	userID := middleware.GetUserID(c)
	var req service.AIProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	profile, err := h.svc.Create(userID, req)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.OK(c, profile)
}

func (h *AIProfileHandler) Update(c *gin.Context) {
	userID := middleware.GetUserID(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "配置 ID 错误")
		return
	}

	var req service.AIProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	profile, err := h.svc.Update(userID, id, req)
	if err != nil {
		if err == service.ErrAIProfileNotFound {
			response.Fail(c, 404, err.Error())
			return
		}
		response.BadRequest(c, err.Error())
		return
	}
	response.OK(c, profile)
}

func (h *AIProfileHandler) Delete(c *gin.Context) {
	userID := middleware.GetUserID(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "配置 ID 错误")
		return
	}

	if err := h.svc.Delete(userID, id); err != nil {
		response.Fail(c, 404, err.Error())
		return
	}
	response.OKWithMsg(c, "删除成功", nil)
}

func (h *AIProfileHandler) Test(c *gin.Context) {
	var req service.AIProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	if err := h.svc.Test(c.Request.Context(), req); err != nil {
		response.BadRequest(c, "模型配置测试失败: "+err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}
