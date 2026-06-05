package repository

import (
	"vid-lens/internal/model"

	"gorm.io/gorm"
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

// FindByIDWithDetail 查找任务并预加载关联的转录和总结
func (r *TaskRepository) FindByIDWithDetail(id int64) (*model.VideoTask, error) {
	var task model.VideoTask
	err := r.db.
		Preload("Transcription").
		Preload("Summary").
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

// ListByUserID 分页查询用户的视频任务列表
// 面试亮点：(user_id, created_at) 联合索引，天然按时间排序
func (r *TaskRepository) ListByUserID(userID int64, page, pageSize int) ([]model.VideoTask, int64, error) {
	var tasks []model.VideoTask
	var total int64

	query := r.db.Where("user_id = ?", userID)
	query.Model(&model.VideoTask{}).Count(&total)

	offset := (page - 1) * pageSize
	err := query.
		Select("id, user_id, file_md5, filename, file_url, file_size, status, error_msg, created_at, updated_at").
		Order("created_at DESC").
		Offset(offset).
		Limit(pageSize).
		Find(&tasks).Error

	return tasks, total, err
}

// UpdateStatus 更新任务状态
func (r *TaskRepository) UpdateStatus(id int64, status int8, errMsg string) error {
	updates := map[string]interface{}{
		"status":     status,
		"error_msg":  errMsg,
	}
	return r.db.Model(&model.VideoTask{}).Where("id = ?", id).Updates(updates).Error
}

// UpdateFileURL 更新文件存储路径
func (r *TaskRepository) UpdateFileURL(id int64, fileURL string) error {
	return r.db.Model(&model.VideoTask{}).Where("id = ?", id).Update("file_url", fileURL).Error
}

// Delete 删除任务（逻辑删除）
func (r *TaskRepository) Delete(id int64) error {
	return r.db.Delete(&model.VideoTask{}, id).Error
}
