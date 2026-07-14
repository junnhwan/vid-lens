package repository

import (
	"vid-lens/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TaskMessageFailureRepository struct{ db *gorm.DB }

func NewTaskMessageFailureRepository(db *gorm.DB) *TaskMessageFailureRepository {
	return &TaskMessageFailureRepository{db: db}
}

func (r *TaskMessageFailureRepository) Record(failure *model.KafkaMessageFailure) error {
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "consumer_group"}, {Name: "topic"}, {Name: "partition"}, {Name: "message_offset"}},
		DoUpdates: clause.AssignmentColumns([]string{"consumer_name", "message_key", "payload", "error_message", "updated_at"}),
	}).Create(failure).Error
}
