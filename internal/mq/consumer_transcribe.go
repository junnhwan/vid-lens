package mq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/observability"
	"vid-lens/internal/pkg/ffmpeg"
	"vid-lens/internal/pkg/visualprogress"
	"vid-lens/internal/processing"
	"vid-lens/internal/repository"

	amqp "github.com/rabbitmq/amqp091-go"
)

// handleTranscribe 处理文字提取任务
func (c *Consumer) handleTranscribe(ctx context.Context, delivery amqp.Delivery) error {
	var payload AnalyzePayload
	if err := json.Unmarshal(delivery.Body, &payload); err != nil {
		return err
	}

	task, err := c.repo.Task.FindByID(payload.TaskID)
	if err != nil {
		return err
	}
	claim, err := c.claimTaskForMessage(task.ID, TaskJobTranscribe, model.TaskStageTranscribing, payload.ClaimToken)
	if err != nil {
		return fmt.Errorf("获取转录 processing lease 失败: %w", err)
	}
	switch claim.Outcome {
	case repository.TaskLeaseBusy:
		return fmt.Errorf("转录 processing lease 正由其他消费者持有")
	case repository.TaskLeaseStale:
		return errStaleDispatch
	case repository.TaskLeaseTerminal:
		return nil
	case repository.TaskLeaseAcquired:
	default:
		return fmt.Errorf("未知转录 processing lease 状态: %s", claim.Outcome)
	}
	ctx, stopLease := c.startProcessingLeaseHeartbeat(ctx, task.ID, TaskJobTranscribe, claim.Token)
	defer stopLease()
	task.TraceID = traceIDForTask(payload.TraceID, task)
	ctx = c.contextForTaskJob(ctx, task, TaskJobTranscribe, payload.BudgetID)
	job, err := c.repo.TaskJob.FindByTaskAndType(task.ID, TaskJobTranscribe)
	if err != nil {
		return err
	}
	alignmentOnly := job != nil && job.TranscriptAlignmentOnly
	recordFailure := func(failure error) error {
		stage := model.TaskStageTranscribing
		current, _ := c.repo.Task.FindByID(task.ID)
		if alignmentOnly || current != nil && current.Stage == model.TaskStageAligning {
			stage = model.TaskStageAligning
		}
		return c.recordTaskFailure(task.ID, TaskJobTranscribe, stage, failure, claim.Token)
	}
	var intent processing.Intent
	automaticSource := !alignmentOnly && task.ProcessingIntentJSON != ""
	if automaticSource {
		intent, err = processing.Decode(task.ProcessingIntentJSON)
		if err != nil {
			return recordFailure(err)
		}
	}
	// The visual branch starts from the video task, not from an ASR success.
	// Its independent download is intentional until the storage adapter exposes
	// a safe shared local-asset lease.
	waitVisual := func() visualIndexOutcome { return visualIndexOutcome{} }
	if !alignmentOnly && !automaticSource {
		waitVisual = c.startVisualIndexBranch(ctx, task)
	}
	defer waitVisual()

	videoPath, err := c.storage.DownloadToTemp(ctx, task.FileURL)
	if err != nil {
		if handled, degradeErr := c.completeTranscribeWithVisualOnly(ctx, task, claim.Token, err, waitVisual); handled {
			return degradeErr
		}
		return recordFailure(err)
	}
	defer os.Remove(videoPath)

	audioExtractStartedAt := time.Now()
	audioPath, err := ffmpeg.ExtractAudio(ctx, c.ffmpegPath, videoPath)
	c.recordASRStage(ctx, task.ID, "audio_extract", stageStatus(err), time.Since(audioExtractStartedAt))
	if err != nil {
		if handled, degradeErr := c.completeTranscribeWithVisualOnly(ctx, task, claim.Token, err, waitVisual); handled {
			return degradeErr
		}
		return recordFailure(err)
	}
	defer os.Remove(audioPath)

	var transcript string
	var sourceRows []model.VideoTranscriptionChunk
	if alignmentOnly {
		transcript, err = c.transcription().alignExistingTranscript(ctx, task.ID, audioPath)
	} else {
		taskAI, strategyErr := c.strategyForTask(task)
		if strategyErr != nil {
			return recordFailure(strategyErr)
		}
		transcript, err = c.transcription().transcribeAudio(ctx, task.ID, audioPath, taskAI, &sourceRows)
	}
	if err != nil {
		if handled, degradeErr := c.completeTranscribeWithVisualOnly(ctx, task, claim.Token, err, waitVisual); handled {
			return degradeErr
		}
		return recordFailure(err)
	}

	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	if automaticSource {
		if err := c.publishAutomaticASRSource(ctx, task, intent, claim.Token, transcript, sourceRows); err != nil {
			return recordFailure(err)
		}
		return nil
	}
	if !alignmentOnly {
		if err := c.transcription().publish(ctx, task, transcript); err != nil {
			return recordFailure(err)
		}
	}
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	if err := c.waitForVisualAfterASR(ctx, task, waitVisual); err != nil {
		return err
	}
	ragEnqueued, err := c.indexAfterTranscription(ctx, task)
	if err != nil {
		return err
	}
	if !alignmentOnly {
		if err := c.generateTitle(ctx, task, transcript); err != nil {
			return err
		}
	}
	return c.completeTranscribeAfterIndex(ctx, task, claim.Token, ragEnqueued)
}

