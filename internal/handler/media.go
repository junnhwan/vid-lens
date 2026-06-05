package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"vid-lens/internal/middleware"
	"vid-lens/internal/pkg/response"
	"vid-lens/internal/service"
)

type MediaHandler struct {
	svc *service.MediaService
}

func NewMediaHandler(svc *service.MediaService) *MediaHandler {
	return &MediaHandler{svc: svc}
}

// UploadFile 普通文件上传
// POST /api/v1/media/upload
func (h *MediaHandler) UploadFile(c *gin.Context) {
	userID := middleware.GetUserID(c)

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.BadRequest(c, "请选择要上传的文件")
		return
	}
	defer file.Close()

	result, err := h.svc.UploadFile(c.Request.Context(), userID, header.Filename, file, header.Size)
	if err != nil {
		response.InternalError(c, "上传失败: "+err.Error())
		return
	}

	response.OK(c, result)
}

// UploadChunk 分片上传
// POST /api/v1/media/upload-chunk
func (h *MediaHandler) UploadChunk(c *gin.Context) {
	fileMD5 := c.PostForm("file_md5")
	chunkNumber, _ := strconv.Atoi(c.PostForm("chunk_number"))
	if fileMD5 == "" || chunkNumber < 0 {
		response.BadRequest(c, "缺少 file_md5 或 chunk_number")
		return
	}

	chunkFile, _, err := c.Request.FormFile("chunk")
	if err != nil {
		response.BadRequest(c, "请选择分片文件")
		return
	}
	defer chunkFile.Close()

	// 读取分片数据
	chunkData := make([]byte, 0)
	buf := make([]byte, 4096)
	for {
		n, err := chunkFile.Read(buf)
		if n > 0 {
			chunkData = append(chunkData, buf[:n]...)
		}
		if err != nil {
			break
		}
	}

	if err := h.svc.UploadChunk(c.Request.Context(), fileMD5, chunkNumber, chunkData, int64(len(chunkData))); err != nil {
		response.InternalError(c, "分片上传失败: "+err.Error())
		return
	}

	response.OK(c, gin.H{"chunk_number": chunkNumber})
}

// CheckUpload 检查上传进度（断点续传核心）
// GET /api/v1/media/check-upload?file_md5=xxx
func (h *MediaHandler) CheckUpload(c *gin.Context) {
	fileMD5 := c.Query("file_md5")
	if fileMD5 == "" {
		response.BadRequest(c, "缺少 file_md5 参数")
		return
	}

	progress, err := h.svc.CheckUploadProgress(c.Request.Context(), fileMD5)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}

	response.OK(c, progress)
}

// MergeChunks 合并分片
// POST /api/v1/media/merge-chunks
func (h *MediaHandler) MergeChunks(c *gin.Context) {
	userID := middleware.GetUserID(c)

	var req struct {
		FileMD5     string `json:"file_md5" binding:"required"`
		Filename    string `json:"filename" binding:"required"`
		TotalChunks int    `json:"total_chunks" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	result, err := h.svc.MergeChunks(c.Request.Context(), userID, req.FileMD5, req.Filename, req.TotalChunks)
	if err != nil {
		response.InternalError(c, "合并失败: "+err.Error())
		return
	}

	response.OK(c, result)
}

// RequestAnalysis 提交 AI 分析请求
// POST /api/v1/media/analyze/:id
func (h *MediaHandler) RequestAnalysis(c *gin.Context) {
	userID := middleware.GetUserID(c)
	taskID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "无效的任务 ID")
		return
	}

	if err := h.svc.RequestAnalysis(c.Request.Context(), userID, taskID); err != nil {
		response.Fail(c, 400, err.Error())
		return
	}

	response.OKWithMsg(c, "任务已投递至消息队列", gin.H{"task_id": taskID})
}

// RequestTranscribe 提交文字提取请求
// POST /api/v1/media/transcribe/:id
func (h *MediaHandler) RequestTranscribe(c *gin.Context) {
	userID := middleware.GetUserID(c)
	taskID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "无效的任务 ID")
		return
	}

	if err := h.svc.RequestTranscribe(c.Request.Context(), userID, taskID); err != nil {
		response.Fail(c, 400, err.Error())
		return
	}

	response.OKWithMsg(c, "文字提取任务已提交", gin.H{"task_id": taskID})
}

// GetTaskDetail 查询任务详情（前端轮询用）
// GET /api/v1/media/task/:id
func (h *MediaHandler) GetTaskDetail(c *gin.Context) {
	userID := middleware.GetUserID(c)
	taskID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "无效的任务 ID")
		return
	}

	task, err := h.svc.GetTaskDetail(c.Request.Context(), userID, taskID)
	if err != nil {
		response.Fail(c, 404, err.Error())
		return
	}

	response.OK(c, task)
}

// ListTasks 分页查询任务列表
// GET /api/v1/media/list?page=1&page_size=20
func (h *MediaHandler) ListTasks(c *gin.Context) {
	userID := middleware.GetUserID(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	tasks, total, err := h.svc.ListTasks(userID, page, pageSize)
	if err != nil {
		response.InternalError(c, "查询失败")
		return
	}

	response.OK(c, gin.H{
		"list":      tasks,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// DeleteTask 删除任务
// DELETE /api/v1/media/task/:id
func (h *MediaHandler) DeleteTask(c *gin.Context) {
	userID := middleware.GetUserID(c)
	taskID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "无效的任务 ID")
		return
	}

	if err := h.svc.DeleteTask(c.Request.Context(), userID, taskID); err != nil {
		response.Fail(c, 400, err.Error())
		return
	}

	response.OKWithMsg(c, "删除成功", nil)
}

// DownloadAudio 下载音频（通过 Pre-signed URL）
// GET /api/v1/media/download-audio/:id
func (h *MediaHandler) DownloadAudio(c *gin.Context) {
	userID := middleware.GetUserID(c)
	taskID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "无效的任务 ID")
		return
	}

	// 验证权限
	task, err := h.svc.GetTaskDetail(c.Request.Context(), userID, taskID)
	if err != nil {
		response.Fail(c, 404, "任务不存在")
		return
	}

	// 获取 Pre-signed URL
	url, err := h.svc.GetPresignedURL(c.Request.Context(), taskID)
	if err != nil {
		response.InternalError(c, "获取下载链接失败")
		return
	}

	// 返回预签名 URL，前端直接跳转下载
	response.OK(c, gin.H{
		"download_url": url,
		"filename":     task.Filename,
	})
}
