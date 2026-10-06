package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"vid-lens/internal/ai"

	"vid-lens/internal/model"
	"vid-lens/internal/pkg/jwt"
	"vid-lens/internal/repository"
	"vid-lens/internal/storage"

	"gorm.io/gorm"
)

// errMediaTokenInvalid covers every rejection reason for a playback credential:
// malformed, expired, wrong task, or wrong owner. The distinction is never
// surfaced to the caller so probing cannot confirm whether a task exists.
var errMediaTokenInvalid = errors.New("播放凭证无效或已过期")

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
	if err := s.requireModelAction(userID, "summary"); err != nil {
		return err
	}
	transcription, err := s.repo.Transcription.FindByTaskID(task.ID)
	if err != nil {
		return err
	}
	if transcription == nil && task.FileMD5 != "" {
		transcription, err = s.repo.Transcription.FindByMD5(task.FileMD5)
	}
	if err != nil {
		return err
	}
	if transcription != nil && strings.TrimSpace(transcription.Content) != "" {
		existing, err := s.repo.Summary.FindByMD5(task.FileMD5)
		if err != nil {
			return err
		}
		if force || existing == nil {
			prepared, dispatchErr := s.enqueueInitialTask(ctx, task, initialDispatchSpec{
				allowedStatuses: []int8{model.TaskStatusPending, model.TaskStatusCompleted, model.TaskStatusFailed, model.TaskStatusDead},
				jobType:         model.TaskJobTypeSummary, stage: model.TaskStageSummarizing, summaryForce: force,
				enqueue: func(ctx context.Context, prepared model.VideoTask) error {
					return s.mq.EnqueueSummary(ctx, prepared.ID, prepared.FileMD5)
				},
			})
			if dispatchErr != nil && prepared.Token != "" {
				return publicInitialDispatchError(ctx, *task, model.TaskJobTypeSummary, model.TaskStageSummarizing, dispatchErr)
			}
			return dispatchErr
		}
	}
	if task.Status == model.TaskStatusRunning || task.Status == model.TaskStatusQueued {
		if task.Stage == model.TaskStageIndexing {
			return fmt.Errorf("当前正在排队或构建检索索引，任务结束后可生成摘要")
		}
		return fmt.Errorf("任务正在处理中，请勿重复提交")
	}
	// 摘要作业只读转写快照，没有转写就没有可独立生成的输入。此处不再回退到
	// analyze 合并作业：那会让摘要重新占用整条任务租约，被画面与索引串行阻塞。
	if transcription == nil || strings.TrimSpace(transcription.Content) == "" {
		return fmt.Errorf("请先完成文字提取，再生成摘要")
	}
	summary, err := s.repo.Summary.FindByTaskID(task.ID)
	if err != nil {
		return err
	}
	if summary != nil && !force {
		return fmt.Errorf("任务已完成，可直接查看结果")
	}
	// A forced regeneration must not expose the previous report as the result
	// of a failed or only partially completed new run.
	if force && summary != nil {
		if err := s.repo.Summary.DeleteByTaskID(task.ID); err != nil {
			return fmt.Errorf("清除旧摘要失败: %w", err)
		}
	}
	if force && s.repo.SummaryPart != nil {
		if err := s.repo.SummaryPart.DeleteByTaskID(task.ID); err != nil {
			return fmt.Errorf("清除旧摘要分段失败: %w", err)
		}
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

	// 到这里只可能是同内容摘要在上面两次检查之间被并发删除：摘要作业已没有
	// 可复用的既有结果，也不再回退到 analyze 合并作业（那会重新串行占用整条任务租约）。
	return fmt.Errorf("任务状态已变化，请刷新后重试")
}

