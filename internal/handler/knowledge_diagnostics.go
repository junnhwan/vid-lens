package handler

import (
	"context"
	"github.com/gin-gonic/gin"
	"strconv"
	"vid-lens/internal/middleware"
	"vid-lens/internal/pkg/response"
	"vid-lens/internal/service"
)

func (h *ChatHandler) TestKnowledgeRetrieval(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	var req service.KnowledgeRetrievalRequest
	if err != nil || id <= 0 || c.ShouldBindJSON(&req) != nil {
		response.BadRequest(c, "检索参数无效")
		return
	}
	executor, ok := h.execution.(interface {
		TestKnowledgeRetrieval(context.Context, int64, int64, service.KnowledgeRetrievalRequest) (*service.KnowledgeRetrievalResult, error)
	})
	if !ok {
		response.InternalError(c, "检索测试不可用")
		return
	}
	result, err := executor.TestKnowledgeRetrieval(c.Request.Context(), middleware.GetUserID(c), id, req)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.OK(c, result)
}

func (h *ChatHandler) GetRunDetail(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("session_id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "会话 ID 无效")
		return
	}
	result, err := h.chatSvc.GetAgentRunDetail(c.Request.Context(), middleware.GetUserID(c), id, c.Param("run_id"))
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.OK(c, result)
}
