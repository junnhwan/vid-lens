package repository

import (
	"strings"
	"time"

	"vid-lens/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TaskRepository struct {
	db *gorm.DB
}

func NewTaskRepository(db *gorm.DB) *TaskRepository {
	return &TaskRepository{db: db}
}

// Create 创建任务记录
func (r *TaskRepository) Create(task *model.VideoTask) error {
	return r.db.Create(task).Error
}

// FindByID 根据 ID 查找任务
func (r *TaskRepository) FindByID(id int64) (*model.VideoTask, error) {
	var task model.VideoTask
	err := r.db.First(&task, id).Error
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// FindByIDForUpdate serializes deletion against worker status transitions.
// SQLite unit tests cannot prove row-lock behavior;
// TestPostgresForUpdateBlocksConcurrentTransaction verifies it on PostgreSQL.
func (r *TaskRepository) FindByIDForUpdate(id int64) (*model.VideoTask, error) {
	var task model.VideoTask
	err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, id).Error
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func (r *TaskRepository) ListByIDsForUser(userID int64, taskIDs []int64) ([]model.VideoTask, error) {
	if len(taskIDs) == 0 {
		return []model.VideoTask{}, nil
	}
	var tasks []model.VideoTask
	err := r.db.Where("user_id = ? AND id IN ?", userID, taskIDs).Order("id ASC").Find(&tasks).Error
	return tasks, err
}

// FindByIDWithDetail 查找任务并预加载关联的转录和总结
func (r *TaskRepository) FindByIDWithDetail(id int64) (*model.VideoTask, error) {
	var task model.VideoTask
	err := r.db.
		Preload("Asset").
		Preload("Transcription").
		Preload("Summary").
		Preload("Jobs").
		First(&task, id).Error
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// FindByMD5 根据 MD5 查找任务（内容级去重核心）
func (r *TaskRepository) FindByMD5(md5 string) (*model.VideoTask, error) {
	var task model.VideoTask
	err := r.db.Where("file_md5 = ?", md5).First(&task).Error
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// ListByUserID 分页查询用户的视频任务列表，keyword 非空时按文件名/标题模糊搜索
// 面试亮点：(user_id, created_at) 联合索引，天然按时间排序
func (r *TaskRepository) ListByUserID(userID int64, page, pageSize int, keyword string) ([]model.VideoTask, int64, error) {
	var tasks []model.VideoTask
	var total int64

	query := r.db.Where("user_id = ?", userID)
	if kw := strings.TrimSpace(keyword); kw != "" {
		like := "%" + kw + "%"
		query = query.Where("filename LIKE ? OR title LIKE ?", like, like)
	}
	query.Model(&model.VideoTask{}).Count(&total)

	offset := (page - 1) * pageSize
	err := query.
		Select("id, user_id, asset_id, file_md5, filename, title, file_url, file_size, status, stage, trace_id, source_type, retry_count, max_retries, next_retry_at, last_error_code, last_error_msg, last_job_type, stage_started_at, stage_finished_at, started_at, finished_at, error_msg, created_at, updated_at").
		Order("created_at DESC").
		Offset(offset).
		Limit(pageSize).
		Find(&tasks).Error

	return tasks, total, err
}

// UpdateStatus 更新任务状态
func (r *TaskRepository) UpdateStatus(id int64, status int8, errMsg string) error {
	updates := map[string]interface{}{
		"status":    status,
		"error_msg": errMsg,
	}
	if errMsg != "" {
		updates["last_error_msg"] = errMsg
	}
	return r.db.Model(&model.VideoTask{}).Where("id = ?", id).Updates(updates).Error
}

func (r *TaskRepository) UpdateStatusAndStage(id int64, status int8, stage, errMsg string) error {
	now := time.Now()
	updates := map[string]interface{}{
		"status":           status,
		"stage":            stage,
		"stage_started_at": &now,
		"error_msg":        errMsg,
	}
	if stage == model.TaskStageNone || status == model.TaskStatusCompleted || status == model.TaskStatusFailed || status == model.TaskStatusDead {
		updates["stage_finished_at"] = &now
	}
	if errMsg != "" {
		updates["last_error_msg"] = errMsg
	}
	return r.db.Model(&model.VideoTask{}).Where("id = ?", id).Updates(updates).Error
}

// UpdateStatusIf 只在当前状态属于 allowedFrom 时更新状态。
// 返回 false 表示状态已被其他请求改变，调用方应停止当前操作。
func (r *TaskRepository) UpdateStatusIf(id int64, allowedFrom []int8, status int8, errMsg string) (bool, error) {
	updates := map[string]interface{}{
		"status":    status,
		"error_msg": errMsg,
	}
	if errMsg != "" {
		updates["last_error_msg"] = errMsg
	}
	tx := r.db.Model(&model.VideoTask{}).
		Where("id = ? AND status IN ?", id, allowedFrom).
		Updates(updates)
	if tx.Error != nil {
		return false, tx.Error
	}
	return tx.RowsAffected > 0, nil
}

func (r *TaskRepository) UpdateStatusAndStageIf(id int64, allowedFrom []int8, status int8, stage, errMsg string) (bool, error) {
	now := time.Now()
	updates := map[string]interface{}{
		"status":           status,
		"stage":            stage,
		"stage_started_at": &now,
		"error_msg":        errMsg,
	}
	if stage == model.TaskStageNone || status == model.TaskStatusCompleted || status == model.TaskStatusFailed || status == model.TaskStatusDead {
		updates["stage_finished_at"] = &now
	}
	if errMsg != "" {
		updates["last_error_msg"] = errMsg
	}
	tx := r.db.Model(&model.VideoTask{}).
		Where("id = ? AND status IN ?", id, allowedFrom).
		Updates(updates)
	if tx.Error != nil {
		return false, tx.Error
	}
	return tx.RowsAffected > 0, nil
}

// UpdateTitle 写回 AI 生成的视频标题
func (r *TaskRepository) UpdateTitle(id int64, title string) error {
	return r.db.Model(&model.VideoTask{}).Where("id = ?", id).Update("title", title).Error
}

func (r *TaskRepository) RecordRetryableFailure(id int64, jobType, stage, errMsg string, retryCount, maxRetries int, nextRetryAt time.Time) error {
	now := time.Now()
	updates := map[string]interface{}{
		"status":            model.TaskStatusFailed,
		"stage":             stage,
		"error_msg":         errMsg,
		"last_error_code":   "retryable_error",
		"last_error_msg":    errMsg,
		"last_job_type":     jobType,
		"retry_count":       retryCount,
		"max_retries":       maxRetries,
		"next_retry_at":     nextRetryAt,
		"stage_finished_at": &now,
	}
	return r.db.Model(&model.VideoTask{}).Where("id = ?", id).Updates(updates).Error
}

func (r *TaskRepository) RecordTerminalFailure(id int64, jobType, stage, errCode, errMsg string, retryCount, maxRetries int, status int8) error {
	now := time.Now()
	updates := map[string]interface{}{
		"status":            status,
		"stage":             stage,
		"error_msg":         errMsg,
		"last_error_code":   errCode,
		"last_error_msg":    errMsg,
		"last_job_type":     jobType,
		"retry_count":       retryCount,
		"max_retries":       maxRetries,
		"next_retry_at":     nil,
		"stage_finished_at": &now,
		"finished_at":       now,
	}
	return r.db.Model(&model.VideoTask{}).Where("id = ?", id).Updates(updates).Error
}

func (r *TaskRepository) FindDueRetryTasks(now time.Time, limit int) ([]model.VideoTask, error) {
	if limit <= 0 {
		limit = 20
	}

	var tasks []model.VideoTask
	err := r.db.Model(&model.VideoTask{}).
		Select("video_tasks.*").
		Joins("LEFT JOIN task_jobs AS retry_job ON retry_job.task_id = video_tasks.id AND retry_job.job_type = video_tasks.last_job_type").
		Where("video_tasks.last_job_type <> ? AND (retry_job.id IS NULL OR retry_job.retry_count <= retry_job.max_retries)", "").
		Where("((video_tasks.status = ? AND video_tasks.next_retry_at IS NOT NULL AND video_tasks.next_retry_at <= ?) OR (retry_job.status = ? AND retry_job.next_retry_at IS NOT NULL AND retry_job.next_retry_at <= ?) OR (retry_job.status IN ? AND retry_job.processing_token <> ? AND retry_job.lease_expires_at IS NOT NULL AND retry_job.lease_expires_at <= ?) OR (retry_job.id IS NULL AND video_tasks.status IN ? AND video_tasks.processing_token <> ? AND video_tasks.lease_expires_at IS NOT NULL AND video_tasks.lease_expires_at <= ?))",
			model.TaskStatusFailed, now, model.TaskStatusFailed, now, []int8{model.TaskStatusQueued, model.TaskStatusRunning}, "", now, []int8{model.TaskStatusQueued, model.TaskStatusRunning}, "", now).
		Order("COALESCE(retry_job.next_retry_at, retry_job.lease_expires_at, video_tasks.next_retry_at) ASC").
		Limit(limit).
		Find(&tasks).Error
	return tasks, err
}

func (r *TaskRepository) CountActiveByAssetID(assetID int64) (int64, error) {
	if assetID <= 0 {
		return 0, nil
	}
	var count int64
	err := r.db.Model(&model.VideoTask{}).Where("asset_id = ?", assetID).Count(&count).Error
	return count, err
}

// Delete 删除任务（逻辑删除）
func (r *TaskRepository) Delete(id int64) error {
	return r.db.Delete(&model.VideoTask{}, id).Error
}