// completeTranscribeAfterIndex finishes the transcribe job's processing lease
// and decides who owns final task completion. Final completion is only handed
// to the rag job when a rag message is actually in flight and the rag index has
// not already been delivered; otherwise the task is completed here. Handing
// completion to a rag run that already gave up (its CompleteTaskProcessing CAS
// was rejected while the transcribe lease was held, its message is Acked and
// the dedup key retained) or that was never enqueued (content+model dedup hit)
// leaves the task stuck at running/indexing forever — that is the completion
// race this boundary closes.
func (c *Consumer) completeTranscribeAfterIndex(ctx context.Context, task *model.VideoTask, token string, ragEnqueued bool) error {
	handoffToRAG := ragEnqueued && !c.ragIndexAlreadyDelivered(ctx, task.ID)
	parentStatus, parentStage := int8(model.TaskStatusCompleted), model.TaskStageNone
	if handoffToRAG {
		parentStatus, parentStage = model.TaskStatusRunning, model.TaskStageIndexing
	}
	completed, err := c.completeTaskProcessing(repository.TaskProcessingCompleteRequest{
		TaskID: task.ID, JobType: TaskJobTranscribe, JobStage: model.TaskStageTranscribing,
		Token: token, TaskStatus: parentStatus, TaskStage: parentStage, Now: c.currentTime(),
	})
	if err != nil {
		return err
	}
	if !completed {
		observability.Log(ctx, slog.Default(), slog.LevelWarn, "transcribe completion deferred: processing lease held elsewhere",
			slog.Int64("task_id", task.ID))
	}
	return nil
}

// ragIndexAlreadyDelivered reports whether the rag side has already produced
// its terminal outcome (status=indexed) for this task. A delivered index means
// no rag run remains: the enqueued job either already built the index or never
// needs to run again.
func (c *Consumer) ragIndexAlreadyDelivered(ctx context.Context, taskID int64) bool {
	if c.repo == nil || c.repo.RAGIndex == nil {
		return false
	}
	delivered, err := c.repo.RAGIndex.ExistsIndexedByTaskID(taskID)
	if err != nil {
		// Fail toward the handoff: the rag consumer still completes the task in
		// the common path, so an unavailable index probe must not complete early.
		observability.Log(ctx, slog.Default(), slog.LevelWarn, "probe rag index delivery failed",
			slog.Int64("task_id", taskID), slog.String("error", observability.SafeError(err)))
		return false
	}
	return delivered
}

// processVideo 核心业务：FFmpeg → ASR → LLM
func (c *Consumer) processVideo(ctx context.Context, task *model.VideoTask) error {
	existingTranscription, err := c.repo.Transcription.FindByTaskID(task.ID)
	if err != nil {
		return fmt.Errorf("查询转录失败: %w", err)
	}
	if existingTranscription != nil && strings.TrimSpace(existingTranscription.Content) != "" {
		observability.Log(ctx, slog.Default(), slog.LevelInfo, "reuse transcription for summary")
		return c.summarizeTask(ctx, task)
	}

	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	if err := c.transitionTaskStage(ctx, task.ID, model.TaskStageTranscribing); err != nil {
		return fmt.Errorf("更新转录阶段失败: %w", err)
	}
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	observability.Log(ctx, slog.Default(), slog.LevelInfo, "audio extraction started")
	videoPath, err := c.storage.DownloadToTemp(ctx, task.FileURL)
	if err != nil {
		return fmt.Errorf("下载视频失败: %w", err)
	}
	defer os.Remove(videoPath)
	waitVisual := c.startVisualIndexBranch(ctx, task)
	defer waitVisual()

	audioExtractStartedAt := time.Now()
	audioPath, err := ffmpeg.ExtractAudio(ctx, c.ffmpegPath, videoPath)
	c.recordASRStage(ctx, task.ID, "audio_extract", stageStatus(err), time.Since(audioExtractStartedAt))
	if err != nil {
		if outcome := waitVisual(); outcome.err == nil && outcome.count > 0 {
			if _, indexErr := c.indexAfterTranscription(ctx, task); indexErr != nil {
				return indexErr
			}
			return c.summarizeTask(ctx, task)
		}
		return fmt.Errorf("提取音频失败: %w", err)
	}
	defer os.Remove(audioPath)

	observability.Log(ctx, slog.Default(), slog.LevelInfo, "asr transcription started")
	taskAI, err := c.strategyForTask(task)
	if err != nil {
		return err
	}

	transcript, err := c.transcription().transcribeAudio(ctx, task.ID, audioPath, taskAI)
	if err != nil {
		if outcome := waitVisual(); outcome.err == nil && outcome.count > 0 {
			if _, indexErr := c.indexAfterTranscription(ctx, task); indexErr != nil {
				return indexErr
			}
			return c.summarizeTask(ctx, task)
		}
		return fmt.Errorf("语音转文字失败: %w", err)
	}

	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	if err := c.transcription().publish(ctx, task, transcript); err != nil {
		return fmt.Errorf("保存转录失败: %w", err)
	}
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	if err := c.waitForVisualAfterASR(ctx, task, waitVisual); err != nil {
		return err
	}
	if _, err := c.indexAfterTranscription(ctx, task); err != nil {
		return err
	}

	return c.summarizeTask(ctx, task)
}

