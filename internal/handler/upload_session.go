package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"vid-lens/internal/middleware"
	"vid-lens/internal/pkg/response"
	"vid-lens/internal/service"

	"github.com/gin-gonic/gin"
)

// uploadSessionApplication is the transport-facing upload-session contract.
// Keeping this interface at the handler boundary makes HTTP behavior testable
// without coupling transport tests to PostgreSQL or MinIO.
type uploadSessionApplication interface {
	Create(context.Context, int64, service.CreateUploadSessionRequest) (*service.UploadSessionView, error)
	Get(context.Context, int64, string) (*service.UploadSessionView, error)
	AcceptChunk(context.Context, int64, string, int, io.Reader) (*service.UploadChunkResult, error)
	Complete(context.Context, int64, string) (*service.UploadResult, error)
}

// UploadSessionHandler translates the durable upload-session domain contract
// to HTTP. PostgreSQL/MinIO lifecycle rules remain owned by the service.
type UploadSessionHandler struct {
	svc uploadSessionApplication
}

func NewUploadSessionHandler(svc uploadSessionApplication) *UploadSessionHandler {
	return &UploadSessionHandler{svc: svc}
}

// Create creates or resumes the current user's upload session.
func (h *UploadSessionHandler) Create(c *gin.Context) {
	var req service.CreateUploadSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "上传清单参数无效")
		return
	}

	view, err := h.svc.Create(c.Request.Context(), middleware.GetUserID(c), req)
	if err != nil {
		writeUploadSessionError(c, err)
		return
	}
	response.OK(c, view)
}

// Get reconstructs authoritative upload progress from PostgreSQL.
func (h *UploadSessionHandler) Get(c *gin.Context) {
	view, err := h.svc.Get(c.Request.Context(), middleware.GetUserID(c), c.Param("session_id"))
	if err != nil {
		writeUploadSessionError(c, err)
		return
	}
	response.OK(c, view)
}

// UploadChunk accepts one raw application/octet-stream chunk. The handler uses
// the immutable manifest to bound the request body before the service performs
// authoritative size and content verification.
func (h *UploadSessionHandler) UploadChunk(c *gin.Context) {
	userID := middleware.GetUserID(c)
	sessionID := c.Param("session_id")
	view, err := h.svc.Get(c.Request.Context(), userID, sessionID)
	if err != nil {
		writeUploadSessionError(c, err)
		return
	}

	expectedSize, err := expectedUploadSessionChunkSize(view, c.Param("index"))
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if c.Request.ContentLength > expectedSize {
		response.BadRequest(c, fmt.Sprintf("分片大小超过清单限制: 最大 %d 字节", expectedSize))
		return
	}

	chunkIndex, _ := strconv.Atoi(c.Param("index")) // validated above
	body := http.MaxBytesReader(c.Writer, c.Request.Body, expectedSize+1)
	defer body.Close()
	result, err := h.svc.AcceptChunk(c.Request.Context(), userID, sessionID, chunkIndex, body)
	if err != nil {
		writeUploadSessionError(c, err)
		return
	}
	response.OK(c, result)
}

// Complete verifies and assembles the immutable manifest, then returns the
// stable task identity persisted on the upload session.
func (h *UploadSessionHandler) Complete(c *gin.Context) {
	result, err := h.svc.Complete(c.Request.Context(), middleware.GetUserID(c), c.Param("session_id"))
	if err != nil {
		writeUploadSessionError(c, err)
		return
	}
	response.OK(c, result)
}

func expectedUploadSessionChunkSize(view *service.UploadSessionView, rawIndex string) (int64, error) {
	index, err := strconv.Atoi(rawIndex)
	if err != nil || view == nil || index < 0 || index >= view.TotalChunks {
		return 0, errors.New("分片序号无效")
	}
	if view.FileSize <= 0 || view.ChunkSize <= 0 || view.TotalChunks <= 0 {
		return 0, errors.New("上传清单无效")
	}

	expectedSize := view.ChunkSize
	if index == view.TotalChunks-1 {
		expectedSize = view.FileSize - int64(view.TotalChunks-1)*view.ChunkSize
	}
	if expectedSize <= 0 || expectedSize > view.ChunkSize {
		return 0, errors.New("上传清单无效")
	}
	return expectedSize, nil
}

func uploadSessionHTTPStatus(err error) int {
	var domainErr *service.UploadSessionError
	if !errors.As(err, &domainErr) {
		return http.StatusInternalServerError
	}
	switch domainErr.Kind {
	case service.UploadSessionErrorInvalid:
		return http.StatusBadRequest
	case service.UploadSessionErrorNotFound:
		return http.StatusNotFound
	case service.UploadSessionErrorConflict,
		service.UploadSessionErrorInProgress,
		service.UploadSessionErrorFailed:
		return http.StatusConflict
	case service.UploadSessionErrorExpired:
		return http.StatusGone
	default:
		return http.StatusInternalServerError
	}
}

func writeUploadSessionError(c *gin.Context, err error) {
	status := uploadSessionHTTPStatus(err)
	message := "上传会话处理失败"
	var domainErr *service.UploadSessionError
	if status != http.StatusInternalServerError && errors.As(err, &domainErr) && domainErr.Message != "" {
		message = domainErr.Message
	}
	response.Fail(c, status, message)
}
