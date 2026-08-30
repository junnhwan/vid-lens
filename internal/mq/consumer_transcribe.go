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
	"vid-lens/internal/repository"
	"vid-lens/internal/transcript"

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

	videoPath, err := c.storage.DownloadToTemp(ctx, task.FileURL)
	if err != nil {
		return c.recordTaskFailure(payload.TaskID, TaskJobTranscribe, model.TaskStageTranscribing, err, claim.Token)
	}
	defer os.Remove(videoPath)

	audioExtractStartedAt := time.Now()
	audioPath, err := ffmpeg.ExtractAudio(ctx, c.ffmpegPath, videoPath)
	c.recordASRStage(ctx, task.ID, "audio_extract", stageStatus(err), time.Since(audioExtractStartedAt))
	if err != nil {
		return c.recordTaskFailure(payload.TaskID, TaskJobTranscribe, model.TaskStageTranscribing, err, claim.Token)
	}
	defer os.Remove(audioPath)

	taskAI, err := c.strategyForTask(task)
	if err != nil {
		return c.recordTaskFailure(payload.TaskID, TaskJobTranscribe, model.TaskStageTranscribing, err, claim.Token)
	}

	transcript, err := c.transcribeAudio(ctx, task.ID, audioPath, taskAI)
	if err != nil {
		return c.recordTaskFailure(payload.TaskID, TaskJobTranscribe, model.TaskStageTranscribing, err, claim.Token)
	}

	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	persistStartedAt := time.Now()
	if err := c.runLeasedSideEffect(ctx, func(repos *repository.Repositories) error {
		return repos.Transcription.Upsert(&model.VideoTranscription{
			TaskID: task.ID, FileMD5: task.FileMD5, Content: transcript, Words: len([]rune(transcript)),
		})
	}); err != nil {
		c.recordASRStage(ctx, task.ID, "persistence", "failed", time.Since(persistStartedAt))
		return c.recordTaskFailure(payload.TaskID, TaskJobTranscribe, model.TaskStageTranscribing, err, claim.Token)
	}
	c.recordASRStage(ctx, task.ID, "persistence", "success", time.Since(persistStartedAt))
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	c.indexVisualAfterTranscription(ctx, task)
	if err := c.indexAfterTranscription(ctx, task); err != nil {
		return err
	}
	if err := c.generateTitle(ctx, task, transcript); err != nil {
		return err
	}
	parentStatus, parentStage := int8(model.TaskStatusCompleted), model.TaskStageNone
	if c.ragProducer != nil {
		parentStatus, parentStage = model.TaskStatusRunning, model.TaskStageIndexing
	}
	completed, err := c.completeTaskProcessing(repository.TaskProcessingCompleteRequest{TaskID: task.ID, JobType: TaskJobTranscribe, JobStage: model.TaskStageTranscribing, Token: claim.Token, TaskStatus: parentStatus, TaskStage: parentStage, Now: c.currentTime()})
	if err != nil {
		return err
	}
	if !completed {
		return nil
	}
	return nil
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

	audioExtractStartedAt := time.Now()
	audioPath, err := ffmpeg.ExtractAudio(ctx, c.ffmpegPath, videoPath)
	c.recordASRStage(ctx, task.ID, "audio_extract", stageStatus(err), time.Since(audioExtractStartedAt))
	if err != nil {
		return fmt.Errorf("提取音频失败: %w", err)
	}
	defer os.Remove(audioPath)

	observability.Log(ctx, slog.Default(), slog.LevelInfo, "asr transcription started")
	taskAI, err := c.strategyForTask(task)
	if err != nil {
		return err
	}

	transcript, err := c.transcribeAudio(ctx, task.ID, audioPath, taskAI)
	if err != nil {
		return fmt.Errorf("语音转文字失败: %w", err)
	}

	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	persistStartedAt := time.Now()
	if err := c.runLeasedSideEffect(ctx, func(repos *repository.Repositories) error {
		return repos.Transcription.Upsert(&model.VideoTranscription{
			TaskID: task.ID, FileMD5: task.FileMD5, Content: transcript, Words: len([]rune(transcript)),
		})
	}); err != nil {
		c.recordASRStage(ctx, task.ID, "persistence", "failed", time.Since(persistStartedAt))
		return fmt.Errorf("保存转录失败: %w", err)
	}
	c.recordASRStage(ctx, task.ID, "persistence", "success", time.Since(persistStartedAt))
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	c.indexVisualAfterTranscription(ctx, task)
	if err := c.indexAfterTranscription(ctx, task); err != nil {
		return err
	}

	return c.summarizeTask(ctx, task)
}