// RequestTranscribe 提交文字提取。force=true 时清除分片缓存并允许覆盖已有转写。
func (s *MediaService) RequestTranscribe(ctx context.Context, userID, taskID int64, force bool, alignment ...bool) error {
	alignOnly := len(alignment) > 0 && alignment[0]
	if alignOnly && (force || len(s.tools.TranscriptAlignerCommand) == 0) {
		return fmt.Errorf("逐句对齐需要已配置的音文对齐服务，且不能同时重新识别")
	}
	task, err := s.repo.Task.FindByID(taskID)
	if err != nil {
		return fmt.Errorf("任务不存在")
	}
	if task.UserID != userID {
		return fmt.Errorf("无权操作此任务")
	}
	if !alignOnly {
		if err := s.requireModelAction(userID, "transcribe"); err != nil {
			return err
		}
	}
	if task.Status == model.TaskStatusRunning || task.Status == model.TaskStatusQueued {
		return fmt.Errorf("任务正在处理中")
	}
	transcription, err := s.repo.Transcription.FindByTaskID(task.ID)
	if err != nil {
		return err
	}
	// A failed refresh retains the last published transcript until its
	// replacement is complete. Resume missing windows without discarding the
	// successfully paid ASR work, including tasks older code marked completed.
	resumeIncomplete := task.LastJobType == model.TaskJobTypeTranscribe &&
		(task.Status == model.TaskStatusFailed || task.Status == model.TaskStatusDead)
	if !force && s.repo.TranscriptionChunk != nil {
		chunks, err := s.repo.TranscriptionChunk.ListByTaskID(task.ID)
		if err != nil {
			return err
		}
		if alignOnly {
			if len(chunks) == 0 {
				return fmt.Errorf("旧转写缺少原始音频窗口，请先重新转写")
			}
			for _, chunk := range chunks {
				if chunk.Status != model.TranscriptionChunkStatusCompleted || chunk.WindowEndMS <= chunk.WindowStartMS {
					return fmt.Errorf("请先完成全部转写窗口，再对齐句子时间")
				}
			}
		}
		for _, chunk := range chunks {
			if chunk.Status != model.TranscriptionChunkStatusCompleted {
				resumeIncomplete = true
				break
			}
		}
	}
	if transcription != nil && !force && !resumeIncomplete && !alignOnly {
		return fmt.Errorf("文字提取已完成，可直接查看结果")
	}

	// 内容+目标级去重（docs/architecture/data-model.md）：force=false 且当前 task 无自有转写时，
	// 把短路查询从"当前 task 的转写"提到"按 file_md5 查任意 task/任意用户的
	// 成功转写"。命中 → 复用，不重跑 ASR，返回"文字提取已完成，可直接查看结果"
	// （与原单 task 短路语义一致）。分析目标级独立：转写命中不替 task 做整体
	// 完成判定（摘要可能仍缺，用户可继续 RequestAnalysis）。
	if !force && transcription == nil && !resumeIncomplete && !alignOnly {
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

	_, err = s.enqueueInitialTask(ctx, task, initialDispatchSpec{
		transcriptAlignmentOnly: alignOnly,
		allowedStatuses:         []int8{model.TaskStatusPending, model.TaskStatusFailed, model.TaskStatusCompleted, model.TaskStatusDead},
		jobType:                 model.TaskJobTypeTranscribe,
		resetTranscription:      force,
		stage:                   model.TaskStageTranscribing,
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

// SetTaskVisualDisabled controls whether future transcribe runs extract frames.
// Existing frame rows, object images, and RAG projections are preserved.
func (s *MediaService) SetTaskVisualDisabled(ctx context.Context, userID, taskID int64, disabled bool) (*model.VideoTask, error) {
	mode := model.VisualModeBoth
	if disabled {
		mode = model.VisualModeOff
	}
	return s.SetTaskVisualMode(ctx, userID, taskID, mode)
}

func (s *MediaService) SetTaskVisualMode(ctx context.Context, userID, taskID int64, mode string) (*model.VideoTask, error) {
	if !model.ValidVisualMode(mode) {
		return nil, fmt.Errorf("画面模式必须为 off、ocr、caption 或 both")
	}
	updated, err := s.repo.Task.SetVisualMode(userID, taskID, mode)
	if err != nil {
		return nil, err
	}
	if !updated {
		return nil, fmt.Errorf("视频不存在或正在处理，请等待任务结束后再修改画面证据设置")
	}
	return s.GetTaskDetail(ctx, userID, taskID)
}

// RequestVisualBuild extracts only selected visual observations, then updates retrieval.
// The durable dispatch lease and retry scheduler are shared with other media jobs.
func (s *MediaService) RequestVisualBuild(ctx context.Context, userID, taskID int64) error {
	task, err := s.repo.Task.FindByID(taskID)
	if err != nil || task.UserID != userID {
		return fmt.Errorf("视频不存在或无权访问")
	}
	if task.EffectiveVisualMode() == model.VisualModeOff {
		return fmt.Errorf("请先选择 OCR、画面描述或混合模式")
	}
	if strings.TrimSpace(task.FileURL) == "" {
		return fmt.Errorf("视频尚未完成导入")
	}
	if task.VisualCaptionAllowed() {
		if err := s.requireModelAction(userID, "caption"); err != nil {
			return err
		}
	}
	producer, ok := s.mq.(interface {
		EnqueueVisual(context.Context, int64) error
	})
	if !ok {
		return fmt.Errorf("画面构建队列不可用")
	}
	_, err = s.enqueueInitialTask(ctx, task, initialDispatchSpec{
		allowedStatuses: []int8{model.TaskStatusPending, model.TaskStatusCompleted, model.TaskStatusFailed, model.TaskStatusDead},
		jobType:         model.TaskJobTypeVisual, stage: model.TaskStageVisual,
		enqueue: func(enqueueCtx context.Context, prepared model.VideoTask) error {
			return producer.EnqueueVisual(enqueueCtx, prepared.ID)
		},
	})
	if errors.Is(err, repository.ErrInitialTaskDispatchConflict) {
		return fmt.Errorf("视频正在处理，请等待当前任务结束")
	}
	if err != nil {
		return publicInitialDispatchError(ctx, *task, model.TaskJobTypeVisual, model.TaskStageVisual, err)
	}
	return nil
}

func (s *MediaService) requireModelAction(userID int64, action string) error {
	// Legacy services inject their strategy directly. Production always wires
	// the profile resolver; resolve again at submission, never trust the UI.
	if s.profiles == nil {
		return nil
	}
	profile, err := s.profiles.GetDefaultAIProfile(userID)
	if err != nil {
		return err
	}
	if profile == nil {
		return ErrAIProfileRequired
	}
	return ai.RequireAction(*profile, action)
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
	if s.repo.SummaryRevision != nil {
		effective, readErr := s.repo.SummaryRevision.Effective(ctx, userID, taskID)
		if readErr != nil {
			return nil, readErr
		}
		if effective.Revision != nil {
			// Copy the shared cache row; never mutate a result reused by another task.
			row := model.AISummary{TaskID: taskID, FileMD5: task.FileMD5, ModelName: "用户修订", Content: effective.Content, CreatedAt: effective.Revision.CreatedAt}
			if effective.Generated != nil {
				row.ID = effective.Generated.ID
			}
			task.Summary = &row
			task.SummaryRevision = &model.SummaryRevisionState{Version: effective.Version, RevisionID: effective.Revision.ID, BaseGeneratedHash: effective.BaseHash, CurrentGeneratedHash: effective.CurrentGeneratedHash, SourceStatus: effective.SourceStatus, Origin: effective.Revision.Origin}
		}
	}
	// 与列表一致：有正文即标记，便于前端合并后立刻灰显，无需再猜
	if task.Transcription != nil && task.Transcription.Content != "" {
		task.HasTranscription = true
	}
	if task.Summary != nil && task.Summary.Content != "" {
		task.HasSummary = true
	}
	for i := range task.Jobs {
		if task.Jobs[i].JobType == model.TaskJobTypeSummary {
			task.SummaryJob = &task.Jobs[i]
			break
		}
	}
	applySummaryAvailability(task)
	if !task.HasSummary && s.repo.SummaryPart != nil {
		parts, progressErr := s.repo.SummaryPart.List(task.ID)
		if progressErr != nil {
			return nil, fmt.Errorf("读取摘要进度失败: %w", progressErr)
		}
		if len(parts) > 0 {
			progress := &model.SummaryProgress{Phase: "segments"}
			maxLevel := 0
			for _, part := range parts {
				if part.Level > maxLevel {
					maxLevel = part.Level
				}
			}
			if maxLevel > 0 {
				progress.Phase = "merging"
			}
			for _, part := range parts {
				if part.Level != maxLevel {
					continue
				}
				progress.Total++
				if part.Status == "completed" {
					progress.Completed++
				}
				if part.Status == "failed" {
					progress.FailedPart = part.PartIndex + 1
				}
				if part.Status != "completed" && progress.Current == 0 {
					progress.Current, progress.StartMS, progress.EndMS = part.PartIndex+1, part.StartMS, part.EndMS
				}
			}
			task.SummaryProgress = progress
		}
	}
	if indexed, visual, presenceErr := s.repo.Task.ProcessingPresenceByTaskIDs([]int64{task.ID}); presenceErr == nil {
		task.HasRAGIndex = indexed[task.ID]
		task.VisualStatus = visual[task.ID]
	}
	if s.profiles != nil {
		if profile, profileErr := s.profiles.GetDefaultAIProfile(userID); profileErr == nil && profile != nil {
			if indexes, indexErr := s.repo.RAGIndex.ListByTaskIDsAndModel(userID, []int64{task.ID}, profile.EmbeddingModel); indexErr == nil {
				for _, index := range indexes {
					task.Retrievable = index.Status == model.RAGIndexStatusIndexed
				}
			}
		}
	}
	return task, nil
}

// ListTasks 分页查询，keyword 非空时按文件名/标题搜索。
// 返回的任务会附带 has_transcription / has_summary，便于前端灰显主操作按钮且不加载正文。
func (s *MediaService) ListTasks(userID int64, page, pageSize int, keyword string, activity ...string) ([]model.VideoTask, int64, error) {
	tasks, total, err := s.repo.Task.ListByUserID(userID, page, pageSize, keyword, activity...)
	if err != nil {
		return nil, 0, err
	}
	if len(tasks) == 0 {
		return tasks, total, nil
	}
	txSet, sumSet, flagErr := s.repo.Task.ResultPresenceByTaskIDs(tasks)
	if flagErr != nil {
		// 标记失败不阻断列表；前端仍可点开详情
		return tasks, total, nil
	}
	for i := range tasks {
		tasks[i].HasTranscription = txSet[tasks[i].ID]
		tasks[i].HasSummary = sumSet[tasks[i].ID]
	}
	ids := make([]int64, len(tasks))
	for i := range tasks {
		ids[i] = tasks[i].ID
	}
	if s.repo.SummaryRevision != nil {
		if revised, presenceErr := s.repo.SummaryRevision.Presence(context.Background(), userID, ids); presenceErr == nil {
			for i := range tasks {
				tasks[i].HasSummary = tasks[i].HasSummary || revised[tasks[i].ID]
			}
		}
	}
	if indexed, visual, presenceErr := s.repo.Task.ProcessingPresenceByTaskIDs(ids); presenceErr == nil {
		for i := range tasks {
			tasks[i].HasRAGIndex = indexed[tasks[i].ID]
			tasks[i].VisualStatus = visual[tasks[i].ID]
		}
	}
	jobs, err := s.repo.TaskJob.SummariesByTaskIDs(ids)
	if err != nil {
		return nil, 0, err
	}
	byTask := make(map[int64]*model.TaskJob, len(jobs))
	for i := range jobs {
		byTask[jobs[i].TaskID] = &jobs[i]
	}
	for i := range tasks {
		tasks[i].SummaryJob = byTask[tasks[i].ID]
		applySummaryAvailability(&tasks[i])
	}
	if s.profiles != nil {
		profile, profileErr := s.profiles.GetDefaultAIProfile(userID)
		if profileErr == nil && profile != nil {
			indexes, readErr := s.repo.RAGIndex.ListByTaskIDsAndModel(userID, ids, profile.EmbeddingModel)
			if readErr != nil {
				return nil, 0, readErr
			}
			ready := make(map[int64]bool, len(indexes))
			for _, index := range indexes {
				ready[index.TaskID] = index.Status == model.RAGIndexStatusIndexed
			}
			for i := range tasks {
				tasks[i].Retrievable = ready[tasks[i].ID]
			}
		}
	}
	return tasks, total, nil
}

func applySummaryAvailability(task *model.VideoTask) {
	task.CanSummarize = task.HasTranscription && repository.SummarySourceReady(task) && !repository.SummaryJobActive(task.SummaryJob)
	// During a forced generation, a shared cache from an earlier task is not
	// the new job's result. Publish it only after this job has completed.
	if task.SummaryJob != nil && task.SummaryJob.Status != model.TaskStatusCompleted {
		task.Summary = nil
		task.HasSummary = false
	}
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
	_, transcriptRows, err := taskTranscriptSource(s.repo, task)
	if err != nil {
		return nil, fmt.Errorf("读取转写时间线失败: %w", err)
	}
	frames, err := s.repo.VisualFrame.ListByTaskID(taskID)
	if err != nil {
		return nil, fmt.Errorf("读取视觉时间线失败: %w", err)
	}
	timeline := BuildVideoTimeline(taskID, transcriptRows, frames)
	timeline.AlignmentAvailable = len(s.tools.TranscriptAlignerCommand) > 0 && len(transcriptRows) > 0
	for _, row := range transcriptRows {
		if row.Status != model.TranscriptionChunkStatusCompleted || row.WindowStartMS < 0 || row.WindowEndMS <= row.WindowStartMS {
			timeline.AlignmentAvailable = false
			break
		}
	}
	timeline.StudySourceReason = studySourceReason(task, transcriptRows, timeline)
	timeline.StudySourceReady = timeline.StudySourceReason == ""
	timeline.VisualCoverage = visualCoverage(frames)
	timeline.Title = task.Title
	if strings.TrimSpace(timeline.Title) == "" {
		timeline.Title = task.Filename
	}
	return &timeline, nil
}

// playbackPathPrefix is the single place that knows the stream route shape.
// The frontend prefixes it with the API base to build a request path; keeping
// the two in sync here avoids a second copy of the route string.
const playbackPathPrefix = "/media/task/"

// GetPlaybackURL returns a same-origin stream path carrying a task-scoped
// credential. A signed MinIO URL cannot be used here: its host is the storage
// endpoint (loopback on the server), which no browser can reach.
func (s *MediaService) GetPlaybackURL(ctx context.Context, userID, taskID int64) (string, error) {
	return s.taskMediaURL(ctx, userID, taskID, "stream")
}

func (s *MediaService) taskMediaURL(ctx context.Context, userID, taskID int64, action string) (string, error) {
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

	token, err := jwt.GenerateMediaToken(userID, taskID, s.playbackSecret, jwt.MediaTokenTTL)
	if err != nil {
		return "", fmt.Errorf("生成播放凭证失败: %w", err)
	}
	return fmt.Sprintf("%s%d/%s?token=%s", playbackPathPrefix, taskID, action, url.QueryEscape(token)), nil
}

// OpenTaskMedia validates a playback credential and resolves it to the task's
// stored object. The credential is task-scoped, so a URL shared for one video
// cannot be replayed against another.
func (s *MediaService) OpenTaskMedia(ctx context.Context, taskID int64, token string) (*model.VideoTask, storage.Object, string, error) {
	claims, err := jwt.ParseMediaToken(token, s.playbackSecret)
	if err != nil {
		return nil, nil, "", errMediaTokenInvalid
	}
	if claims.TaskID != taskID {
		return nil, nil, "", errMediaTokenInvalid
	}

	task, err := s.repo.Task.FindByID(taskID)
	if err != nil {
		return nil, nil, "", errMediaTokenInvalid
	}
	// Ownership is re-checked against the credential's user, not the request,
	// so a deleted or reassigned task cannot be read through a stale token.
	if task.UserID != claims.UserID || strings.TrimSpace(task.FileURL) == "" {
		return nil, nil, "", errMediaTokenInvalid
	}

	object, err := s.storage.OpenObject(ctx, task.FileURL)
	if err != nil {
		return nil, nil, "", err
	}
	return task, object, s.storage.ObjectContentType(ctx, task.FileURL), nil
}

// OpenTaskVisualFrame resolves a saved frame through its task-scoped media
// credential. The browser never receives a storage object key as a URL.
func (s *MediaService) OpenTaskVisualFrame(ctx context.Context, taskID, frameID int64, token string) (storage.Object, error) {
	claims, err := jwt.ParseMediaToken(token, s.playbackSecret)
	if err != nil || claims.TaskID != taskID {
		return nil, errMediaTokenInvalid
	}
	task, err := s.repo.Task.FindByID(taskID)
	if err != nil || task.UserID != claims.UserID {
		return nil, errMediaTokenInvalid
	}
	frame, err := s.repo.VisualFrame.FindForUser(ctx, claims.UserID, taskID, frameID)
	if err != nil || frame == nil || strings.TrimSpace(frame.ObjectKey) == "" {
		return nil, errMediaTokenInvalid
	}
	return s.storage.OpenObject(ctx, frame.ObjectKey)
}

// GetDownloadURL keeps storage endpoints private and uses the same task-scoped
// authorization as playback. The download handler sets an attachment filename.
func (s *MediaService) GetDownloadURL(ctx context.Context, userID, taskID int64) (string, error) {
	return s.taskMediaURL(ctx, userID, taskID, "download")
}

// UpdateTaskTitle 由用户改写展示标题。不重建索引；已有标题不会被后续自动生成覆盖
// （自动生成只在标题为空时写入）。
func (s *MediaService) UpdateTaskTitle(ctx context.Context, userID, taskID int64, title string) (*model.VideoTask, error) {
	title = model.SanitizeVideoTitle(title)
	if title == "" {
		return nil, ErrTaskTitleRequired
	}
	task, err := s.repo.Task.FindByID(taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}
	if task.UserID != userID {
		return nil, ErrTaskNotFound
	}
	if err := s.repo.Task.UpdateTitle(taskID, title); err != nil {
		return nil, err
	}
	return s.GetTaskDetail(ctx, userID, taskID)
}