func stageStatus(err error) string {
	if err != nil {
		return "failed"
	}
	return "success"
}

func (c *Consumer) recordASRStage(ctx context.Context, taskID int64, stage, status string, duration time.Duration) {
	if metrics := observability.DefaultMetrics(); metrics != nil {
		metrics.ObserveASRStage(stage, status, duration)
	}
	observability.Log(ctx, slog.Default(), slog.LevelInfo, "asr stage measured",
		slog.Int64("task_id", taskID), slog.String("asr_stage", stage), slog.String("status", status),
		slog.Float64("duration_ms", float64(duration)/float64(time.Millisecond)))
}

func (c *Consumer) strategyForTask(task *model.VideoTask, actions ...string) (ai.Strategy, error) {
	if c.profiles == nil || c.aiFactory == nil {
		if c.ai == nil {
			return nil, fmt.Errorf("请先配置 AI 服务")
		}
		return ai.NewObservedStrategy(c.ai, c.aiRecorder, ai.CallContext{
			UserID: task.UserID,
			TaskID: task.ID,
		}), nil
	}

	profile, err := c.processingProfile(task)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, fmt.Errorf("请先配置 AI 服务")
	}
	action := "transcribe"
	if len(actions) > 0 {
		action = actions[0]
	}
	if err := ai.RequireAction(*profile, action); err != nil {
		return nil, err
	}
	strategy, err := c.aiFactory.NewAnalysisStrategy(*profile)
	if err != nil {
		return nil, err
	}
	return ai.NewObservedStrategy(strategy, c.aiRecorder, ai.CallContext{
		UserID:      task.UserID,
		TaskID:      task.ID,
		ASRProvider: profile.ASRProvider,
		ASRModel:    profile.ASRModel,
		LLMProvider: profile.LLMProvider,
		LLMModel:    profile.LLMModel,
	}), nil
}

func (c *Consumer) waitForVisualAfterASR(ctx context.Context, task *model.VideoTask, waitVisual func() visualIndexOutcome) error {
	// The transcript has been persisted; any remaining wait belongs to the
	// visual branch, not ASR. Keep the lease until the whole job finishes.
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	if c.visualIndex != nil && task.EffectiveVisualMode() != model.VisualModeOff {
		if err := c.transitionTaskStage(ctx, task.ID, model.TaskStageVisual); err != nil {
			return err
		}
	}
	_ = waitVisual()
	return requireProcessingLease(ctx)
}

type visualIndexOutcome struct {
	count int
	err   error
}

func (c *Consumer) completeTranscribeWithVisualOnly(ctx context.Context, task *model.VideoTask, token string, asrErr error, waitVisual func() visualIndexOutcome) (bool, error) {
	if waitVisual == nil || task == nil {
		return false, nil
	}
	// Visual evidence can serve silent media, but cannot satisfy a failed
	// transcript refresh or discard partially recognized speech. Leave these
	// failures to the caller so the job records its error and can resume ASR.
	// In particular, an old published transcript/index is not refresh success.
	if c.repo != nil {
		previous, err := c.repo.Transcription.FindByTaskID(task.ID)
		if err != nil {
			return true, err
		}
		if previous == nil && task.FileMD5 != "" {
			previous, err = c.repo.Transcription.FindByMD5(task.FileMD5)
			if err != nil {
				return true, err
			}
		}
		if previous != nil && strings.TrimSpace(previous.Content) != "" {
			return false, nil
		}
		chunks, err := c.repo.TranscriptionChunk.ListByTaskID(task.ID)
		if err != nil {
			return true, err
		}
		for _, chunk := range chunks {
			if chunk.Status != model.TranscriptionChunkStatusCompleted || strings.TrimSpace(chunk.Content) != "" {
				return false, nil
			}
		}
	}
	visual := waitVisual()
	if visual.err != nil || visual.count <= 0 {
		return false, nil
	}
	if metrics := observability.DefaultMetrics(); metrics != nil {
		metrics.ObserveMultimodalEvidence("asr_branch", "visual", "fail_open")
	}
	observability.Log(ctx, slog.Default(), slog.LevelWarn, "asr unavailable; continuing with visual-only evidence",
		slog.Int("visual_frames", visual.count), slog.String("asr_error", observability.SafeError(asrErr)))
	ragEnqueued, err := c.indexAfterTranscription(ctx, task)
	if err != nil {
		return true, err
	}
	return true, c.completeTranscribeAfterIndex(ctx, task, token, ragEnqueued)
}