// transcribeAudio splits long audio into 300s segments and persists each
// segment's state independently (TranscriptionChunk), so an ASR failure
// mid-video only re-runs the missing segment and reuses already-completed
// results. This is the 片级 (segment-level) half of the failure-reuse story:
// the same durable-retry idea that the job-level dispatch lease provides at MQ
// granularity is applied here at ASR-segment granularity, forming the
// "投递-处理-片级" three-layer failure-reuse chain. The real driver is the
// ASR model's single-call ≤10MB limit, which makes 300s chunking a hard
// requirement, not an optimisation.
func (c *Consumer) transcribeAudio(ctx context.Context, taskID int64, audioPath string, strategy ai.Strategy) (string, error) {
	ctx = observability.WithCorrelation(ctx, observability.Correlation{Stage: model.TaskStageTranscribing})
	observability.Log(ctx, slog.Default(), slog.LevelInfo, "asr chunking started",
		slog.Int64("task_id", taskID),
		slog.Int("segment_seconds", ffmpeg.DefaultAudioSegmentSeconds),
		slog.Int("overlap_seconds", ffmpeg.DefaultAudioSegmentOverlapSeconds))
	segmentStartedAt := time.Now()
	segments, cleanupDir, err := c.prepareAudioSegments(ctx, audioPath)
	c.recordASRStage(ctx, taskID, "segment_prepare", stageStatus(err), time.Since(segmentStartedAt))
	if err != nil {
		return "", err
	}
	if err := requireProcessingLease(ctx); err != nil {
		return "", err
	}
	if len(segments) == 0 {
		return "", fmt.Errorf("没有可转写的音频片段")
	}
	if cleanupDir != "" {
		defer os.RemoveAll(cleanupDir)
	}
	observability.Log(ctx, slog.Default(), slog.LevelInfo, "asr chunks prepared", slog.Int64("task_id", taskID), slog.Int("chunk_count", len(segments)))

	parts := make([]string, len(segments))
	type asrWork struct {
		index   int
		segment ffmpeg.AudioSegment
	}
	pending := make([]asrWork, 0, len(segments))
	for i, segment := range segments {
		if err := requireProcessingLease(ctx); err != nil {
			return "", err
		}
		if completed := c.completedTranscriptionChunk(taskID, i, segment.SegmentKey); completed != "" {
			if metrics := observability.DefaultMetrics(); metrics != nil {
				metrics.IncASRChunkReuse()
			}
			observability.Log(ctx, slog.Default(), slog.LevelInfo, "asr chunk reused", slog.Int64("task_id", taskID), slog.Int("chunk_index", i+1), slog.Int("chunk_count", len(segments)), slog.Int("output_chars", len([]rune(completed))))
			parts[i] = completed
			continue
		}

		persistStartedAt := time.Now()
		if err := c.markTranscriptionChunkRunning(ctx, taskID, i, segment); err != nil {
			c.recordASRStage(ctx, taskID, "persistence", "failed", time.Since(persistStartedAt))
			return "", err
		}
		c.recordASRStage(ctx, taskID, "persistence", "success", time.Since(persistStartedAt))
		pending = append(pending, asrWork{index: i, segment: segment})
	}

	type asrResult struct {
		work     asrWork
		text     string
		err      error
		duration time.Duration
	}
	workerCount := c.asrConcurrency
	if workerCount <= 0 {
		workerCount = 1
	}
	if workerCount > len(pending) {
		workerCount = len(pending)
	}
	if workerCount > 0 {
		jobs := make(chan asrWork, len(pending))
		results := make(chan asrResult, workerCount)
		var workers sync.WaitGroup
		workers.Add(workerCount)
		for worker := 0; worker < workerCount; worker++ {
			go func() {
				defer workers.Done()
				for {
					select {
					case <-ctx.Done():
						return
					case work, ok := <-jobs:
						if !ok {
							return
						}
						startedAt := time.Now()
						chunkStrategy := c.retryingASRStrategy(ctx, taskID, work.index, strategy)
						chunkCtx := withASROperationKey(ctx, taskID, work.index)
						text, err := chunkStrategy.Transcribe(chunkCtx, work.segment.Path)
						results <- asrResult{work: work, text: text, err: err, duration: time.Since(startedAt)}
					}
				}
			}()
		}
		for _, work := range pending {
			jobs <- work
		}
		close(jobs)
		go func() {
			workers.Wait()
			close(results)
		}()

		failures := make([]error, len(segments))
		for result := range results {
			if metrics := observability.DefaultMetrics(); metrics != nil {
				metrics.ObserveASRChunk(stageStatus(result.err), result.duration)
			}
			if result.err != nil {
				failures[result.work.index] = result.err
				if ctx.Err() == nil {
					persistStartedAt := time.Now()
					persistErr := c.markTranscriptionChunkFailed(ctx, taskID, result.work.index, result.work.segment.Path, result.err)
					c.recordASRStage(ctx, taskID, "persistence", stageStatus(persistErr), time.Since(persistStartedAt))
					if persistErr != nil {
						failures[result.work.index] = errors.Join(result.err, persistErr)
					}
				}
				continue
			}

			text := strings.TrimSpace(result.text)
			parts[result.work.index] = text
			observability.Log(ctx, slog.Default(), slog.LevelInfo, "asr chunk completed",
				slog.Int64("task_id", taskID), slog.Int("chunk_index", result.work.index+1),
				slog.Int("chunk_count", len(segments)), slog.Int("output_chars", len([]rune(text))))
			if text != "" {
				persistStartedAt := time.Now()
				persistErr := c.markTranscriptionChunkCompleted(ctx, taskID, result.work.index, result.work.segment, text)
				c.recordASRStage(ctx, taskID, "persistence", stageStatus(persistErr), time.Since(persistStartedAt))
				if persistErr != nil {
					failures[result.work.index] = persistErr
				}
			}
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		for i, failure := range failures {
			if failure != nil {
				return "", fmt.Errorf("第 %d 段 ASR 失败: %w", i+1, failure)
			}
		}
	}
	if err := requireProcessingLease(ctx); err != nil {
		return "", err
	}
	compactParts := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			compactParts = append(compactParts, part)
		}
	}
	if len(compactParts) == 0 {
		return "", fmt.Errorf("ASR 返回空结果")
	}

	stitchStartedAt := time.Now()
	stitched := transcript.Stitch(compactParts)
	if !hasOverlappingAudioWindows(segments) {
		// Compatibility adapters and historical tests provide path-only chunks.
		// Without proof that the audio inputs overlap, deduplicating equal text
		// could destroy legitimately repeated speech.
		stitched = transcript.StitchResult{Content: strings.Join(compactParts, "\n\n")}
	}
	c.recordASRStage(ctx, taskID, "stitch", "success", time.Since(stitchStartedAt))
	matchedBoundaries := 0
	for _, boundary := range stitched.Boundaries {
		if boundary.MatchRunes > 0 {
			matchedBoundaries++
		}
	}
	observability.Log(ctx, slog.Default(), slog.LevelInfo, "asr transcription completed",
		slog.Int64("task_id", taskID), slog.Int("chunk_count", len(segments)),
		slog.Int("boundary_count", len(stitched.Boundaries)), slog.Int("matched_boundaries", matchedBoundaries),
		slog.Int("output_chars", len([]rune(stitched.Content))))
	return stitched.Content, nil
}

