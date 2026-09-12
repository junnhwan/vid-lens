package handler

import (
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"io"
	"net/http"
	"strconv"
	"vid-lens/internal/middleware"
	"vid-lens/internal/pkg/response"
	"vid-lens/internal/repository"
)

type ChatFeedbackHandler struct {
	repo *repository.ChatFeedbackRepository
}

func NewChatFeedbackHandler(repo *repository.ChatFeedbackRepository) *ChatFeedbackHandler {
	return &ChatFeedbackHandler{repo: repo}
}
func feedbackIDs(c *gin.Context) (int64, int64, bool) {
	s, e := strconv.ParseInt(c.Param("session_id"), 10, 64)
	m, e2 := strconv.ParseInt(c.Param("message_id"), 10, 64)
	if e != nil || e2 != nil || s <= 0 || m <= 0 {
		response.BadRequest(c, "无效回答标识")
		return 0, 0, false
	}
	return s, m, true
}
func feedbackError(c *gin.Context, e error) {
	if errors.Is(e, gorm.ErrRecordNotFound) {
		response.Fail(c, 404, "回答不存在")
	} else if errors.Is(e, repository.ErrInvalidFeedback) {
		response.BadRequest(c, "反馈类别或备注无效")
	} else {
		response.InternalError(c, "反馈操作失败")
	}
}
func (h *ChatFeedbackHandler) Put(c *gin.Context) {
	s, m, ok := feedbackIDs(c)
	if !ok {
		return
	}
	var req struct {
		Rating   string `json:"rating"`
		Category string `json:"category"`
		Note     string `json:"note"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 16384))
	decoder.DisallowUnknownFields()
	if e := decoder.Decode(&req); e != nil {
		response.BadRequest(c, "无效反馈字段")
		return
	}
	if e := decoder.Decode(new(any)); e != io.EOF {
		response.BadRequest(c, "无效反馈内容")
		return
	}
	f, e := h.repo.Put(c.Request.Context(), middleware.GetUserID(c), s, m, req.Rating, req.Category, req.Note)
	if e != nil {
		feedbackError(c, e)
		return
	}
	response.OK(c, f)
}
func (h *ChatFeedbackHandler) Get(c *gin.Context) {
	s, m, ok := feedbackIDs(c)
	if !ok {
		return
	}
	f, e := h.repo.Get(c.Request.Context(), middleware.GetUserID(c), s, m)
	if e != nil {
		feedbackError(c, e)
		return
	}
	response.OK(c, f)
}
func (h *ChatFeedbackHandler) Delete(c *gin.Context) {
	s, m, ok := feedbackIDs(c)
	if !ok {
		return
	}
	if e := h.repo.Delete(c.Request.Context(), middleware.GetUserID(c), s, m); e != nil {
		feedbackError(c, e)
		return
	}
	response.OK(c, gin.H{"cleared": true})
}
func (h *ChatFeedbackHandler) Candidates(c *gin.Context) {
	page, e := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, e2 := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	if e != nil || e2 != nil || page < 1 || size < 1 || size > 100 {
		response.BadRequest(c, "无效分页")
		return
	}
	list, total, e := h.repo.Candidates(c.Request.Context(), middleware.GetUserID(c), page, size)
	if e != nil {
		feedbackError(c, e)
		return
	}
	response.OK(c, response.PageResult{List: list, Total: total, Page: page, PageSize: size})
}
