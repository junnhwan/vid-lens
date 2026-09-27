package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"strconv"
	"time"
	"vid-lens/internal/artifact"
	"vid-lens/internal/middleware"
	"vid-lens/internal/pkg/response"
	"vid-lens/internal/service"
)

type ArtifactHandler struct{ svc *service.ArtifactService }

func NewArtifactHandler(svc *service.ArtifactService) *ArtifactHandler { return &ArtifactHandler{svc} }
func artifactError(c *gin.Context, err error) {
	status, code := 500, "internal_error"
	var domain *artifact.Error
	if errors.As(err, &domain) {
		status, code = domain.Status, domain.Code
	}
	c.JSON(status, response.Response{Code: status, Message: code, Data: gin.H{"error_code": code}})
}
func artifactBody(c *gin.Context, v any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 600*1024)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		artifactError(c, artifact.Err("invalid_request", 400))
		return false
	}
	if err = artifact.Decode(raw, v); err != nil {
		artifactError(c, err)
		return false
	}
	return true
}
func artifactOK(c *gin.Context, status int, data any, err error) {
	if err != nil {
		artifactError(c, err)
		return
	}
	c.JSON(status, response.Response{Code: status, Message: "success", Data: data})
}
func artifactPage(c *gin.Context) (int, int, bool) {
	page, size := 1, 20
	var err error
	if v := c.Query("page"); v != "" {
		page, err = strconv.Atoi(v)
		if err != nil {
			page = 0
		}
	}
	if v := c.Query("page_size"); v != "" {
		size, err = strconv.Atoi(v)
		if err != nil {
			size = 0
		}
	}
	if page < 1 || page > 100000 || size < 1 || size > 100 {
		artifactError(c, artifact.Err("invalid_request", 400))
		return 0, 0, false
	}
	return page, size, true
}
func (h *ArtifactHandler) Submit(c *gin.Context) {
	if denyIfDemo(c, "生成成果") {
		return
	}
	var req artifact.GenerationRequest
	if !artifactBody(c, &req) {
		return
	}
	v, e := h.svc.Submit(c.Request.Context(), middleware.GetUserID(c), c.GetHeader("Idempotency-Key"), req, nil)
	artifactOK(c, 202, v, e)
}
func (h *ArtifactHandler) Run(c *gin.Context) {
	v, e := h.svc.Run(c.Request.Context(), middleware.GetUserID(c), c.Param("id"))
	artifactOK(c, 200, v, e)
}
func (h *ArtifactHandler) Cancel(c *gin.Context) {
	if denyIfDemo(c, "取消生成") {
		return
	}
	v, e := h.svc.Cancel(c.Request.Context(), middleware.GetUserID(c), c.Param("id"))
	artifactOK(c, 200, v, e)
}
func (h *ArtifactHandler) Resume(c *gin.Context) {
	if denyIfDemo(c, "恢复生成") {
		return
	}
	v, e := h.svc.Resume(c.Request.Context(), middleware.GetUserID(c), c.Param("id"))
	artifactOK(c, 202, v, e)
}
func (h *ArtifactHandler) Retry(c *gin.Context) {
	if denyIfDemo(c, "重新生成") {
		return
	}
	v, e := h.svc.Retry(c.Request.Context(), middleware.GetUserID(c), c.Param("id"), c.GetHeader("Idempotency-Key"))
	artifactOK(c, 202, v, e)
}
func (h *ArtifactHandler) Get(c *gin.Context) {
	v, e := h.svc.Get(c.Request.Context(), middleware.GetUserID(c), c.Param("id"))
	artifactOK(c, 200, v, e)
}
func (h *ArtifactHandler) List(c *gin.Context) {
	page, size, ok := artifactPage(c)
	if !ok {
		return
	}
	var source int64
	if q := c.Query("source_id"); q != "" {
		var e error
		source, e = strconv.ParseInt(q, 10, 64)
		if e != nil || source <= 0 {
			artifactError(c, artifact.Err("invalid_request", 400))
			return
		}
	}
	rows, total, e := h.svc.List(c.Request.Context(), middleware.GetUserID(c), source, page, size)
	artifactOK(c, 200, response.PageResult{List: rows, Total: total, Page: page, PageSize: size}, e)
}
func (h *ArtifactHandler) Create(c *gin.Context) {
	if denyIfDemo(c, "保存成果") {
		return
	}
	var req struct {
		SourceIDs []int64       `json:"source_ids"`
		Body      artifact.Body `json:"body"`
	}
	if !artifactBody(c, &req) {
		return
	}
	v, e := h.svc.Create(c.Request.Context(), middleware.GetUserID(c), req.SourceIDs, req.Body)
	artifactOK(c, 200, v, e)
}
func (h *ArtifactHandler) Save(c *gin.Context) {
	if denyIfDemo(c, "编辑成果") {
		return
	}
	var req struct {
		ExpectedHeadVersion int64         `json:"expected_head_version"`
		Body                artifact.Body `json:"body"`
	}
	if !artifactBody(c, &req) {
		return
	}
	v, e := h.svc.Save(c.Request.Context(), middleware.GetUserID(c), c.Param("id"), req.ExpectedHeadVersion, &req.Body, "")
	artifactOK(c, 200, v, e)
}
func (h *ArtifactHandler) Adopt(c *gin.Context) {
	if denyIfDemo(c, "采用成果") {
		return
	}
	var req struct {
		ExpectedHeadVersion int64  `json:"expected_head_version"`
		VersionID           string `json:"version_id"`
	}
	if !artifactBody(c, &req) {
		return
	}
	if req.VersionID == "" {
		artifactError(c, artifact.Err("invalid_request", 400))
		return
	}
	v, e := h.svc.Save(c.Request.Context(), middleware.GetUserID(c), c.Param("id"), req.ExpectedHeadVersion, nil, req.VersionID)
	artifactOK(c, 200, v, e)
}
func (h *ArtifactHandler) Versions(c *gin.Context) {
	v, e := h.svc.Versions(c.Request.Context(), middleware.GetUserID(c), c.Param("id"))
	artifactOK(c, 200, gin.H{"list": v}, e)
}
func (h *ArtifactHandler) Version(c *gin.Context) {
	v, e := h.svc.Version(c.Request.Context(), middleware.GetUserID(c), c.Param("id"), c.Param("version_id"))
	artifactOK(c, 200, v, e)
}
func (h *ArtifactHandler) Source(c *gin.Context) {
	id, e := strconv.ParseInt(c.Param("id"), 10, 64)
	if e != nil || id <= 0 {
		artifactError(c, artifact.Err("invalid_request", 400))
		return
	}
	v, e := h.svc.Source(c.Request.Context(), middleware.GetUserID(c), id)
	artifactOK(c, 200, v, e)
}
func (h *ArtifactHandler) Evidence(c *gin.Context) {
	v, e := h.svc.Evidence(c.Request.Context(), middleware.GetUserID(c), c.Param("manifest_id"), c.Param("evidence_id"))
	artifactOK(c, 200, v, e)
}
func (h *ArtifactHandler) LearningPosition(c *gin.Context) {
	v, e := h.svc.LearningPosition(c.Request.Context(), middleware.GetUserID(c))
	artifactOK(c, 200, v, e)
}
func (h *ArtifactHandler) SaveLearningPosition(c *gin.Context) {
	var req struct {
		ExpectedRevision int64  `json:"expected_revision"`
		TaskID           int64  `json:"task_id"`
		ArtifactID       string `json:"artifact_id"`
		VersionID        string `json:"version_id"`
		BlockID          string `json:"block_id"`
		TimeMS           int64  `json:"time_ms"`
	}
	if !artifactBody(c, &req) {
		return
	}
	v, e := h.svc.SaveLearningPosition(c.Request.Context(), middleware.GetUserID(c), req.ExpectedRevision, req.TaskID, req.ArtifactID, req.VersionID, req.BlockID, req.TimeMS)
	artifactOK(c, 200, v, e)
}
func (h *ArtifactHandler) BlockContext(c *gin.Context) {
	v, e := h.svc.BlockContext(c.Request.Context(), middleware.GetUserID(c), c.Param("id"), c.Query("version_id"), c.Param("block_id"))
	artifactOK(c, 200, v, e)
}
func (h *ArtifactHandler) PreviewAnswer(c *gin.Context) {
	var req struct {
		MessageID           int64  `json:"message_id"`
		AfterBlockID        string `json:"after_block_id"`
		ExpectedHeadVersion int64  `json:"expected_head_version"`
	}
	if !artifactBody(c, &req) {
		return
	}
	v, e := h.svc.PreviewAnswer(c.Request.Context(), middleware.GetUserID(c), req.MessageID, c.Param("id"), req.AfterBlockID, req.ExpectedHeadVersion)
	artifactOK(c, 200, v, e)
}
func (h *ArtifactHandler) ImportAnswer(c *gin.Context) {
	if denyIfDemo(c, "收录回答") {
		return
	}
	var req struct {
		MessageID           int64  `json:"message_id"`
		AfterBlockID        string `json:"after_block_id"`
		ExpectedHeadVersion int64  `json:"expected_head_version"`
		Personal            bool   `json:"personal_without_sources"`
	}
	if !artifactBody(c, &req) {
		return
	}
	v, e := h.svc.ImportAnswer(c.Request.Context(), middleware.GetUserID(c), req.MessageID, c.Param("id"), req.AfterBlockID, req.ExpectedHeadVersion, c.GetHeader("Idempotency-Key"), req.Personal)
	artifactOK(c, 200, v, e)
}
func (h *ArtifactHandler) Tasks(c *gin.Context) {
	page, size, ok := artifactPage(c)
	if !ok {
		return
	}
	v, total, e := h.svc.Tasks(c.Request.Context(), middleware.GetUserID(c), page, size)
	artifactOK(c, 200, response.PageResult{List: v, Total: total, Page: page, PageSize: size}, e)
}
func (h *ArtifactHandler) Events(c *gin.Context) {
	cursor := c.GetHeader("Last-Event-ID")
	if q, ok := c.GetQuery("after_seq"); ok {
		cursor = q
	}
	var after int64
	var err error
	if cursor != "" {
		after, err = strconv.ParseInt(cursor, 10, 64)
	}
	if err != nil || after < 0 {
		artifactError(c, artifact.Err("invalid_cursor", 400))
		return
	}
	owner, id := middleware.GetUserID(c), c.Param("id")
	events, err := h.svc.Events(c.Request.Context(), owner, id, after)
	if err != nil {
		artifactError(c, err)
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	c.Status(200)
	c.Writer.Flush()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		for _, e := range events {
			data := json.RawMessage(e.DataJSON)
			raw, _ := json.Marshal(gin.H{"schema_version": 1, "run_id": e.RunID, "seq": e.Seq, "type": e.Type, "created_at": e.CreatedAt, "data": data})
			if _, err = fmt.Fprintf(c.Writer, "id: %d\nevent: %s\ndata: %s\n\n", e.Seq, e.Type, raw); err != nil {
				return
			}
			after = e.Seq
		}
		c.Writer.Flush()
		run, e := h.svc.Run(c.Request.Context(), owner, id)
		if e != nil {
			return
		}
		if run.Status != "pending" && run.Status != "running" && after >= run.LastSeq {
			return
		}
		select {
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
		}
		events, err = h.svc.Events(c.Request.Context(), owner, id, after)
		if err != nil {
			return
		}
		if len(events) == 0 {
			if _, err = fmt.Fprint(c.Writer, ": heartbeat\n\n"); err != nil {
				return
			}
		}
	}
}