func hasOverlappingAudioWindows(segments []ffmpeg.AudioSegment) bool {
	if len(segments) < 2 {
		return false
	}
	for i := 1; i < len(segments); i++ {
		previous, current := segments[i-1], segments[i]
		if previous.SegmentKey == "" || current.SegmentKey == "" || previous.Version == "" || current.Version == "" || previous.WindowEndMS <= current.WindowStartMS {
			return false
		}
	}
	return true
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

func withASROperationKey(ctx context.Context, taskID int64, chunkIndex int) context.Context {
	metadata := ai.GovernanceContextFromContext(ctx)
	metadata.OperationKey = fmt.Sprintf("task-%d-asr-chunk-%d", taskID, chunkIndex)
	metadata.AttemptKey = ""
	return ai.WithGovernanceContext(ctx, metadata)
}

func (c *Consumer) retryingASRStrategy(ctx context.Context, taskID int64, chunkIndex int, strategy ai.Strategy) ai.Strategy {
	policy := c.asrRetryPolicy
	metrics := observability.DefaultMetrics()
	policy.BeginAttempt = func(attempt int) {
		if metrics != nil {
			metrics.IncASRProviderInflight()
		}
		observability.Log(ctx, slog.Default(), slog.LevelInfo, "asr provider request started",
			slog.Int64("task_id", taskID), slog.Int("chunk_index", chunkIndex+1), slog.Int("attempt", attempt+1))
	}
	policy.ObserveAttempt = func(observation ai.ProviderAttemptObservation) {
		if observation.Phase == "request" {
			if metrics != nil {
				metrics.DecASRProviderInflight()
			}
			c.recordASRStage(ctx, taskID, "provider_request", stageStatus(observation.Err), observation.Duration)
		}
		if observation.Phase == "retry_wait" {
			c.recordASRStage(ctx, taskID, "retry_wait", "retry", observation.SleepDuration)
			observability.Log(ctx, slog.Default(), slog.LevelInfo, "asr provider retry wait completed",
				slog.Int64("task_id", taskID), slog.Int("chunk_index", chunkIndex+1),
				slog.Int("attempt", observation.Attempt+1),
				slog.Float64("requested_delay_ms", float64(observation.RetryDelay)/float64(time.Millisecond)))
		}
	}
	return ai.RetryStrategy(strategy, policy)
}

func (c *Consumer) prepareAudioSegments(ctx context.Context, audioPath string) ([]ffmpeg.AudioSegment, string, error) {
	// Tests and explicitly injected legacy adapters keep the old path-only seam.
	// Production consumers use the richer overlap-window adapter configured by
	// NewConsumer.
	if c.splitAudio != nil {
		paths, err := c.splitAudio(ctx, c.ffmpegPath, audioPath, ffmpeg.DefaultAudioSegmentSeconds)
		if err != nil {
			return nil, "", err
		}
		segments := make([]ffmpeg.AudioSegment, 0, len(paths))
		for i, path := range paths {
			segments = append(segments, ffmpeg.AudioSegment{Index: i, Path: path})
		}
		return segments, "", nil
	}
	split := c.splitAudioWindows
	if split == nil {
		split = ffmpeg.SplitAudioWindows
	}
	return split(ctx, c.ffmpegPath, audioPath, ffmpeg.DefaultAudioSegmentSeconds, ffmpeg.DefaultAudioSegmentOverlapSeconds)
}

func (c *Consumer) completedTranscriptionChunk(taskID int64, chunkIndex int, segmentKey string) string {
	if c.repo == nil || c.repo.TranscriptionChunk == nil {
		return ""
	}
	chunk, err := c.repo.TranscriptionChunk.FindByTaskAndIndex(taskID, chunkIndex)
	if err != nil || chunk == nil {
		return ""
	}
	if segmentKey != "" && chunk.SegmentKey != segmentKey {
		return ""
	}
	if chunk.Status == model.TranscriptionChunkStatusCompleted && strings.TrimSpace(chunk.Content) != "" {
		return strings.TrimSpace(chunk.Content)
	}
	return ""
}

func (c *Consumer) markTranscriptionChunkRunning(ctx context.Context, taskID int64, chunkIndex int, segment ffmpeg.AudioSegment) error {
	if c.repo == nil || c.repo.TranscriptionChunk == nil {
		return nil
	}
	return c.runLeasedSideEffect(ctx, func(repos *repository.Repositories) error {
		return repos.TranscriptionChunk.UpsertRunningWithTimeline(taskID, chunkIndex, segment.Path, transcriptionChunkTimeline(segment))
	})
}

func (c *Consumer) markTranscriptionChunkCompleted(ctx context.Context, taskID int64, chunkIndex int, segment ffmpeg.AudioSegment, content string) error {
	if c.repo == nil || c.repo.TranscriptionChunk == nil {
		return nil
	}
	return c.runLeasedSideEffect(ctx, func(repos *repository.Repositories) error {
		return repos.TranscriptionChunk.UpsertCompletedWithTimeline(taskID, chunkIndex, segment.Path, content, transcriptionChunkTimeline(segment))
	})
}

func transcriptionChunkTimeline(segment ffmpeg.AudioSegment) repository.TranscriptionChunkTimeline {
	return repository.TranscriptionChunkTimeline{
		SegmentKey: segment.SegmentKey, SegmenterVersion: segment.Version,
		WindowStartMS: segment.WindowStartMS, WindowEndMS: segment.WindowEndMS,
		CoreStartMS: segment.CoreStartMS, CoreEndMS: segment.CoreEndMS,
	}
}

func (c *Consumer) markTranscriptionChunkFailed(ctx context.Context, taskID int64, chunkIndex int, audioObject string, cause error) error {
	if c.repo == nil || c.repo.TranscriptionChunk == nil {
		return nil
	}
	return c.runLeasedSideEffect(ctx, func(repos *repository.Repositories) error {
		return repos.TranscriptionChunk.UpsertFailed(taskID, chunkIndex, audioObject, cause.Error())
	})
}

func (c *Consumer) strategyForTask(task *model.VideoTask) (ai.Strategy, error) {
	if c.profiles == nil || c.aiFactory == nil {
		if c.ai == nil {
			return nil, fmt.Errorf("请先配置 AI 服务")
		}
		return ai.NewObservedStrategy(c.ai, c.aiRecorder, ai.CallContext{
			UserID: task.UserID,
			TaskID: task.ID,
		}), nil
	}

	profile, err := c.profiles.GetDefaultAIProfile(task.UserID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, fmt.Errorf("请先配置 AI 服务")
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

// indexVisualAfterTranscription extracts keyframe OCR evidence. Failures are
// logged only: ASR + RAG remain the product critical path when FailOpen.
func (c *Consumer) indexVisualAfterTranscription(ctx context.Context, task *model.VideoTask) {
	if c == nil || c.visualIndex == nil || task == nil {
		return
	}
	if err := requireProcessingLease(ctx); err != nil {
		observability.Log(ctx, slog.Default(), slog.LevelWarn, "skip visual index: lease lost", slog.String("error", observability.SafeError(err)))
		return
	}
	observability.Log(ctx, slog.Default(), slog.LevelInfo, "visual index started")
	count, err := c.visualIndex(ctx, task)
	if err != nil {
		observability.Log(ctx, slog.Default(), slog.LevelWarn, "visual index failed (continuing)", slog.String("error", observability.SafeError(err)))
		return
	}
	observability.Log(ctx, slog.Default(), slog.LevelInfo, "visual index completed", slog.Int("ocr_frames", count))
}
