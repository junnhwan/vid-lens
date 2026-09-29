package repository

import "vid-lens/internal/model"

// SaveTranscriptionAndInvalidateIndex keeps the new source text and its
// retrieval projection in sync within the caller's processing-lease transaction.
// Old chunks must not remain searchable while the replacement index is built.
func (r *Repositories) SaveTranscriptionAndInvalidateIndex(transcription *model.VideoTranscription) error {
	if err := r.Transcription.Upsert(transcription); err != nil {
		return err
	}
	if err := r.db.Model(&model.VideoRAGIndex{}).Where("task_id = ?", transcription.TaskID).
		Updates(map[string]interface{}{
			"status":           model.RAGIndexStatusNeedsRebuild,
			"chunk_count":      0,
			"total_chunks":     0,
			"completed_chunks": 0,
			"finished_at":      nil,
		}).Error; err != nil {
		return err
	}
	return r.VideoChunk.DeleteByTaskID(transcription.TaskID)
}
