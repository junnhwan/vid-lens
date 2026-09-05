package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

// 任务提交、查询、删除和对象访问；不负责具体文件上传。
// RequestAnalysis 提交 AI 分析。force=true 时允许覆盖已有总结（重新调用模型）。
func (s *MediaService) RequestAnalysis(ctx context.Context, userID, taskID int64, force bool) error {
	task, err := s.repo.Task.FindByID(taskID)
	if err != nil {
		return fmt.Errorf("任务不存在")
	}
	if task.UserID != userID {
		return fmt.Errorf("无权操作此任务")
	}
	if task.Status == model.TaskStatusRunning || task.Status == model.TaskStatusQueued {
		return fmt.Errorf("任务正在处理中，请勿重复提交")
	}
	summary, err := s.repo.Summary.FindByTaskID(task.ID)
	if err != nil {
		return err
	}
	if summary != nil && !force {
		return fmt.Errorf("任务已完成，可直接查看结果")
	}

	// 内容+目标级去重（docs/architecture/data-model.md）：force=false 且当前 task 无自有摘要时，
	// 把短路查询从"当前 task 的摘要"提到"按 file_md5 查任意 task/任意用户的
	// 成功摘要"。命中 → 复用，不重跑 LLM，返回"已完成可直接查看结果"
	// （与原单 task 短路语义一致）。单 job 命中不替 task 做整体完成判定
	// （分析目标级独立）；全命中秒传到 Completed 走上传链路。
	// 仅复用成功结果：摘要表行存在即成功（无 status 列，失败不落行）。
	if !force && summary == nil {
		hit, lookupErr := s.reuseResultByFileMD5(ctx, task, model.TaskJobTypeAnalyze, func(md5 string) (bool, error) {
			existing, err := s.repo.Summary.FindByMD5(md5)
			if err != nil {
				return false, err
			}
			return existing != nil, nil
		})
		if lookupErr != nil {
			return lookupErr
		}
		if hit {
			return fmt.Errorf("任务已完成，可直接查看结果")
		}
	}

	_, err = s.enqueueInitialTask(ctx, task, initialDispatchSpec{
		allowedStatuses: []int8{model.TaskStatusPending, model.TaskStatusFailed, model.TaskStatusCompleted, model.TaskStatusDead},
		jobType:         model.TaskJobTypeAnalyze,
		stage:           model.TaskStageSummarizing,
		enqueue: func(enqueueCtx context.Context, prepared model.VideoTask) error {
			return s.mq.EnqueueAnalyze(enqueueCtx, prepared.ID, prepared.FileMD5)
		},
	})
	if errors.Is(err, repository.ErrInitialTaskDispatchConflict) {
		return fmt.Errorf("任务状态已变化，请刷新后重试")
	}
	if err != nil {
		return publicInitialDispatchError(ctx, *task, model.TaskJobTypeAnalyze, model.TaskStageSummarizing, err)
	}
	return nil
}

