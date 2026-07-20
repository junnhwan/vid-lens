package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/observability"
	"vid-lens/internal/pkg/ffmpeg"
	"vid-lens/internal/repository"

	"github.com/segmentio/kafka-go"
)

// handleTranscribe 处理文字提取任务
func (c *Consumer) handleTranscribe(ctx context.Context, msg kafka.Message) error {
	var payload AnalyzePayload
	if err := json.Unmarshal(msg.Value, &payload); err != nil {
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
	case repository.TaskLeaseStale, repository.TaskLeaseTerminal:
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

	audioPath, err := ffmpeg.ExtractAudio(ctx, c.ffmpegPath, videoPath)
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
	if err := c.runLeasedSideEffect(ctx, func(repos *repository.Repositories) error {
		return repos.Transcription.Upsert(&model.VideoTranscription{
			TaskID: task.ID, Content: transcript, Words: len([]rune(transcript)),
		})
	}); err != nil {
		return c.recordTaskFailure(payload.TaskID, TaskJobTranscribe, model.TaskStageTranscribing, err, claim.Token)
	}
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

	audioPath, err := ffmpeg.ExtractAudio(ctx, c.ffmpegPath, videoPath)
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
	if err := c.runLeasedSideEffect(ctx, func(repos *repository.Repositories) error {
		return repos.Transcription.Upsert(&model.VideoTranscription{
			TaskID: task.ID, Content: transcript, Words: len([]rune(transcript)),
		})
	}); err != nil {
		return fmt.Errorf("保存转录失败: %w", err)
	}
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	c.indexVisualAfterTranscription(ctx, task)
	if err := c.indexAfterTranscription(ctx, task); err != nil {
		return err
	}

	return c.summarizeTask(ctx, task)
}

func (c *Consumer) transcribeAudio(ctx context.Context, taskID int64, audioPath string, strategy ai.Strategy) (string, error) {
	ctx = observability.WithCorrelation(ctx, observability.Correlation{Stage: model.TaskStageTranscribing})
	splitAudio := c.splitAudio
	if splitAudio == nil {
		splitAudio = ffmpeg.SplitAudio
	}

	observability.Log(ctx, slog.Default(), slog.LevelInfo, "asr chunking started", slog.Int64("task_id", taskID), slog.Int("segment_seconds", ffmpeg.DefaultAudioSegmentSeconds))
	chunks, err := splitAudio(ctx, c.ffmpegPath, audioPath, ffmpeg.DefaultAudioSegmentSeconds)
	if err != nil {
		return "", err
	}
	if err := requireProcessingLease(ctx); err != nil {
		return "", err
	}
	if len(chunks) == 0 {
		return "", fmt.Errorf("没有可转写的音频片段")
	}
	defer os.RemoveAll(filepath.Dir(chunks[0]))
	observability.Log(ctx, slog.Default(), slog.LevelInfo, "asr chunks prepared", slog.Int64("task_id", taskID), slog.Int("chunk_count", len(chunks)))

	parts := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		if err := requireProcessingLease(ctx); err != nil {
			return "", err
		}
		if completed := c.completedTranscriptionChunk(taskID, i); completed != "" {
			if metrics := observability.DefaultMetrics(); metrics != nil {
				metrics.IncASRChunkReuse()
			}
			observability.Log(ctx, slog.Default(), slog.LevelInfo, "asr chunk reused", slog.Int64("task_id", taskID), slog.Int("chunk_index", i+1), slog.Int("chunk_count", len(chunks)), slog.Int("output_chars", len([]rune(completed))))
			parts = append(parts, completed)
			continue
		}

		if err := c.markTranscriptionChunkRunning(ctx, taskID, i, chunk); err != nil {
			return "", err
		}
		if err := requireProcessingLease(ctx); err != nil {
			return "", err
		}
		chunkStartedAt := time.Now()
		text, transcribeErr := strategy.Transcribe(ctx, chunk)
		if err := requireProcessingLease(ctx); err != nil {
			return "", err
		}
		if transcribeErr != nil {
			if metrics := observability.DefaultMetrics(); metrics != nil {
				metrics.ObserveASRChunk("failed", time.Since(chunkStartedAt))
			}
			if err := c.markTranscriptionChunkFailed(ctx, taskID, i, chunk, transcribeErr); err != nil {
				return "", err
			}
			if err := requireProcessingLease(ctx); err != nil {
				return "", err
			}
			return "", fmt.Errorf("第 %d 段 ASR 失败: %w", i+1, transcribeErr)
		}
		if metrics := observability.DefaultMetrics(); metrics != nil {
			metrics.ObserveASRChunk("success", time.Since(chunkStartedAt))
		}
		text = strings.TrimSpace(text)
		chars := len([]rune(text))
		observability.Log(ctx, slog.Default(), slog.LevelInfo, "asr chunk completed", slog.Int64("task_id", taskID), slog.Int("chunk_index", i+1), slog.Int("chunk_count", len(chunks)), slog.Int("output_chars", chars))
		if text != "" {
			if err := c.markTranscriptionChunkCompleted(ctx, taskID, i, chunk, text); err != nil {
				return "", err
			}
			if err := requireProcessingLease(ctx); err != nil {
				return "", err
			}
			parts = append(parts, text)
		}
	}
	if err := requireProcessingLease(ctx); err != nil {
		return "", err
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("ASR 返回空结果")
	}

	transcript := strings.Join(parts, "\n\n")
	observability.Log(ctx, slog.Default(), slog.LevelInfo, "asr transcription completed", slog.Int64("task_id", taskID), slog.Int("chunk_count", len(chunks)), slog.Int("output_chars", len([]rune(transcript))))
	return transcript, nil
}

func (c *Consumer) completedTranscriptionChunk(taskID int64, chunkIndex int) string {
	if c.repo == nil || c.repo.TranscriptionChunk == nil {
		return ""
	}
	chunk, err := c.repo.TranscriptionChunk.FindByTaskAndIndex(taskID, chunkIndex)
	if err != nil || chunk == nil {
		return ""
	}
	if chunk.Status == model.TranscriptionChunkStatusCompleted && strings.TrimSpace(chunk.Content) != "" {
		return strings.TrimSpace(chunk.Content)
	}
	return ""
}

func (c *Consumer) markTranscriptionChunkRunning(ctx context.Context, taskID int64, chunkIndex int, audioObject string) error {
	if c.repo == nil || c.repo.TranscriptionChunk == nil {
		return nil
	}
	return c.runLeasedSideEffect(ctx, func(repos *repository.Repositories) error {
		return repos.TranscriptionChunk.UpsertRunning(taskID, chunkIndex, audioObject)
	})
}

func (c *Consumer) markTranscriptionChunkCompleted(ctx context.Context, taskID int64, chunkIndex int, audioObject, content string) error {
	if c.repo == nil || c.repo.TranscriptionChunk == nil {
		return nil
	}
	return c.runLeasedSideEffect(ctx, func(repos *repository.Repositories) error {
		return repos.TranscriptionChunk.UpsertCompleted(taskID, chunkIndex, audioObject, content)
	})
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
