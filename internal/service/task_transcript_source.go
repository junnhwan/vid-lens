package service

import (
	"context"
	"fmt"
	"strings"

	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

// taskTranscriptSource preserves the legacy full-text projection. Active
// snapshots supply text directly and deliberately return no synthetic ASR
// chunks; provenance-aware callers use taskTextSource instead. Only legacy
// tasks may reuse the eligible shared ASR cache by media fingerprint.
func taskTranscriptSource(repos *repository.Repositories, task *model.VideoTask) (*model.VideoTranscription, []model.VideoTranscriptionChunk, error) {
	if task.ActiveTextSourceID != "" {
		source, err := taskTextSource(context.Background(), repos, task)
		if err != nil {
			return nil, nil, err
		}
		return source.Transcription, nil, nil
	}
	if !repository.LegacyResultReuseAllowed(task) {
		return nil, nil, nil
	}
	rows, err := repos.TranscriptionChunk.ListByTaskID(task.ID)
	if err != nil {
		return nil, nil, err
	}
	t, err := repos.Transcription.FindByTaskID(task.ID)
	if err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 && t == nil && repository.LegacyResultReuseAllowed(task) && task.FileMD5 != "" {
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
