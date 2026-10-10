package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"vid-lens/internal/artifact"
	"vid-lens/internal/middleware"
	"vid-lens/internal/repository"
	"vid-lens/internal/service"
)

type UserTagHandler struct{ svc *service.UserTagService }

func NewUserTagHandler(svc *service.UserTagService) *UserTagHandler { return &UserTagHandler{svc} }

func (h *UserTagHandler) List(c *gin.Context) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil {
		artifactError(c, artifact.Err("invalid_request", 400))
		return
	}
	size, err := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	if err != nil {
		artifactError(c, artifact.Err("invalid_request", 400))
		return
	}
	rows, total, err := h.svc.List(c.Request.Context(), middleware.GetUserID(c), page, size, c.Query("search"), c.DefaultQuery("sort", "name"))
	artifactOK(c, http.StatusOK, gin.H{"list": rows, "total": total, "page": page, "page_size": size}, err)
}
func (h *UserTagHandler) Create(c *gin.Context) {
	if denyIfDemo(c, "创建标签") {
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if !artifactBody(c, &input) {
		return
	}
	out, err := h.svc.Create(c.Request.Context(), middleware.GetUserID(c), input.Name)
	artifactOK(c, http.StatusOK, out, err)
}
func (h *UserTagHandler) Rename(c *gin.Context) {
	if denyIfDemo(c, "修改标签") {
		return
	}
	var input struct {
		Name            string `json:"name"`
		ExpectedVersion int64  `json:"expected_version"`
	}
	if !artifactBody(c, &input) {
		return
	}
	out, err := h.svc.Rename(c.Request.Context(), middleware.GetUserID(c), c.Param("tag_id"), input.Name, input.ExpectedVersion)
	artifactOK(c, http.StatusOK, out, err)
}
func (h *UserTagHandler) Aliases(c *gin.Context) {
	if denyIfDemo(c, "修改标签别名") {
		return
	}
	var input struct {
		Aliases         []string `json:"aliases"`
		ExpectedVersion int64    `json:"expected_version"`
	}
	if !artifactBody(c, &input) {
		return
	}
	out, err := h.svc.Aliases(c.Request.Context(), middleware.GetUserID(c), c.Param("tag_id"), input.Aliases, input.ExpectedVersion)
	artifactOK(c, http.StatusOK, out, err)
}
func (h *UserTagHandler) Merge(c *gin.Context) {
	if denyIfDemo(c, "合并标签") {
		return
	}
	var input repository.TagMergeInput
	if !artifactBody(c, &input) {
		return
	}
	out, err := h.svc.Merge(c.Request.Context(), middleware.GetUserID(c), c.Param("tag_id"), c.GetHeader("Idempotency-Key"), input)
	artifactOK(c, http.StatusOK, out, err)
}
func (h *UserTagHandler) Task(c *gin.Context) {
	id, ok := summaryTaskID(c)
	if !ok {
		return
	}
	out, err := h.svc.Task(c.Request.Context(), middleware.GetUserID(c), id)
	artifactOK(c, http.StatusOK, out, err)
}
func (h *UserTagHandler) Patch(c *gin.Context) {
	if denyIfDemo(c, "修改视频标签") {
		return
	}
	id, ok := summaryTaskID(c)
	if !ok {
		return
	}
	var input repository.TagPatch
	if !artifactBody(c, &input) {
		return
	}
	out, err := h.svc.Patch(c.Request.Context(), middleware.GetUserID(c), id, input)
	artifactOK(c, http.StatusOK, out, err)
}
func (h *UserTagHandler) Decide(c *gin.Context) {
	if denyIfDemo(c, "处理标签建议") {
		return
	}
	id, ok := summaryTaskID(c)
	if !ok {
		return
	}
	var input repository.TagSuggestionDecisionInput
	if !artifactBody(c, &input) {
		return
	}
	out, err := h.svc.Decide(c.Request.Context(), middleware.GetUserID(c), id, c.Param("suggestion_id"), input)
	artifactOK(c, http.StatusOK, out, err)
}

func parseTagFilter(c *gin.Context) ([]string, string, error) {
	if len(c.Request.URL.Query()["tag_ids"]) > 1 || len(c.Request.URL.Query()["tag_match"]) > 1 {
		return nil, "", artifact.Err("invalid_request", 400)
	}
	match := c.DefaultQuery("tag_match", "all")
	if match != "all" && match != "any" {
		return nil, "", artifact.Err("invalid_tag_match", 400)
	}
	raw, present := c.GetQuery("tag_ids")
	if !present {
		if _, exists := c.Request.URL.Query()["tag_ids"]; exists {
			return nil, "", artifact.Err("invalid_request", 400)
		}
		return nil, match, nil
	}
	var ids []string
	for _, id := range strings.Split(raw, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			return nil, "", artifact.Err("invalid_request", 400)
		}
		ids = append(ids, id)
	}
	return ids, match, nil
}
