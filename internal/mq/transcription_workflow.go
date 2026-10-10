package mq

import (
	"context"
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
)

// transcriptionWorkflow owns recognition, durable window reuse, assembly and
// publication. The scheduler supplies lease-fenced writes and stage transitions;
// it alone acknowledges messages and commits job terminal states.
type transcriptionWorkflow struct {
	repo                *repository.Repositories
	ffmpegPath          string
	splitAudio          splitAudioFunc
	splitAudioWindows   splitAudioWindowsFunc
	asrConcurrency      int
	asrRetryPolicy      ai.ProviderRetryPolicy
	transcriptAligner   transcript.Aligner
	runLeasedSideEffect func(context.Context, func(*repository.Repositories) error) error
	transitionTaskStage func(context.Context, int64, string) error
	recordASRStage      func(context.Context, int64, string, string, time.Duration)
}

func (c *Consumer) transcription() *transcriptionWorkflow {
	return &transcriptionWorkflow{repo: c.repo, ffmpegPath: c.ffmpegPath,
		splitAudio: c.splitAudio, splitAudioWindows: c.splitAudioWindows,
		asrConcurrency: c.asrConcurrency, asrRetryPolicy: c.asrRetryPolicy,
		transcriptAligner: c.transcriptAligner, runLeasedSideEffect: c.runLeasedSideEffect,
		transitionTaskStage: c.transitionTaskStage, recordASRStage: c.recordASRStage}
}

func (c *transcriptionWorkflow) publish(ctx context.Context, task *model.VideoTask, content string) error {
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	started := time.Now()
	err := c.runLeasedSideEffect(ctx, func(repos *repository.Repositories) error {
		return repos.SaveTranscriptionAndInvalidateIndex(&model.VideoTranscription{
			TaskID: task.ID, FileMD5: task.FileMD5, Content: content, Words: len([]rune(content)),
		})
	})
	c.recordASRStage(ctx, task.ID, "persistence", stageStatus(err), time.Since(started))
	return err
}