// RequestTranscribe 提交文字提取。force=true 时清除分片缓存并允许覆盖已有转写。
func (s *MediaService) RequestTranscribe(ctx context.Context, userID, taskID int64, force bool) error {
	task, err := s.repo.Task.FindByID(taskID)
	if err != nil {
		return fmt.Errorf("任务不存在")
	}
	if task.UserID != userID {
		return fmt.Errorf("无权操作此任务")
	}
	if task.Status == model.TaskStatusRunning || task.Status == model.TaskStatusQueued {
		return fmt.Errorf("任务正在处理中")
	}
	transcription, err := s.repo.Transcription.FindByTaskID(task.ID)
	if err != nil {
		return err
	}
	if transcription != nil && !force {
		return fmt.Errorf("文字提取已完成，可直接查看结果")
	}

	// 内容+目标级去重（docs/architecture/data-model.md）：force=false 且当前 task 无自有转写时，
	// 把短路查询从"当前 task 的转写"提到"按 file_md5 查任意 task/任意用户的
	// 成功转写"。命中 → 复用，不重跑 ASR，返回"文字提取已完成，可直接查看结果"
	// （与原单 task 短路语义一致）。分析目标级独立：转写命中不替 task 做整体
	// 完成判定（摘要可能仍缺，用户可继续 RequestAnalysis）。
	if !force && transcription == nil {
		hit, lookupErr := s.reuseResultByFileMD5(ctx, task, model.TaskJobTypeTranscribe, func(md5 string) (bool, error) {
			existing, err := s.repo.Transcription.FindByMD5(md5)
			if err != nil {
				return false, err
			}
			return existing != nil, nil
		})
		if lookupErr != nil {
			return lookupErr
		}
		if hit {
			return fmt.Errorf("文字提取已完成，可直接查看结果")
		}
	}
	if force && s.repo.TranscriptionChunk != nil {
		// 清掉 ASR 分片完成标记，避免 re-run 复用旧片段
		if err := s.repo.TranscriptionChunk.DeleteByTaskID(task.ID); err != nil {
			return fmt.Errorf("清理旧转写分片失败: %w", err)
		}
	}

	_, err = s.enqueueInitialTask(ctx, task, initialDispatchSpec{
		allowedStatuses: []int8{model.TaskStatusPending, model.TaskStatusFailed, model.TaskStatusCompleted, model.TaskStatusDead},
		jobType:         model.TaskJobTypeTranscribe,
		stage:           model.TaskStageTranscribing,
		enqueue: func(enqueueCtx context.Context, prepared model.VideoTask) error {
			return s.mq.EnqueueTranscribe(enqueueCtx, prepared.ID, prepared.FileMD5)
		},
	})
	if errors.Is(err, repository.ErrInitialTaskDispatchConflict) {
		return fmt.Errorf("任务状态已变化，请刷新后重试")
	}
	if err != nil {
		return publicInitialDispatchError(ctx, *task, model.TaskJobTypeTranscribe, model.TaskStageTranscribing, err)
	}
	return nil
}

// GetTaskDetail 获取任务详情
func (s *MediaService) GetTaskDetail(ctx context.Context, userID, taskID int64) (*model.VideoTask, error) {
	task, err := s.repo.Task.FindByIDWithDetail(taskID)
	if err != nil {
		return nil, err
	}
	if task.UserID != userID {
		return nil, fmt.Errorf("无权访问此任务")
	}
	// 内容去重秒传场景（docs/$1）：新 task 自己没有 transcription/summary
	// 行（不复制行），按 file_md5 关联任意 task/任意用户的已有成功结果行展示。
	if task.Transcription == nil && task.FileMD5 != "" {
		if existing, lookupErr := s.repo.Transcription.FindByMD5(task.FileMD5); lookupErr == nil && existing != nil {
			task.Transcription = existing
		}
	}
	if task.Summary == nil && task.FileMD5 != "" {
		if existing, lookupErr := s.repo.Summary.FindByMD5(task.FileMD5); lookupErr == nil && existing != nil {
			task.Summary = existing
		}
	}
	// 与列表一致：有正文即标记，便于前端合并后立刻灰显，无需再猜
	if task.Transcription != nil && task.Transcription.Content != "" {
		task.HasTranscription = true
	}
	if task.Summary != nil && task.Summary.Content != "" {
		task.HasSummary = true
	}
	return task, nil
}

// ListTasks 分页查询，keyword 非空时按文件名/标题搜索。
// 返回的任务会附带 has_transcription / has_summary，便于前端灰显主操作按钮且不加载正文。
func (s *MediaService) ListTasks(userID int64, page, pageSize int, keyword string) ([]model.VideoTask, int64, error) {
	tasks, total, err := s.repo.Task.ListByUserID(userID, page, pageSize, keyword)
	if err != nil {
		return nil, 0, err
	}
	if len(tasks) == 0 {
		return tasks, total, nil
	}
	ids := make([]int64, len(tasks))
	for i := range tasks {
		ids[i] = tasks[i].ID
	}
	txSet, sumSet, flagErr := s.repo.Task.ResultPresenceByTaskIDs(ids)
	if flagErr != nil {
		// 标记失败不阻断列表；前端仍可点开详情
		return tasks, total, nil
	}
	for i := range tasks {
		tasks[i].HasTranscription = txSet[tasks[i].ID]
		tasks[i].HasSummary = sumSet[tasks[i].ID]
	}
	return tasks, total, nil
}

