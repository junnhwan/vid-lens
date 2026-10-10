package repository

import (
	"context"
	"strings"
	"time"

	"vid-lens/internal/artifact"
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

// ProcessingPresenceByTaskIDs reports published index and visual work for the
// task cards. A completed upload-dedup task may have no visual attempt of its
// own while a later manual index build has already finished.
func (r *TaskRepository) ProcessingPresenceByTaskIDs(taskIDs []int64) (map[int64]bool, map[int64]string, error) {
	indexed := make(map[int64]bool, len(taskIDs))
	visual := make(map[int64]string, len(taskIDs))
	if len(taskIDs) == 0 {
		return indexed, visual, nil
	}
	var indexIDs []int64
	if err := r.db.Model(&model.VideoRAGIndex{}).Where("task_id IN ? AND status = ?", taskIDs, model.RAGIndexStatusIndexed).Distinct("task_id").Pluck("task_id", &indexIDs).Error; err != nil {
		return nil, nil, err
	}
	for _, id := range indexIDs {
		indexed[id] = true
	}
	var rows []model.VideoVisualProgress
	if err := r.db.Where("task_id IN ?", taskIDs).Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	for _, row := range rows {
		visual[row.TaskID] = row.Status
	}
	var frameIDs []int64
	if err := r.db.Model(&model.VideoVisualFrame{}).Where("task_id IN ? AND status = ?", taskIDs, model.VisualFrameStatusCompleted).Distinct("task_id").Pluck("task_id", &frameIDs).Error; err != nil {
		return nil, nil, err
	}
	for _, id := range frameIDs {
		if visual[id] == "" {
			visual[id] = model.VisualProgressCompleted
		}
	}
	return indexed, visual, nil
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

// SetVisualDisabled changes future processing only while the task is idle.
// The status predicate prevents a setting change from racing a visual branch.
func (r *TaskRepository) SetVisualDisabled(userID, taskID int64, disabled bool) (bool, error) {
	mode := model.VisualModeBoth
	if disabled {
		mode = model.VisualModeOff
	}
	return r.SetVisualMode(userID, taskID, mode)
}

func (r *TaskRepository) SetVisualMode(userID, taskID int64, mode string) (bool, error) {
	result := r.db.Model(&model.VideoTask{}).
		Where("id = ? AND user_id = ? AND status NOT IN ?", taskID, userID, []int8{model.TaskStatusQueued, model.TaskStatusRunning}).
		Updates(map[string]interface{}{"visual_mode": mode, "visual_disabled": mode == model.VisualModeOff, "updated_at": time.Now()})
	return result.RowsAffected > 0, result.Error
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

func (r *TaskRepository) ListIndexedTaskIDsForUser(userID int64, embeddingModel string) ([]int64, error) {
	var ids []int64
	err := r.db.Table("video_rag_indexes AS ri").
		Joins("JOIN video_tasks AS vt ON vt.id = ri.task_id AND vt.user_id = ri.user_id AND vt.deleted_at IS NULL").
		Where("ri.user_id = ? AND ri.embedding_model = ? AND ri.status = ?", userID, embeddingModel, model.RAGIndexStatusIndexed).
		Order("ri.task_id").Pluck("ri.task_id", &ids).Error
	return ids, err
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

// ResultPresenceByTaskIDs includes published results reused by identical
// uploads, without loading their bodies into the video list.
func (r *TaskRepository) ResultPresenceByTaskIDs(tasks []model.VideoTask) (hasTranscription, hasSummary map[int64]bool, err error) {
	hasTranscription, hasSummary = map[int64]bool{}, map[int64]bool{}
	if len(tasks) == 0 {
		return hasTranscription, hasSummary, nil
	}
	ids := make([]int64, 0, len(tasks))
	ownedIDs := map[int64]bool{}
	md5ToIDs := map[string][]int64{}
	md5s := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
		ownedIDs[task.ID] = true
		if LegacyResultReuseAllowed(&task) && task.FileMD5 != "" {
			if len(md5ToIDs[task.FileMD5]) == 0 {
				md5s = append(md5s, task.FileMD5)
			}
			md5ToIDs[task.FileMD5] = append(md5ToIDs[task.FileMD5], task.ID)
		}
	}
	type resultRow struct {
		TaskID  int64
		FileMD5 string
	}
	mark := func(rows []resultRow, target map[int64]bool) {
		for _, row := range rows {
			if ownedIDs[row.TaskID] {
				target[row.TaskID] = true
			}
			for _, id := range md5ToIDs[row.FileMD5] {
				target[id] = true
			}
		}
	}
	var txRows []resultRow
	// Return the fingerprint only for cache-eligible rows. Direct owner records
	// remain visible while private source results cannot mark other tasks ready.
	if err = r.db.Model(&model.VideoTranscription{}).
		Select("task_id, CASE WHEN "+legacyTranscriptionCacheSQL("")+" THEN file_md5 ELSE '' END AS file_md5").
		Where("task_id IN ? OR (file_md5 IN ? AND ("+legacyTranscriptionCacheSQL("")+"))", ids, md5s).
		Find(&txRows).Error; err != nil {
		return nil, nil, err
	}
	mark(txRows, hasTranscription)
	var sumRows []resultRow
	if err = r.db.Model(&model.AISummary{}).
		Select("task_id, CASE WHEN "+legacySummaryCacheSQL("")+" THEN file_md5 ELSE '' END AS file_md5").
		Where("task_id IN ? OR (file_md5 IN ? AND ("+legacySummaryCacheSQL("")+"))", ids, md5s).
		Find(&sumRows).Error; err != nil {
		return nil, nil, err
	}
	mark(sumRows, hasSummary)
	return hasTranscription, hasSummary, nil
}

// LegacyResultReuseAllowed distinguishes old file-cache tasks from imports with
// frozen processing intent, including tasks whose first source is not ready yet.
func LegacyResultReuseAllowed(task *model.VideoTask) bool {
	return task != nil && task.ActiveTextSourceID == "" && task.ProcessingIntentJSON == ""
}

func legacyTaskCacheSQL(prefix string) string {
	return "COALESCE(" + prefix + "active_text_source_id, '') = '' AND COALESCE(" + prefix + "processing_intent_json, '') = ''"
}

// ListByUserID 分页查询用户的视频任务列表，keyword 非空时按文件名/标题模糊搜索
// The (user_id, created_at) index supports stable chronological pagination.
func (r *TaskRepository) ListByUserID(userID int64, page, pageSize int, keyword string, activity ...string) ([]model.VideoTask, int64, error) {
	return r.ListByUserIDFiltered(userID, page, pageSize, keyword, TagFilter{}, activity...)
}

func (r *TaskRepository) ListByUserIDFiltered(userID int64, page, pageSize int, keyword string, tags TagFilter, activity ...string) ([]model.VideoTask, int64, error) {
	var tasks []model.VideoTask
	var total int64

	query := r.db.Where("user_id = ?", userID)
	if kw := strings.TrimSpace(keyword); kw != "" {
		like := "%" + strings.ToLower(kw) + "%"
		query = query.Where("LOWER(filename) LIKE ? OR LOWER(title) LIKE ?", like, like)
	}
	if len(activity) > 0 {
		switch activity[0] {
		case "ready":
			query = query.Where("status NOT IN ?", []int8{model.TaskStatusQueued, model.TaskStatusRunning}).Where("EXISTS (SELECT 1 FROM video_transcriptions AS tx WHERE (tx.task_id = video_tasks.id OR (video_tasks.file_md5 <> '' AND tx.file_md5 = video_tasks.file_md5 AND ("+legacyTaskCacheSQL("video_tasks.")+") AND ("+legacyTranscriptionCacheSQL("tx.")+"))) AND TRIM(tx.content) <> '') OR EXISTS (SELECT 1 FROM video_visual_frames AS vf WHERE vf.task_id = video_tasks.id AND vf.status = ? AND (TRIM(vf.ocr_text) <> '' OR TRIM(vf.vision_caption) <> ''))", model.VisualFrameStatusCompleted)
		case "processing":
			query = query.Where("status IN ? OR (status NOT IN ? AND EXISTS (SELECT 1 FROM task_jobs AS sj WHERE sj.task_id = video_tasks.id AND sj.job_type = ? AND sj.status IN ?))",
				[]int8{model.TaskStatusQueued, model.TaskStatusRunning},
				[]int8{model.TaskStatusQueued, model.TaskStatusRunning, model.TaskStatusFailed, model.TaskStatusDead},
				model.TaskJobTypeSummary, []int8{model.TaskStatusQueued, model.TaskStatusRunning})
		case "pending":
			query = query.Where("status = ?", model.TaskStatusPending)
		case "failed":
			query = query.Where("status IN ? OR (status NOT IN ? AND EXISTS (SELECT 1 FROM task_jobs AS sj WHERE sj.task_id = video_tasks.id AND sj.job_type = ? AND sj.status IN ?))",
				[]int8{model.TaskStatusFailed, model.TaskStatusDead},
				[]int8{model.TaskStatusQueued, model.TaskStatusRunning, model.TaskStatusFailed, model.TaskStatusDead},
				model.TaskJobTypeSummary, []int8{model.TaskStatusFailed, model.TaskStatusDead})
		}
	}
	var err error
	query, err = applyTagFilter(query, r.db, userID, tags)
	if err != nil {
		return nil, 0, err
	}
	if err := query.Model(&model.VideoTask{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	err = query.
		Select("id, user_id, asset_id, file_md5, filename, title, file_url, file_size, status, stage, trace_id, source_type, active_text_source_id, processing_intent_json, visual_mode, visual_disabled, retry_count, max_retries, next_retry_at, last_error_code, last_error_msg, last_job_type, stage_started_at, stage_finished_at, started_at, finished_at, error_msg, created_at, updated_at").
		Order("created_at DESC, id DESC").
		Offset(offset).
		Limit(pageSize).
		Find(&tasks).Error

	return tasks, total, err
}

func applyTagFilter(query, db *gorm.DB, owner int64, filter TagFilter) (*gorm.DB, error) {
	match := filter.Match
	if match == "" {
		match = "all"
	}
	if match != "all" && match != "any" {
		return nil, artifact.Err("invalid_tag_match", 400)
	}
	if len(filter.IDs) == 0 {
		return query, nil
	}
	ids, err := NewUserTagRepository(db).ResolveIDs(context.Background(), owner, filter.IDs)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return query.Where("1 = 0"), nil
	}
	if match == "any" {
		return query.Where("EXISTS (SELECT 1 FROM video_tag_assignments AS a WHERE a.user_id = video_tasks.user_id AND a.task_id = video_tasks.id AND a.tag_id IN ?)", ids), nil
	}
	return query.Where("(SELECT COUNT(DISTINCT a.tag_id) FROM video_tag_assignments AS a WHERE a.user_id = video_tasks.user_id AND a.task_id = video_tasks.id AND a.tag_id IN ?) = ?", ids, len(ids)), nil
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

// UpdateTitle records an explicit user edit, including edits that race a worker.
func (r *TaskRepository) UpdateTitle(id int64, title string) error {
	return r.db.Model(&model.VideoTask{}).Where("id = ?", id).Updates(map[string]any{"title": title, "title_origin": "user"}).Error
}

// SetGeneratedTitleIfBlank is a database CAS: a late worker cannot replace a
// user edit or a legacy title whose source is unknown.
func (r *TaskRepository) SetGeneratedTitleIfBlank(id int64, title string) (bool, error) {
	result := r.db.Model(&model.VideoTask{}).Where("id = ? AND title = '' AND title_origin = ''", id).
		Updates(map[string]any{"title": title, "title_origin": "auto"})
	return result.RowsAffected == 1, result.Error
}

func (r *TaskRepository) RecordRetryableFailure(id int64, jobType, stage, errMsg string, retryCount, maxRetries int, nextRetryAt time.Time, errorCode ...string) error {
	now := time.Now()
	code := "retryable_error"
	if len(errorCode) > 0 && errorCode[0] != "" {
		code = errorCode[0]
	}
	updates := map[string]interface{}{
		"status":            model.TaskStatusFailed,
		"stage":             stage,
		"error_msg":         errMsg,
		"last_error_code":   code,
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
	// The two lease-expiry branches also honor next_retry_at so a redispatch
	// can be damped: with the original message possibly still queued behind
	// prefetch, an undamped sweep re-published a duplicate every lease expiry.
	err := r.db.Model(&model.VideoTask{}).
		Select("video_tasks.*").
		Joins("LEFT JOIN task_jobs AS retry_job ON retry_job.task_id = video_tasks.id AND retry_job.job_type = video_tasks.last_job_type").
		Where("video_tasks.last_job_type <> ? AND (retry_job.id IS NULL OR retry_job.retry_count <= retry_job.max_retries)", "").
		Where("((video_tasks.status = ? AND video_tasks.next_retry_at IS NOT NULL AND video_tasks.next_retry_at <= ?) OR (retry_job.status = ? AND retry_job.next_retry_at IS NOT NULL AND retry_job.next_retry_at <= ?) OR (retry_job.status IN ? AND retry_job.processing_token <> ? AND retry_job.lease_expires_at IS NOT NULL AND retry_job.lease_expires_at <= ? AND (retry_job.next_retry_at IS NULL OR retry_job.next_retry_at <= ?)) OR (retry_job.id IS NULL AND video_tasks.status IN ? AND video_tasks.processing_token <> ? AND video_tasks.lease_expires_at IS NOT NULL AND video_tasks.lease_expires_at <= ? AND (video_tasks.next_retry_at IS NULL OR video_tasks.next_retry_at <= ?)))",
			model.TaskStatusFailed, now, model.TaskStatusFailed, now, []int8{model.TaskStatusQueued, model.TaskStatusRunning}, "", now, now, []int8{model.TaskStatusQueued, model.TaskStatusRunning}, "", now, now).
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

func (r *TaskRepository) ListOwnedTaskIDs(userID int64) ([]int64, error) {
	var ids []int64
	err := r.db.Model(&model.VideoTask{}).Where("user_id = ?", userID).Order("id ASC").Pluck("id", &ids).Error
	return ids, err
}
