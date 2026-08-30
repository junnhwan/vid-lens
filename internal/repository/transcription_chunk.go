package repository

import (
	"errors"

	"gorm.io/gorm"
	"vid-lens/internal/model"
)

type TranscriptionChunkRepository struct {
	db *gorm.DB
}

type TranscriptionChunkTimeline struct {
	SegmentKey       string
	SegmenterVersion string
	WindowStartMS    int64
	WindowEndMS      int64
	CoreStartMS      int64
	CoreEndMS        int64
}

func NewTranscriptionChunkRepository(db *gorm.DB) *TranscriptionChunkRepository {
	return &TranscriptionChunkRepository{db: db}
}

func (r *TranscriptionChunkRepository) FindByTaskAndIndex(taskID int64, chunkIndex int) (*model.VideoTranscriptionChunk, error) {
	var chunk model.VideoTranscriptionChunk
	err := r.db.Where("task_id = ? AND chunk_index = ?", taskID, chunkIndex).First(&chunk).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &chunk, nil
}

func (r *TranscriptionChunkRepository) ListByTaskID(taskID int64) ([]model.VideoTranscriptionChunk, error) {
	var chunks []model.VideoTranscriptionChunk
	err := r.db.Where("task_id = ?", taskID).Order("chunk_index ASC").Find(&chunks).Error
	return chunks, err
}

func (r *TranscriptionChunkRepository) UpsertRunning(taskID int64, chunkIndex int, audioObject string) error {
	return r.UpsertRunningWithTimeline(taskID, chunkIndex, audioObject, TranscriptionChunkTimeline{})
}

func (r *TranscriptionChunkRepository) UpsertRunningWithTimeline(taskID int64, chunkIndex int, audioObject string, timeline TranscriptionChunkTimeline) error {
	if err := validateTranscriptionTimeline(timeline); err != nil {
		return err
	}
	existing, err := r.FindByTaskAndIndex(taskID, chunkIndex)
	if err != nil {
		return err
	}
	if existing == nil {
		return r.db.Create(&model.VideoTranscriptionChunk{
			TaskID:      taskID,
			ChunkIndex:  chunkIndex,
			AudioObject: audioObject,
			SegmentKey:  timeline.SegmentKey, SegmenterVersion: timeline.SegmenterVersion,
			WindowStartMS: timeline.WindowStartMS, WindowEndMS: timeline.WindowEndMS,
			CoreStartMS: timeline.CoreStartMS, CoreEndMS: timeline.CoreEndMS,
			Status: model.TranscriptionChunkStatusRunning,
		}).Error
	}
	return r.db.Model(existing).Updates(map[string]interface{}{
		"audio_object":      audioObject,
		"segment_key":       timeline.SegmentKey,
		"segmenter_version": timeline.SegmenterVersion,
		"window_start_ms":   timeline.WindowStartMS,
		"window_end_ms":     timeline.WindowEndMS,
		"core_start_ms":     timeline.CoreStartMS,
		"core_end_ms":       timeline.CoreEndMS,
		"status":            model.TranscriptionChunkStatusRunning,
		"error_msg":         "",
	}).Error
}

func (r *TranscriptionChunkRepository) UpsertCompleted(taskID int64, chunkIndex int, audioObject, content string) error {
	return r.UpsertCompletedWithRange(taskID, chunkIndex, audioObject, content, 0, 0)
}

// UpsertCompletedWithRange persists a completed ASR segment with an optional
// provider/decoder supplied time range. A 0/0 range explicitly means that the
// segment timing is unknown; callers must not derive it from ChunkIndex.
func (r *TranscriptionChunkRepository) UpsertCompletedWithRange(taskID int64, chunkIndex int, audioObject, content string, startSecond, endSecond int) error {
	if startSecond < 0 || endSecond < startSecond {
		return gorm.ErrInvalidData
	}
	existing, err := r.FindByTaskAndIndex(taskID, chunkIndex)
	if err != nil {
		return err
	}
	chars := len([]rune(content))
	if existing == nil {
		return r.db.Create(&model.VideoTranscriptionChunk{
			TaskID:      taskID,
			ChunkIndex:  chunkIndex,
			AudioObject: audioObject,
			StartSecond: startSecond,
			EndSecond:   endSecond,
			Status:      model.TranscriptionChunkStatusCompleted,
			Content:     content,
			Chars:       chars,
			ErrorMsg:    "",
		}).Error
	}
	return r.db.Model(existing).Updates(map[string]interface{}{
		"audio_object": audioObject,
		"start_second": startSecond,
		"end_second":   endSecond,
		"status":       model.TranscriptionChunkStatusCompleted,
		"content":      content,
		"chars":        chars,
		"error_msg":    "",
	}).Error
}

