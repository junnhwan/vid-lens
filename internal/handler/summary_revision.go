package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"vid-lens/internal/artifact"
	"vid-lens/internal/middleware"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
	"vid-lens/internal/service"
)

type SummaryRevisionHandler struct {
	svc   *service.SummaryRevisionService
	repos *repository.Repositories
}

func NewSummaryRevisionHandler(svc *service.SummaryRevisionService, repos *repository.Repositories) *SummaryRevisionHandler {
	return &SummaryRevisionHandler{svc: svc, repos: repos}
}

func summaryTaskID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		artifactError(c, artifact.Err("invalid_request", 400))
		return 0, false
	}
	return id, true
}

func (h *SummaryRevisionHandler) Get(c *gin.Context) {
	id, ok := summaryTaskID(c)
	if !ok {
		return
	}
	row, err := h.svc.Effective(c.Request.Context(), middleware.GetUserID(c), id)
	if err != nil {
		artifactError(c, err)
		return
	}
	var revisionID string
	if row.Revision != nil {
		revisionID = row.Revision.ID
	}
	versionRef := model.SummaryVersionRef{RevisionID: revisionID}
	if revisionID == "" {
		generatedVersion := int64(0)
		if row.Generated != nil {
			generatedVersion = row.Generated.GeneratedVersion
		}
		versionRef.GeneratedVersion = &generatedVersion
	}
	format := "legacy"
	if row.Document != nil {
		format = "summary-v2"
	}
	artifactOK(c, http.StatusOK, gin.H{"task_id": id, "version_ref": versionRef, "content": row.Content, "document": row.Document, "format": format, "content_digest": row.ContentDigest, "content_hash_kind": row.ContentHashKind, "base_generated_hash_kind": row.BaseHashKind, "current_generated_hash_kind": row.CurrentGeneratedHashKind, "revision": row.Version, "revision_id": revisionID, "base_generated_hash": row.BaseHash, "current_generated_hash": row.CurrentGeneratedHash, "source_status": row.SourceStatus, "has_generated": row.Generated != nil, "has_revision": row.Revision != nil}, nil)
}

func (h *SummaryRevisionHandler) Export(c *gin.Context) {
	id, ok := summaryTaskID(c)
	if !ok {
		return
	}
	row, err := h.svc.Effective(c.Request.Context(), middleware.GetUserID(c), id)
	if err != nil {
		artifactError(c, err)
		return
	}
	if strings.TrimSpace(row.Content) == "" {
		artifactError(c, artifact.Err("source_not_ready", 422))
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=video-%d-summary-v%d.md", id, row.Version))
	c.Data(http.StatusOK, "text/markdown; charset=utf-8", []byte(row.Content))
}

func (h *SummaryRevisionHandler) Edit(c *gin.Context) {
	id, ok := summaryTaskID(c)
	if !ok {
		return
	}
	var input service.SummaryEditInput
	if !artifactBody(c, &input) {
		return
	}
	view, err := h.svc.Submit(c.Request.Context(), middleware.GetUserID(c), id, c.GetHeader("Idempotency-Key"), input)
	artifactOK(c, http.StatusAccepted, view, err)
}

func (h *SummaryRevisionHandler) Operation(c *gin.Context) {
	id, ok := summaryTaskID(c)
	if !ok {
		return
	}
	view, err := h.svc.Operation(c.Request.Context(), middleware.GetUserID(c), id, c.Param("operation_id"))
	artifactOK(c, http.StatusOK, view, err)
}

func (h *SummaryRevisionHandler) LatestOperation(c *gin.Context) {
	id, ok := summaryTaskID(c)
	if !ok {
		return
	}
	view, err := h.svc.LatestOperation(c.Request.Context(), middleware.GetUserID(c), id)
	artifactOK(c, http.StatusOK, view, err)
}

func (h *SummaryRevisionHandler) Apply(c *gin.Context) {
	id, ok := summaryTaskID(c)
	if !ok {
		return
	}
	var input struct {
		ExpectedRevision int64 `json:"expected_revision"`
	}
	if !artifactBody(c, &input) {
		return
	}
	view, err := h.svc.Apply(c.Request.Context(), middleware.GetUserID(c), id, c.Param("operation_id"), input.ExpectedRevision)
	artifactOK(c, http.StatusOK, view, err)
}

func (h *SummaryRevisionHandler) Undo(c *gin.Context) {
	id, ok := summaryTaskID(c)
	if !ok {
		return
	}
	var input struct {
		ExpectedRevision int64 `json:"expected_revision"`
	}
	if !artifactBody(c, &input) {
		return
	}
	view, err := h.svc.Undo(c.Request.Context(), middleware.GetUserID(c), id, c.Param("operation_id"), input.ExpectedRevision)
	artifactOK(c, http.StatusOK, view, err)
}

func (h *SummaryRevisionHandler) ResolveBase(c *gin.Context) {
	id, ok := summaryTaskID(c)
	if !ok {
		return
	}
	var input struct {
		ExpectedRevision int64  `json:"expected_revision"`
		Choice           string `json:"choice"`
	}
	if !artifactBody(c, &input) {
		return
	}
	row, err := h.svc.ResolveBase(c.Request.Context(), middleware.GetUserID(c), id, input.ExpectedRevision, input.Choice)
	if err != nil {
		artifactError(c, err)
		return
	}
	artifactOK(c, http.StatusOK, gin.H{"task_id": id, "content": row.Content, "document": row.Document, "content_digest": row.ContentDigest, "content_hash_kind": row.ContentHashKind, "base_generated_hash_kind": row.BaseHashKind, "current_generated_hash_kind": row.CurrentGeneratedHashKind, "revision": row.Version, "source_status": row.SourceStatus}, nil)
}

func (h *SummaryRevisionHandler) Rules(c *gin.Context) {
	id, ok := summaryTaskID(c)
	if !ok {
		return
	}
	set, err := service.EffectiveTermRules(c.Request.Context(), h.repos, middleware.GetUserID(c), id)
	artifactOK(c, http.StatusOK, set, err)
}

func (h *SummaryRevisionHandler) SaveRule(c *gin.Context) {
	id, ok := summaryTaskID(c)
	if !ok {
		return
	}
	var input service.VideoTermRuleInput
	if !artifactBody(c, &input) {
		return
	}
	set, err := service.SaveVideoTermRule(c.Request.Context(), h.repos, middleware.GetUserID(c), id, input)
	artifactOK(c, http.StatusOK, set, err)
}

func (h *SummaryRevisionHandler) DisableRule(c *gin.Context) {
	id, ok := summaryTaskID(c)
	if !ok {
		return
	}
	var input struct {
		ExpectedVersion int64 `json:"expected_version"`
	}
	if !artifactBody(c, &input) {
		return
	}
	set, err := service.DisableVideoTermRule(c.Request.Context(), h.repos, middleware.GetUserID(c), id, c.Param("rule_id"), input.ExpectedVersion)
	artifactOK(c, http.StatusOK, set, err)
}
