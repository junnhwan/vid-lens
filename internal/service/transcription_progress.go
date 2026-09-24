package service

import (
	"context"
	"fmt"
	"time"

	"vid-lens/internal/config"
	"vid-lens/internal/model"
)

// TranscriptionProgress only exposes chunks after checking task ownership.
// A chunk's audio object and provider error are intentionally omitted.
type TranscriptionProgress struct {
	TaskID           int64                        `json:"task_id"`
	Status           int8                         `json:"status"`
	Stage            string                       `json:"stage"`
	JobStatus        int8                         `json:"job_status"`
	JobRetryCount    int                          `json:"job_retry_count"`
	JobMaxRetries    int                          `json:"job_max_retries"`
	JobNextRetryAt   *time.Time                   `json:"job_next_retry_at,omitempty"`
	StartedAt        *time.Time                   `json:"started_at,omitempty"`
	UpdatedAt        time.Time                    `json:"updated_at"`
	VideoConcurrency int                          `json:"video_concurrency"`
	ChunkConcurrency int                          `json:"chunk_concurrency"`
	Total            int                          `json:"total"`
	Pending          int                          `json:"pending"`
	Running          int                          `json:"running"`
	RetryWaiting     int                          `json:"retry_waiting"`
	Completed        int                          `json:"completed"`
	Failed           int                          `json:"failed"`
	Chunks           []TranscriptionChunkProgress `json:"chunks"`
}

type TranscriptionChunkProgress struct {
	Index       int        `json:"index"`
	Status      string     `json:"status"`
	StartMS     int64      `json:"start_ms"`
	EndMS       int64      `json:"end_ms"`
	RetryCount  int        `json:"retry_count"`
	WaitReason  string     `json:"wait_reason,omitempty"`
	NextRetryAt *time.Time `json:"next_retry_at,omitempty"`
	Content     string     `json:"content,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (s *MediaService) GetTranscriptionProgress(_ context.Context, userID, taskID int64) (*TranscriptionProgress, error) {
	task, err := s.repo.Task.FindByID(taskID)
	if err != nil || task == nil || task.UserID != userID {
		return nil, fmt.Errorf("任务不存在或无权访问")
	}
	videoConcurrency := s.transcriptionMQ.TranscribePrefetch
	if videoConcurrency <= 0 {
		videoConcurrency = s.transcriptionMQ.Prefetch
	}
	if videoConcurrency <= 0 {
		videoConcurrency = 1
	}
	chunkConcurrency := s.transcriptionMQ.ASRConcurrency
	if chunkConcurrency <= 0 {
		chunkConcurrency = config.DefaultASRConcurrency
	}
	result := &TranscriptionProgress{
		TaskID: task.ID, Status: task.Status, Stage: task.Stage, StartedAt: task.StartedAt,
		UpdatedAt: task.UpdatedAt, VideoConcurrency: videoConcurrency, ChunkConcurrency: chunkConcurrency,
		Chunks: []TranscriptionChunkProgress{},
	}
	if s.repo.TaskJob != nil {
		job, err := s.repo.TaskJob.FindByTaskAndType(taskID, model.TaskJobTypeTranscribe)
		if err != nil {
			return nil, err
		}
		if job != nil {
			result.JobStatus, result.JobRetryCount, result.JobMaxRetries = job.Status, job.RetryCount, job.MaxRetries
			result.JobNextRetryAt = job.NextRetryAt
			result.StartedAt = job.StartedAt
			result.UpdatedAt = job.UpdatedAt
		}
	}
	if s.repo.TranscriptionChunk == nil {
		return result, nil
	}
	chunks, err := s.repo.TranscriptionChunk.ListByTaskID(taskID)
	if err != nil {
		return nil, err
	}
	result.Total = len(chunks)
	for _, chunk := range chunks {
		if chunk.UpdatedAt.After(result.UpdatedAt) {
			result.UpdatedAt = chunk.UpdatedAt
		}
		item := TranscriptionChunkProgress{
			Index: chunk.ChunkIndex + 1, Status: chunk.Status,
			StartMS: chunk.CoreStartMS, EndMS: chunk.CoreEndMS,
			RetryCount: chunk.RetryCount, WaitReason: chunk.WaitReason,
			NextRetryAt: chunk.NextRetryAt, UpdatedAt: chunk.UpdatedAt,
		}
		if item.EndMS == 0 {
			item.StartMS, item.EndMS = chunk.WindowStartMS, chunk.WindowEndMS
		}
		if chunk.Status == model.TranscriptionChunkStatusCompleted {
			item.Content = chunk.Content
		}
		switch chunk.Status {
		case model.TranscriptionChunkStatusPending:
			result.Pending++
		case model.TranscriptionChunkStatusRunning:
			result.Running++
		case model.TranscriptionChunkStatusRetryWait:
			result.RetryWaiting++
		case model.TranscriptionChunkStatusCompleted:
			result.Completed++
		case model.TranscriptionChunkStatusFailed:
			result.Failed++
		}
		result.Chunks = append(result.Chunks, item)
	}
	return result, nil
}
