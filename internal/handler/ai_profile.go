package handler

import (
	"encoding/json"
	"errors"
	"io"
	"strconv"

	"github.com/gin-gonic/gin"
	"vid-lens/internal/middleware"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/response"
	"vid-lens/internal/service"
)

type AIProfileHandler struct {
	svc *service.AIProfileService
}

func NewAIProfileHandler(svc *service.AIProfileService) *AIProfileHandler {
	return &AIProfileHandler{svc: svc}
}

// isDemoUser 演示账号：陌生人可登录，AI 配置只读且脱敏，禁止修改/测试。
func isDemoUser(c *gin.Context) bool {
	return middleware.GetRole(c) == model.RoleDemo
}

// denyIfDemo 演示账号只读保护：命中即写 403 并返回 true，调用方应直接 return。
// 演示账号只保留「视频列表 + 问答」；上传、异步触发、删除等变更一律拒绝。
func denyIfDemo(c *gin.Context, action string) bool {
	if !isDemoUser(c) {
		return false
	}
	response.Forbidden(c, "演示账号仅可观看与问答，不可"+action)
	return true
}

func (h *AIProfileHandler) List(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if isDemoUser(c) {
		profiles, err := h.svc.ListMasked(userID)
		if err != nil {
			response.InternalError(c, "查询 AI 配置失败")
			return
		}
		response.OK(c, profiles)
		return
	}
	profiles, err := h.svc.List(userID)
	if err != nil {
		response.InternalError(c, "查询 AI 配置失败")
		return
	}
	response.OK(c, profiles)
}

func (h *AIProfileHandler) Create(c *gin.Context) {
	if isDemoUser(c) {
		response.Forbidden(c, "演示账号不可修改 AI 配置")
		return
	}
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
	if isDemoUser(c) {
		response.Forbidden(c, "演示账号不可修改 AI 配置")
		return
	}
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
	if isDemoUser(c) {
		response.Forbidden(c, "演示账号不可修改 AI 配置")
		return
	}
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
	if isDemoUser(c) {
		response.Forbidden(c, "演示账号不可测试 AI 配置")
		return
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		response.BadRequest(c, "参数错误")
		return
	}
	var idReq struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(body, &idReq); err == nil && idReq.ID > 0 {
		userID := middleware.GetUserID(c)
		if err := h.svc.TestSavedProfile(c.Request.Context(), userID, idReq.ID); err != nil {
			if errors.Is(err, service.ErrAIProfileNotFound) {
				response.Fail(c, 404, err.Error())
				return
			}
			response.BadRequest(c, "模型配置测试失败: "+err.Error())
			return
		}
		response.OK(c, gin.H{"ok": true})
		return
	}

	var req service.AIProfileRequest
	if err := json.Unmarshal(body, &req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	if err := validateAIProfileRequestBinding(req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	if err := h.svc.Test(c.Request.Context(), req); err != nil {
		response.BadRequest(c, "模型配置测试失败: "+err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func (h *AIProfileHandler) ListModels(c *gin.Context) {
	if isDemoUser(c) {
		response.Forbidden(c, "演示账号不可拉取模型列表")
		return
	}
	userID := middleware.GetUserID(c)
	var req service.ListModelsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	models, err := h.svc.ListModels(c.Request.Context(), userID, req)
	if err != nil {
		if errors.Is(err, service.ErrAIProfileNotFound) {
			response.Fail(c, 404, err.Error())
			return
		}
		response.BadRequest(c, "拉取模型列表失败: "+err.Error())
		return
	}
	response.OK(c, gin.H{"models": models})
}

func (h *AIProfileHandler) ProbeEmbeddingDim(c *gin.Context) {
	if isDemoUser(c) {
		response.Forbidden(c, "演示账号不可检测模型维度")
		return
	}
	userID := middleware.GetUserID(c)
	var req service.ProbeEmbeddingDimRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	dim, err := h.svc.ProbeEmbeddingDim(c.Request.Context(), userID, req)
	if err != nil {
		if errors.Is(err, service.ErrAIProfileNotFound) {
			response.Fail(c, 404, err.Error())
			return
		}
		response.BadRequest(c, "检测维度失败: "+err.Error())
		return
	}
	response.OK(c, gin.H{"dimension": dim})
}

func validateAIProfileRequestBinding(req service.AIProfileRequest) error {
	if req.Name == "" {
		return errors.New("配置名称不能为空")
	}
	if req.LLMProvider == "" || req.LLMBaseURL == "" || req.LLMModel == "" {
		return errors.New("LLM 配置不完整")
	}
	if req.ASRProvider == "" || req.ASRBaseURL == "" || req.ASRModel == "" {
		return errors.New("ASR 配置不完整")
	}
	if req.EmbeddingProvider == "" || req.EmbeddingEndpoint == "" || req.EmbeddingModel == "" || req.EmbeddingDim <= 0 {
		return errors.New("embedding 配置不完整")
	}
	return nil
}
