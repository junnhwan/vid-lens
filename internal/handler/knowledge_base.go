package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"vid-lens/internal/middleware"
	"vid-lens/internal/pkg/response"
	"vid-lens/internal/service"
)

type KnowledgeBaseHandler struct {
	svc *service.KnowledgeBaseService
}

func NewKnowledgeBaseHandler(svc *service.KnowledgeBaseService) *KnowledgeBaseHandler {
	return &KnowledgeBaseHandler{svc: svc}
}

func (h *KnowledgeBaseHandler) Create(c *gin.Context) {
	var req service.CreateKnowledgeBaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	kb, err := h.svc.Create(c.Request.Context(), middleware.GetUserID(c), req)
	if err != nil {
		writeKnowledgeBaseError(c, err)
		return
	}
	response.OK(c, kb)
}

func (h *KnowledgeBaseHandler) List(c *gin.Context) {
	knowledgeBases, err := h.svc.List(c.Request.Context(), middleware.GetUserID(c))
	if err != nil {
		writeKnowledgeBaseError(c, err)
		return
	}
	response.OK(c, knowledgeBases)
}

func (h *KnowledgeBaseHandler) Get(c *gin.Context) {
	knowledgeBaseID, ok := parsePositiveKnowledgeBaseID(c, "id", "知识库 ID 错误")
	if !ok {
		return
	}
	kb, err := h.svc.Get(c.Request.Context(), middleware.GetUserID(c), knowledgeBaseID)
	if err != nil {
		writeKnowledgeBaseError(c, err)
		return
	}
	response.OK(c, kb)
}

func (h *KnowledgeBaseHandler) Update(c *gin.Context) {
	knowledgeBaseID, ok := parsePositiveKnowledgeBaseID(c, "id", "知识库 ID 错误")
	if !ok {
		return
	}
	var req service.UpdateKnowledgeBaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	kb, err := h.svc.Update(c.Request.Context(), middleware.GetUserID(c), knowledgeBaseID, req)
	if err != nil {
		writeKnowledgeBaseError(c, err)
		return
	}
	response.OK(c, kb)
}

func (h *KnowledgeBaseHandler) Delete(c *gin.Context) {
	knowledgeBaseID, ok := parsePositiveKnowledgeBaseID(c, "id", "知识库 ID 错误")
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), middleware.GetUserID(c), knowledgeBaseID); err != nil {
		writeKnowledgeBaseError(c, err)
		return
	}
	response.OKWithMsg(c, "删除成功", nil)
}

func (h *KnowledgeBaseHandler) AddVideo(c *gin.Context) {
	knowledgeBaseID, ok := parsePositiveKnowledgeBaseID(c, "id", "知识库 ID 错误")
	if !ok {
		return
	}
	var req struct {
		TaskID int64 `json:"task_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}
	if req.TaskID <= 0 {
		response.BadRequest(c, "任务 ID 错误")
		return
	}
	if err := h.svc.AddVideo(c.Request.Context(), middleware.GetUserID(c), knowledgeBaseID, req.TaskID); err != nil {
		writeKnowledgeBaseError(c, err)
		return
	}
	response.OKWithMsg(c, "添加成功", nil)
}

func (h *KnowledgeBaseHandler) RemoveVideo(c *gin.Context) {
	knowledgeBaseID, ok := parsePositiveKnowledgeBaseID(c, "id", "知识库 ID 错误")
	if !ok {
		return
	}
	taskID, ok := parsePositiveKnowledgeBaseID(c, "task_id", "任务 ID 错误")
	if !ok {
		return
	}
	if err := h.svc.RemoveVideo(c.Request.Context(), middleware.GetUserID(c), knowledgeBaseID, taskID); err != nil {
		writeKnowledgeBaseError(c, err)
		return
	}
	response.OKWithMsg(c, "移除成功", nil)
}

func parsePositiveKnowledgeBaseID(c *gin.Context, param, message string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(param), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, message)
		return 0, false
	}
	return id, true
}

func writeKnowledgeBaseError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrKnowledgeBaseNotFound),
		errors.Is(err, service.ErrKnowledgeBaseTaskNotFound),
		errors.Is(err, service.ErrKnowledgeBaseVideoNotFound):
		response.Fail(c, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrKnowledgeBaseNameRequired),
		errors.Is(err, service.ErrKnowledgeBaseNameTooLong),
		errors.Is(err, service.ErrKnowledgeBaseDescriptionTooLong),
		errors.Is(err, service.ErrKnowledgeBaseDefaultProfileRequired),
		errors.Is(err, service.ErrKnowledgeBaseTaskNotIndexed),
		errors.Is(err, service.ErrKnowledgeBaseVideoLimit):
		response.BadRequest(c, err.Error())
	default:
		response.InternalError(c, "知识库操作失败")
	}
}