func (r *TranscriptionChunkRepository) UpsertCompletedWithTimeline(taskID int64, chunkIndex int, audioObject, content string, timeline TranscriptionChunkTimeline) error {
	if err := validateTranscriptionTimeline(timeline); err != nil {
		return err
	}
	// Content is the raw ASR observation for the full overlap window. The
	// compatibility second range must therefore cover the window; core range is
	// persisted separately for later source ownership and semantic chunking.
	startSecond := int(timeline.WindowStartMS / 1000)
	endSecond := int((timeline.WindowEndMS + 999) / 1000)
	if timeline.WindowEndMS == 0 {
		startSecond, endSecond = 0, 0
	}
	existing, err := r.FindByTaskAndIndex(taskID, chunkIndex)
	if err != nil {
		return err
	}
	values := map[string]interface{}{
		"audio_object": audioObject, "segment_key": timeline.SegmentKey, "segmenter_version": timeline.SegmenterVersion,
		"window_start_ms": timeline.WindowStartMS, "window_end_ms": timeline.WindowEndMS,
		"core_start_ms": timeline.CoreStartMS, "core_end_ms": timeline.CoreEndMS,
		"start_second": startSecond, "end_second": endSecond,
		"status": model.TranscriptionChunkStatusCompleted, "content": content,
		"chars": len([]rune(content)), "error_msg": "",
	}
	if existing == nil {
		return r.db.Create(&model.VideoTranscriptionChunk{
			TaskID: taskID, ChunkIndex: chunkIndex, AudioObject: audioObject,
			SegmentKey: timeline.SegmentKey, SegmenterVersion: timeline.SegmenterVersion,
			WindowStartMS: timeline.WindowStartMS, WindowEndMS: timeline.WindowEndMS,
			CoreStartMS: timeline.CoreStartMS, CoreEndMS: timeline.CoreEndMS,
			StartSecond: startSecond, EndSecond: endSecond,
			Status: model.TranscriptionChunkStatusCompleted, Content: content, Chars: len([]rune(content)),
		}).Error
	}
	return r.db.Model(existing).Updates(values).Error
}

func validateTranscriptionTimeline(timeline TranscriptionChunkTimeline) error {
	if timeline.WindowStartMS < 0 || timeline.WindowEndMS < timeline.WindowStartMS || timeline.CoreStartMS < 0 || timeline.CoreEndMS < timeline.CoreStartMS {
		return gorm.ErrInvalidData
	}
	if timeline.CoreEndMS > 0 && (timeline.CoreStartMS < timeline.WindowStartMS || timeline.CoreEndMS > timeline.WindowEndMS) {
		return gorm.ErrInvalidData
	}
	return nil
}

func (r *TranscriptionChunkRepository) UpsertFailed(taskID int64, chunkIndex int, audioObject, errMsg string) error {
	existing, err := r.FindByTaskAndIndex(taskID, chunkIndex)
	if err != nil {
		return err
	}
	if len(errMsg) > 500 {
		errMsg = errMsg[:500]
	}
	if existing == nil {
		return r.db.Create(&model.VideoTranscriptionChunk{
			TaskID:      taskID,
			ChunkIndex:  chunkIndex,
			AudioObject: audioObject,
			Status:      model.TranscriptionChunkStatusFailed,
			ErrorMsg:    errMsg,
			RetryCount:  1,
		}).Error
	}
	return r.db.Model(existing).Updates(map[string]interface{}{
		"audio_object": audioObject,
		"status":       model.TranscriptionChunkStatusFailed,
		"error_msg":    errMsg,
		"retry_count":  existing.RetryCount + 1,
	}).Error
}

func (r *TranscriptionChunkRepository) DeleteByTaskID(taskID int64) error {
	return r.db.Where("task_id = ?", taskID).Delete(&model.VideoTranscriptionChunk{}).Error
}