// transcribeAudio splits audio into bounded speech windows and persists each
// segment's state independently (TranscriptionChunk), so an ASR failure
// mid-video only re-runs the missing segment and reuses already-completed
// results. This is the 片级 (segment-level) half of the failure-reuse story:
// the same durable-retry idea that the job-level dispatch lease provides at MQ
// granularity is applied here at ASR-segment granularity, forming the
// "投递-处理-片级" three-layer failure-reuse chain.
// Short windows also bound citation uncertainty when an ASR provider returns
// text only. This increases request count in exchange for usable playback
// ranges; provider-native segment times are retained whenever available.
// retainedRows optionally receives the exact ordered observations assembled on
// success. Source publication must use these rather than unrelated stale rows
// from an earlier segmentation recipe still present in the task's chunk table.
func (c *transcriptionWorkflow) transcribeAudio(ctx context.Context, taskID int64, audioPath string, strategy ai.Strategy, retainedRows ...*[]model.VideoTranscriptionChunk) (string, error) {
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
		if completed, found := c.completedTranscriptionChunk(taskID, i, segment.SegmentKey); found {
			if metrics := observability.DefaultMetrics(); metrics != nil {
				metrics.IncASRChunkReuse()
			}
			observability.Log(ctx, slog.Default(), slog.LevelInfo, "asr chunk reused", slog.Int64("task_id", taskID), slog.Int("chunk_index", i+1), slog.Int("chunk_count", len(segments)), slog.Int("output_chars", len([]rune(completed))))
			parts[i] = completed
			continue
		}

		persistStartedAt := time.Now()
		if err := c.markTranscriptionChunkPending(ctx, taskID, i, segment); err != nil {
			c.recordASRStage(ctx, taskID, "persistence", "failed", time.Since(persistStartedAt))
			return "", err
		}
		c.recordASRStage(ctx, taskID, "persistence", "success", time.Since(persistStartedAt))
		pending = append(pending, asrWork{index: i, segment: segment})
	}

	type asrResult struct {
		work     asrWork
		output   ai.TranscriptionResult
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
						if err := c.markTranscriptionChunkRunning(ctx, taskID, work.index, work.segment); err != nil {
							results <- asrResult{work: work, err: err}
							continue
						}
						chunkStrategy := c.retryingASRStrategy(ctx, taskID, work.index, strategy)
						chunkCtx := withASROperationKey(ctx, taskID, work.index)
						output, err := ai.TranscribeDetailed(chunkCtx, chunkStrategy, work.segment.Path)
						results <- asrResult{work: work, output: output, err: err, duration: time.Since(startedAt)}
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

			text := strings.TrimSpace(result.output.Text)
			parts[result.work.index] = text
			observability.Log(ctx, slog.Default(), slog.LevelInfo, "asr chunk completed",
				slog.Int64("task_id", taskID), slog.Int("chunk_index", result.work.index+1),
				slog.Int("chunk_count", len(segments)), slog.Int("output_chars", len([]rune(text))))
			persistStartedAt := time.Now()
			var timed []model.TranscriptionSegment
			if text != "" {
				timed = absoluteTranscriptionSegments(result.output.Segments, result.work.segment)
			}
			persistErr := c.markTranscriptionChunkCompleted(ctx, taskID, result.work.index, result.work.segment, text, timed)
			c.recordASRStage(ctx, taskID, "persistence", stageStatus(persistErr), time.Since(persistStartedAt))
			if persistErr != nil {
				failures[result.work.index] = persistErr
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
	rows := make([]model.VideoTranscriptionChunk, len(segments))
	for i, segment := range segments {
		rows[i] = model.VideoTranscriptionChunk{TaskID: taskID, ChunkIndex: i, Content: parts[i], Status: model.TranscriptionChunkStatusCompleted,
			SegmentKey: segment.SegmentKey, SegmenterVersion: segment.Version,
			WindowStartMS: segment.WindowStartMS, WindowEndMS: segment.WindowEndMS, CoreStartMS: segment.CoreStartMS, CoreEndMS: segment.CoreEndMS}
		if c.repo != nil && c.repo.TranscriptionChunk != nil {
			stored, err := c.repo.TranscriptionChunk.FindByTaskAndIndex(taskID, i)
			if err != nil {
				return "", err
			}
			if stored != nil && stored.SegmentKey == segment.SegmentKey {
				rows[i] = *stored
			}
		}
	}
	// An installed aligner is a capability, not authorization to run a local
	// model. Only an explicit, durable alignment-only job may invoke it.
	stitchStartedAt := time.Now()
	stitched := transcript.Assemble(rows)
	if strings.TrimSpace(stitched.Content) == "" {
		return "", fmt.Errorf("ASR 返回空结果")
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
	for _, destination := range retainedRows {
		if destination != nil {
			*destination = append([]model.VideoTranscriptionChunk(nil), rows...)
		}
	}
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

func withASROperationKey(ctx context.Context, taskID int64, chunkIndex int) context.Context {
	metadata := ai.GovernanceContextFromContext(ctx)
	metadata.OperationKey = fmt.Sprintf("task-%d-asr-chunk-%d", taskID, chunkIndex)
	metadata.AttemptKey = ""
	return ai.WithGovernanceContext(ctx, metadata)
}

func (c *transcriptionWorkflow) retryingASRStrategy(ctx context.Context, taskID int64, chunkIndex int, strategy ai.Strategy) ai.Strategy {
	policy := c.asrRetryPolicy
	metrics := observability.DefaultMetrics()
	policy.BeginAttempt = func(attempt int) {
		if c.repo != nil && c.repo.TranscriptionChunk != nil {
			if err := c.repo.TranscriptionChunk.MarkAttempt(taskID, chunkIndex, attempt); err != nil {
				observability.Log(ctx, slog.Default(), slog.LevelWarn, "asr attempt progress update failed", slog.Int64("task_id", taskID), slog.Int("chunk_index", chunkIndex+1), slog.String("error", observability.SafeError(err)))
			}
		}
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
			if observation.Err != nil && observation.RetryDelay > 0 && c.repo != nil && c.repo.TranscriptionChunk != nil {
				reason := "provider_retry"
				var providerErr *ai.ProviderError
				var admissionErr *ai.AdmissionError
				switch {
				case errors.As(observation.Err, &admissionErr):
					reason = "local_admission"
				case errors.As(observation.Err, &providerErr) && providerErr.Class == ai.ErrorRateLimited:
					reason = "provider_rate_limit"
				}
				if err := c.repo.TranscriptionChunk.MarkRetryWait(taskID, chunkIndex, observation.Attempt+1, reason, time.Now().Add(observation.RetryDelay)); err != nil {
					observability.Log(ctx, slog.Default(), slog.LevelWarn, "asr retry progress update failed", slog.Int64("task_id", taskID), slog.Int("chunk_index", chunkIndex+1), slog.String("error", observability.SafeError(err)))
				}
			}
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

func (c *transcriptionWorkflow) prepareAudioSegments(ctx context.Context, audioPath string) ([]ffmpeg.AudioSegment, string, error) {
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

func (c *transcriptionWorkflow) completedTranscriptionChunk(taskID int64, chunkIndex int, segmentKey string) (string, bool) {
	if c.repo == nil || c.repo.TranscriptionChunk == nil {
		return "", false
	}
	chunk, err := c.repo.TranscriptionChunk.FindByTaskAndIndex(taskID, chunkIndex)
	if err != nil || chunk == nil {
		return "", false
	}
	if segmentKey != "" && chunk.SegmentKey != segmentKey {
		return "", false
	}
	if chunk.Status == model.TranscriptionChunkStatusCompleted {
		return strings.TrimSpace(chunk.Content), true
	}
	return "", false
}

func (c *transcriptionWorkflow) markTranscriptionChunkRunning(ctx context.Context, taskID int64, chunkIndex int, segment ffmpeg.AudioSegment) error {
	if c.repo == nil || c.repo.TranscriptionChunk == nil {
		return nil
	}
	return c.runLeasedSideEffect(ctx, func(repos *repository.Repositories) error {
		return repos.TranscriptionChunk.UpsertRunningWithTimeline(taskID, chunkIndex, segment.Path, transcriptionChunkTimeline(segment))
	})
}

func (c *transcriptionWorkflow) markTranscriptionChunkPending(ctx context.Context, taskID int64, chunkIndex int, segment ffmpeg.AudioSegment) error {
	if c.repo == nil || c.repo.TranscriptionChunk == nil {
		return nil
	}
	return c.runLeasedSideEffect(ctx, func(repos *repository.Repositories) error {
		return repos.TranscriptionChunk.UpsertPendingWithTimeline(taskID, chunkIndex, segment.Path, transcriptionChunkTimeline(segment))
	})
}

func (c *transcriptionWorkflow) markTranscriptionChunkCompleted(ctx context.Context, taskID int64, chunkIndex int, segment ffmpeg.AudioSegment, content string, timed []model.TranscriptionSegment) error {
	if c.repo == nil || c.repo.TranscriptionChunk == nil {
		return nil
	}
	return c.runLeasedSideEffect(ctx, func(repos *repository.Repositories) error {
		return repos.TranscriptionChunk.UpsertCompletedWithTimedSegments(taskID, chunkIndex, segment.Path, content, transcriptionChunkTimeline(segment), timed)
	})
}

// Invalid provider timing cannot become precise evidence. Keep valid observed
// spans in provider order; the unmatched text still has the coarse audio range.
func absoluteTranscriptionSegments(segments []model.TranscriptionSegment, window ffmpeg.AudioSegment) []model.TranscriptionSegment {
	if window.WindowStartMS < 0 || window.WindowEndMS <= window.WindowStartMS {
		return nil
	}
	durationMS := window.WindowEndMS - window.WindowStartMS
	var previousStart int64
	out := make([]model.TranscriptionSegment, 0, len(segments))
	for _, segment := range segments {
		segment.Text = strings.TrimSpace(segment.Text)
		if segment.Text == "" || segment.StartMS < 0 || segment.EndMS <= segment.StartMS ||
			segment.EndMS > durationMS || (len(out) > 0 && segment.StartMS < previousStart) {
			continue
		}
		previousStart = segment.StartMS
		segment.StartMS += window.WindowStartMS
		segment.EndMS += window.WindowStartMS
		out = append(out, segment)
	}
	return out
}

func transcriptionChunkTimeline(segment ffmpeg.AudioSegment) repository.TranscriptionChunkTimeline {
	return repository.TranscriptionChunkTimeline{
		SegmentKey: segment.SegmentKey, SegmenterVersion: segment.Version,
		WindowStartMS: segment.WindowStartMS, WindowEndMS: segment.WindowEndMS,
		CoreStartMS: segment.CoreStartMS, CoreEndMS: segment.CoreEndMS,
	}
}

func (c *transcriptionWorkflow) markTranscriptionChunkFailed(ctx context.Context, taskID int64, chunkIndex int, audioObject string, cause error) error {
	if c.repo == nil || c.repo.TranscriptionChunk == nil {
		return nil
	}
	return c.runLeasedSideEffect(ctx, func(repos *repository.Repositories) error {
		return repos.TranscriptionChunk.UpsertFailed(taskID, chunkIndex, audioObject, cause.Error())
	})
}