func (c *Consumer) startVisualIndexBranch(ctx context.Context, task *model.VideoTask) func() visualIndexOutcome {
	if c == nil || c.visualIndex == nil || task == nil || task.EffectiveVisualMode() == model.VisualModeOff {
		return func() visualIndexOutcome { return visualIndexOutcome{} }
	}
	result := make(chan visualIndexOutcome, 1)
	owner := processingLeaseOwnerFromContext(ctx)
	if owner != nil {
		owned, err := c.repo.BeginVisualProgress(repository.TaskProcessingLeaseRequest{
			TaskID: task.ID, JobType: owner.jobType, Token: owner.token, Now: c.currentTime(),
		})
		if err != nil || !owned {
			if err == nil {
				err = ErrProcessingLeaseLost
			}
			return func() visualIndexOutcome { return visualIndexOutcome{err: err} }
		}
		ctx = visualprogress.WithJobAttempt(ctx, owner.token, owner.jobType)
	}
	observability.Log(ctx, slog.Default(), slog.LevelInfo, "visual index branch started")
	go func() {
		// The cap bounds concurrent frame-extraction + vision/OCR fan-out across
		// tasks; a queued branch waits here without holding provider resources.
		if c.visualSlots != nil {
			select {
			case c.visualSlots <- struct{}{}:
				defer func() { <-c.visualSlots }()
			case <-ctx.Done():
				if owner != nil {
					c.finishVisualProgressForJob(task.ID, owner.jobType, owner.token, ctx.Err())
				}
				result <- visualIndexOutcome{err: ctx.Err()}
				return
			}
		}
		count, err := c.visualIndex(ctx, task)
		if owner != nil && err != nil {
			c.finishVisualProgressForJob(task.ID, owner.jobType, owner.token, err)
		}
		result <- visualIndexOutcome{count: count, err: err}
	}()
	var once sync.Once
	var outcome visualIndexOutcome
	return func() visualIndexOutcome {
		once.Do(func() {
			outcome = <-result
			if outcome.err != nil {
				if metrics := observability.DefaultMetrics(); metrics != nil {
					metrics.ObserveMultimodalEvidence("visual_index", "visual", "failed")
				}
				observability.Log(ctx, slog.Default(), slog.LevelWarn, "visual index branch failed (continuing)", slog.String("error", observability.SafeError(outcome.err)))
				return
			}
			if metrics := observability.DefaultMetrics(); metrics != nil {
				metrics.ObserveMultimodalEvidence("visual_index", "visual", "success")
			}
			observability.Log(ctx, slog.Default(), slog.LevelInfo, "visual index branch completed", slog.Int("visual_frames", outcome.count))
		})
		return outcome
	}
}

func (c *Consumer) finishVisualProgress(taskID int64, token string, outcomeErr error) {
	c.finishVisualProgressForJob(taskID, TaskJobTranscribe, token, outcomeErr)
}

func (c *Consumer) finishVisualProgressForJob(taskID int64, jobType, token string, outcomeErr error) {
	if c == nil || c.repo == nil || c.repo.VisualProgress == nil || outcomeErr == nil {
		return
	}
	current, err := c.repo.VisualProgress.Find(taskID)
	if err != nil || current == nil || current.AttemptToken != token {
		return
	}
	status, code := model.VisualProgressFailed, "visual_processing_failed"
	if errors.Is(outcomeErr, context.Canceled) {
		status, code = model.VisualProgressCanceled, "canceled"
	}
	_, _ = c.repo.AdvanceVisualProgress(repository.TaskProcessingLeaseRequest{
		TaskID: taskID, JobType: jobType, Token: token, Now: c.currentTime(),
	}, repository.VisualProgressUpdate{
		Status: status, Phase: current.Phase, TotalKnown: current.TotalKnown,
		TotalFrames: current.TotalFrames, Processed: current.Processed, Failed: current.Failed,
		OCRFailed: current.OCRFailed, VisionFailed: current.VisionFailed, ErrorCode: code,
	})
}