// DeleteTask 删除
func (s *MediaService) DeleteTask(ctx context.Context, userID, taskID int64) error {
	cleanup := s.taskCleanup
	if cleanup == nil {
		return ErrTaskCleanupUnavailable
	}
	job, err := cleanup.RequestDelete(ctx, userID, taskID)
	if err != nil {
		return err
	}
	if err := cleanup.ExecuteJob(ctx, job.ID); err != nil {
		// The request is already durable and the task is hidden. Returning an
		// error would tell the client to retry an operation that has committed;
		// the scheduler owns recovery from this point.
		log.Printf("[task_cleanup] immediate cleanup deferred: task_id=%d job_id=%d err=%v", taskID, job.ID, err)
	}
	return nil
}

// GetPresignedURL 获取预签名链接
func (s *MediaService) GetPresignedURL(ctx context.Context, taskID int64) (string, error) {
	task, err := s.repo.Task.FindByID(taskID)
	if err != nil {
		return "", err
	}
	return s.storage.GetPresignedURL(ctx, task.FileURL)
}

// GetVideoTimeline returns the task-scoped canonical source projection used by
// the evidence UI. It never reads retrieval chunks, so expanded model context
// cannot become public timeline content by accident.
func (s *MediaService) GetVideoTimeline(ctx context.Context, userID, taskID int64) (*VideoTimeline, error) {
	task, err := s.repo.Task.FindByID(taskID)
	if err != nil {
		return nil, err
	}
	if task.UserID != userID {
		return nil, fmt.Errorf("无权访问此任务")
	}
	transcriptRows, err := s.repo.TranscriptionChunk.ListByTaskID(taskID)
	if err != nil {
		return nil, fmt.Errorf("读取转写时间线失败: %w", err)
	}
	// Older successful tasks may only have the compatibility full-transcript
	// row. Preserve that source in the timeline without inventing timestamps.
	if len(transcriptRows) == 0 {
		if transcription, lookupErr := s.repo.Transcription.FindByTaskID(taskID); lookupErr != nil {
			return nil, fmt.Errorf("读取转写时间线失败: %w", lookupErr)
		} else if transcription != nil && strings.TrimSpace(transcription.Content) != "" {
			transcriptRows = []model.VideoTranscriptionChunk{{
				ID: transcription.ID, TaskID: taskID, ChunkIndex: 0,
				SegmentKey: fmt.Sprintf("transcription:%d", transcription.ID),
				Status:     model.TranscriptionChunkStatusCompleted, Content: transcription.Content,
			}}
		}
	}
	frames, err := s.repo.VisualFrame.ListByTaskID(taskID)
	if err != nil {
		return nil, fmt.Errorf("读取视觉时间线失败: %w", err)
	}
	timeline := BuildVideoTimeline(taskID, transcriptRows, frames)
	timeline.Title = task.Title
	if strings.TrimSpace(timeline.Title) == "" {
		timeline.Title = task.Filename
	}
	return &timeline, nil
}

// GetPlaybackURL is owner-scoped and short-lived. A citation persists source
// identity and timestamps, never a signed URL.
func (s *MediaService) GetPlaybackURL(ctx context.Context, userID, taskID int64) (string, error) {
	task, err := s.repo.Task.FindByID(taskID)
	if err != nil {
		return "", err
	}
	if task.UserID != userID {
		return "", fmt.Errorf("无权访问此任务")
	}
	if strings.TrimSpace(task.FileURL) == "" {
		return "", fmt.Errorf("视频对象不存在")
	}
	return s.storage.GetPresignedURL(ctx, task.FileURL)
}
