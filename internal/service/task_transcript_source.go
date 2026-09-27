package service

import (
	"fmt"
	"strings"

	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

// The upload dedup path creates a new owner-scoped task for identical media
// without copying ASR rows. Read the already published transcript by the
// shared content fingerprint, as task detail already does. Its source identity
// and content remain stable across timeline, retrieval and artifact snapshots.
func taskTranscriptSource(repos *repository.Repositories, task *model.VideoTask) (*model.VideoTranscription, []model.VideoTranscriptionChunk, error) {
	rows, err := repos.TranscriptionChunk.ListByTaskID(task.ID)
	if err != nil {
		return nil, nil, err
	}
	t, err := repos.Transcription.FindByTaskID(task.ID)
	if err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 && t == nil && task.FileMD5 != "" {
		t, err = repos.Transcription.FindByMD5(task.FileMD5)
		if err != nil {
			return nil, nil, err
		}
		if t != nil && t.TaskID != task.ID {
			rows, err = repos.TranscriptionChunk.ListByTaskID(t.TaskID)
			if err != nil {
				return nil, nil, err
			}
		}
	}
	if len(rows) == 0 && t != nil && strings.TrimSpace(t.Content) != "" {
		rows = []model.VideoTranscriptionChunk{{ID: t.ID, TaskID: task.ID, SegmentKey: fmt.Sprintf("transcription:%d", t.ID), Status: model.TranscriptionChunkStatusCompleted, Content: t.Content}}
	}
	return t, rows, nil
}
