package handler

import (
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"vid-lens/internal/ai"

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
		response.BadRequest(c, "拉取模型列表失败: "+safeProbeError(err))
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
		response.BadRequest(c, "检测维度失败: "+safeProbeError(err))
		return
	}
	response.OK(c, gin.H{"dimension": dim})
}

func (h *AIProfileHandler) ProbeCapability(c *gin.Context) {
	if isDemoUser(c) {
		response.Forbidden(c, "演示账号不可探测模型")
		return
	}
	var req service.ProbeCapabilityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "探测参数格式错误")
		return
	}
	dim, err := h.svc.ProbeCapability(c.Request.Context(), middleware.GetUserID(c), req)
	if err != nil {
		if errors.Is(err, service.ErrAIProfileNotFound) {
			response.Fail(c, 404, "配置不存在")
			return
		}
		response.BadRequest(c, safeProbeError(err))
		return
	}
	response.OK(c, gin.H{"dimension": dim})
}

// Provider bodies and transport errors may contain credentials or request URLs.
func safeProbeError(err error) string {
	if strings.HasPrefix(err.Error(), "向量维度不匹配：") {
		return err.Error()
	}
	switch {
	case strings.Contains(err.Error(), "API Key 不能为空"):
		return "请填写 API Key，或在已保存配置中沿用原密钥"
	case strings.Contains(err.Error(), "不允许访问"):
		return "探测不允许访问本地、内网或保留地址"
	case strings.Contains(err.Error(), "Base URL") || strings.Contains(err.Error(), "Endpoint 不能为空"):
		return "请检查 API 地址格式及路径"
	case strings.Contains(err.Error(), "无法解析模型列表响应"):
		return "服务商返回的模型列表格式与 OpenAI 兼容格式不符"
	case strings.Contains(err.Error(), "上游未返回可用模型"):
		return "服务商未返回可用模型"
	}
	var provider *ai.ProviderError
	if errors.As(err, &provider) {
		switch provider.Class {
		case ai.ErrorAuth:
			return "认证失败，请检查 API Key 与模型权限"
		case ai.ErrorRateLimited:
			return "服务商限流，请稍后重试"
		case ai.ErrorTimeout:
			return "模型请求超时，请检查服务状态"
		case ai.ErrorNetwork:
			return "无法连接模型服务，请检查地址与网络"
		case ai.ErrorProvider5xx:
			return "模型服务暂时不可用，请稍后重试"
		default:
			return "模型拒绝了探测请求，请检查地址、模型名及服务商要求"
		}
	}
	if errors.Is(err, ai.ErrAdmissionRejected) {
		return "当前额度或并发限制不允许探测，请稍后重试"
	}
	return "探测失败，请检查该模型的地址、名称及输入要求"
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

func (h *AIProfileHandler) BudgetOptions(c *gin.Context) {
	response.OK(c, gin.H{"version": 1, "defaults": h.svc.AgentBudgetOptions().Defaults, "limits": h.svc.AgentBudgetOptions().Limits, "visual_available": true})
}

func (h *AIProfileHandler) PromptPreferences(c *gin.Context) {
	views, err := h.svc.PromptPreferences(middleware.GetUserID(c))
	if err != nil {
		response.InternalError(c, "读取提示词配置失败")
		return
	}
	response.OK(c, views)
}

func (h *AIProfileHandler) SetPromptPreference(c *gin.Context) {
	if isDemoUser(c) {
		response.Forbidden(c, "演示账号不可修改提示词偏好")
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "提示词格式错误")
		return
	}
	if err := h.svc.SetPromptPreference(middleware.GetUserID(c), c.Param("function"), req.Text); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.OK(c, gin.H{"saved": true})
}
