package mq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/observability"
	"vid-lens/internal/pkg/ytdlp"
	"vid-lens/internal/processing"
	"vid-lens/internal/repository"
	"vid-lens/internal/textsource"
)

type textSourceSubtitleAdapter interface {
	ResolveIdentity(context.Context, string) (ytdlp.BilibiliIdentity, error)
	ListSubtitleTracks(context.Context, ytdlp.BilibiliIdentity) ([]ytdlp.SubtitleTrack, error)
	FetchSelectedSubtitle(context.Context, ytdlp.BilibiliIdentity, ytdlp.SubtitleTrack) ([]byte, error)
}

type textSourceDispatchProducer interface {
	EnqueueSummary(context.Context, int64, string) error
	EnqueueTranscribe(context.Context, int64, string) error
}

func (c *Consumer) SetTextSourceAdapter(adapter textSourceSubtitleAdapter) {
	c.textSourceAdapter = adapter
}
func (c *Consumer) SetSourceDispatchProducer(producer textSourceDispatchProducer) {
	c.sourceProducer = producer
}

// handleTextSource shares the existing parent lease and dispatch recovery. It
// never starts this workflow for old imports without an explicit frozen intent.
func (c *Consumer) handleTextSource(ctx context.Context, payload AnalyzePayload) error {
	task, err := c.repo.Task.FindByID(payload.TaskID)
	if err != nil {
		return err
	}
	if task.ProcessingIntentJSON == "" {
		return nil
	}
	job, err := c.repo.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeTextSource)
	if err != nil {
		return err
	}
	if job == nil || payload.ClaimToken == "" || task.LastJobType != model.TaskJobTypeTextSource || (payload.MD5 != "" && payload.MD5 != task.FileMD5) {
		return nil
	}
	claim, err := c.claimTaskForMessage(task.ID, model.TaskJobTypeTextSource, model.TaskStageTextSource, payload.ClaimToken)
	if err != nil {
		return err
	}
	if claim.Outcome == repository.TaskLeaseStale || claim.Outcome == repository.TaskLeaseTerminal {
		return nil
	}
	if claim.Outcome != repository.TaskLeaseAcquired {
		return fmt.Errorf("文字来源正在处理中")
	}
	ctx, stop := c.startProcessingLeaseHeartbeat(ctx, task.ID, model.TaskJobTypeTextSource, claim.Token)
	defer stop()
	task, err = c.repo.Task.FindByID(task.ID)
	if err != nil {
		return err
	}
	ctx = c.contextForTaskJob(ctx, task, model.TaskJobTypeTextSource, payload.BudgetID)
	intent, err := processing.Decode(task.ProcessingIntentJSON)
	if err == nil {
		err = c.processTextSource(ctx, task, intent, claim.Token)
	}
	if err != nil {
		if errors.Is(err, ErrProcessingLeaseLost) {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err() // Shutdown leaves durable lease recovery intact.
		}
		return c.recordTaskFailure(task.ID, model.TaskJobTypeTextSource, model.TaskStageTextSource, err, claim.Token)
	}
	return nil
}

func (c *Consumer) processTextSource(ctx context.Context, task *model.VideoTask, intent processing.Intent, token string) error {
	if task.FileURL == "" || task.FileMD5 == "" {
		return fmt.Errorf("文字来源缺少已下载媒体")
	}
	if intent.Options.TextSourcePolicy == "force_asr" {
		return c.delegateTextSourceASR(ctx, task, token, "force_asr")
	}
	var identity textsource.Identity
	if json.Unmarshal([]byte(task.MediaIdentityJSON), &identity) != nil || identity.Platform != "bilibili" {
		return c.delegateTextSourceASR(ctx, task, token, "unsupported_platform")
	}
	if identity.MediaFingerprint != task.FileMD5 || identity.BVID == "" || identity.CID <= 0 || identity.PartIndex <= 0 {
		return fmt.Errorf("文字来源媒体身份不完整")
	}
	adapter := c.textSourceAdapter
	if adapter == nil {
		var err error
		adapter, err = ytdlp.NewAdapter(ytdlp.Config{YtDlpPath: c.ytdlpPath, FFmpegPath: c.ffmpegPath, CookiesPath: c.cookiesPath, ProxyURL: c.proxyURL})
		if err != nil {
			return c.delegateTextSourceASR(ctx, task, token, "subtitle_probe_unavailable")
		}
	}
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	resolved, err := adapter.ResolveIdentity(ctx, task.SourceURL)
	if err != nil {
		reason := "subtitle_probe_failed"
		if errors.Is(err, ytdlp.ErrHTTPProxyUnsupported) {
			reason = "subtitle_proxy_unsupported"
		}
		return c.delegateTextSourceASR(ctx, task, token, reason)
	}
	if resolved.BVID != identity.BVID || resolved.AID != identity.AID || resolved.CID != identity.CID || resolved.PartIndex != identity.PartIndex {
		return c.delegateTextSourceASR(ctx, task, token, "source_identity_mismatch")
	}
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	tracks, err := adapter.ListSubtitleTracks(ctx, resolved)
	if err != nil {
		return c.delegateTextSourceASR(ctx, task, token, "subtitle_probe_failed")
	}
	track, err := ytdlp.SelectSubtitleTrack(tracks, "", intent.Options.PreferredLanguage)
	if err != nil {
		return c.delegateTextSourceASR(ctx, task, token, "no_matching_subtitle")
	}
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	raw, err := adapter.FetchSelectedSubtitle(ctx, resolved, track)
	if err != nil {
		return c.delegateTextSourceASR(ctx, task, token, "subtitle_fetch_failed")
	}
	duration := resolved.DurationMS
	snapshot, err := textsource.ParseSRT(ctx, raw, textsource.ParseOptions{Identity: identity, TrackKey: track.TrackKey, Language: track.Language, SubtitleKind: track.SubtitleKind, KindBasis: track.KindBasis, DurationMS: &duration, ExpectedLanguage: intent.Options.PreferredLanguage})
	if err != nil {
		return c.delegateTextSourceASR(ctx, task, token, "subtitle_unusable", strings.Join(snapshot.Warnings, "; "))
	}
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	key, err := c.storeRawSubtitle(ctx, task, raw)
	if err != nil {
		return err
	}
	snapshot.RawObjectKey = key
	var prepared *repository.InitialTaskDispatch
	lease := &repository.TaskProcessingLeaseRequest{TaskID: task.ID, JobType: model.TaskJobTypeTextSource, Token: token, Now: c.currentTime()}
	source, err := c.repo.PublishTextSource(ctx, repository.PublishTextSourceRequest{UserID: task.UserID, TaskID: task.ID, ExpectedActiveSourceID: task.ActiveTextSourceID, Snapshot: snapshot, Lease: lease}, func(tx *repository.Repositories, source *model.VideoTextSource) error {
		completed, err := tx.CompleteTaskProcessing(repository.TaskProcessingCompleteRequest{TaskID: task.ID, JobType: model.TaskJobTypeTextSource, JobStage: model.TaskStageTextSource, Token: token, TaskStatus: model.TaskStatusCompleted, TaskStage: model.TaskStageNone, Now: c.currentTime()})
		if err != nil {
			return err
		}
		if !completed {
			return ErrProcessingLeaseLost
		}
		if err := tx.TaskJob.RecordTextSourceOutcome(task.ID, "platform_subtitle", "selected platform subtitle published"); err != nil {
			return err
		}
		if intent.Options.AutoSummary {
			dispatch, err := c.prepareAutomaticSummary(tx, task, intent, source)
			if err != nil {
				return err
			}
			prepared = &dispatch
		}
		return nil
	})
	if err != nil {
		c.deleteUnpublishedSubtitle(key)
		return err
	}
	if source.RawObjectKey != key {
		c.deleteUnpublishedSubtitle(key)
	}
	if prepared != nil {
		c.publishSourceNext(ctx, *prepared, model.TaskJobTypeSummary)
	}
	return nil
}

// prepareAutomaticSummary freezes source and generated-base identities inside
// the caller's source-publication transaction. S2 consumes this exact snapshot;
// it must not consult whatever source/profile happens to be current on retry.
func (c *Consumer) prepareAutomaticSummary(tx *repository.Repositories, task *model.VideoTask, intent processing.Intent, source *model.VideoTextSource) (repository.InitialTaskDispatch, error) {
	base, err := tx.Summary.FindByTaskID(task.ID)
	if err != nil {
		return repository.InitialTaskDispatch{}, err
	}
	input := processing.GenerationSnapshot{Intent: intent, SourceID: source.ID, SourceDigest: source.SourceDigest}
	if base != nil {
		input.ExpectedGeneratedVersion = base.GeneratedVersion
		input.ExpectedGeneratedHashKind = base.ContentHashKind
		if input.ExpectedGeneratedHashKind == "" {
			input.ExpectedGeneratedHashKind = model.SummaryHashMarkdown
		}
		input.ExpectedGeneratedHash = base.ContentDigest
		if input.ExpectedGeneratedHash == "" && input.ExpectedGeneratedHashKind == model.SummaryHashMarkdown {
			input.ExpectedGeneratedHash = artifact.Hash(base.Content)
		}
	}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return repository.InitialTaskDispatch{}, err
	}
	now := c.currentTime()
	dispatch, err := tx.PrepareInitialTaskDispatch(repository.InitialTaskDispatchRequest{Task: task, AllowedStatuses: []int8{model.TaskStatusCompleted}, JobType: model.TaskJobTypeSummary, Stage: model.TaskStageSummarizing, SummaryForce: true, Token: uuid.NewString(), Now: now, LeaseUntil: now.Add(2 * time.Minute)})
	if err != nil {
		return repository.InitialTaskDispatch{}, err
	}
	if err := tx.TaskJob.FreezeSummaryInput(task.ID, intent.GenerationID, source.ID, string(inputJSON)); err != nil {
		return repository.InitialTaskDispatch{}, err
	}
	return dispatch, nil
}

// Delegation finishes this source worker and prepares ASR dispatch in one
// transaction. No worker holds its lease waiting for another worker, and no
// RequestTranscribe admission check can race the still-running parent task.
func (c *Consumer) delegateTextSourceASR(ctx context.Context, task *model.VideoTask, token, reason string, diagnostics ...string) error {
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	var prepared repository.InitialTaskDispatch
	err := c.runLeasedSideEffect(ctx, func(tx *repository.Repositories) error {
		completed, err := tx.CompleteTaskProcessing(repository.TaskProcessingCompleteRequest{TaskID: task.ID, JobType: model.TaskJobTypeTextSource, JobStage: model.TaskStageTextSource, Token: token, TaskStatus: model.TaskStatusPending, TaskStage: model.TaskStageUploaded, Now: c.currentTime()})
		if err != nil {
			return err
		}
		if !completed {
			return ErrProcessingLeaseLost
		}
		detail := "platform subtitle unavailable; delegated to ASR"
		// Parser diagnostics contain only validation rules and cue ordinals;
		// provider errors and signed fetch addresses never enter this projection.
		if len(diagnostics) > 0 && diagnostics[0] != "" {
			detail += "; " + diagnostics[0]
		}
		if err := tx.TaskJob.RecordTextSourceOutcome(task.ID, reason, detail); err != nil {
			return err
		}
		prepared, err = tx.PrepareInitialTaskDispatch(repository.InitialTaskDispatchRequest{Task: task, AllowedStatuses: []int8{model.TaskStatusPending}, JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing, Token: uuid.NewString(), Now: c.currentTime(), LeaseUntil: c.currentTime().Add(2 * time.Minute)})
		return err
	})
	if err != nil {
		return err
	}
	c.publishSourceNext(ctx, prepared, model.TaskJobTypeTranscribe)
	return nil
}

// Publishing is only a latency optimization after commit. If the producer is
// absent or RabbitMQ is unavailable, the persisted dispatch lease is sufficient
// for RetryScheduler recovery and must not revert the completed source job.
func (c *Consumer) publishSourceNext(ctx context.Context, dispatch repository.InitialTaskDispatch, jobType string) {
	if c.sourceProducer == nil {
		return
	}
	ctx = ContextWithClaimToken(ContextWithRetryBudgetID(ctx, dispatch.RetryBudgetID), dispatch.Token)
	var err error
	switch jobType {
	case model.TaskJobTypeSummary:
		err = c.sourceProducer.EnqueueSummary(ctx, dispatch.Task.ID, dispatch.Task.FileMD5)
	case model.TaskJobTypeTranscribe:
		err = c.sourceProducer.EnqueueTranscribe(ctx, dispatch.Task.ID, dispatch.Task.FileMD5)
	default:
		err = fmt.Errorf("unknown source handoff job")
	}
	if err != nil {
		observability.Log(ctx, slog.Default(), slog.LevelWarn, "source handoff dispatch remains recoverable", slog.String("job_type", jobType))
	}
}

func (c *Consumer) storeRawSubtitle(ctx context.Context, task *model.VideoTask, raw []byte) (string, error) {
	if c.uploadLocalFile == nil {
		return "", fmt.Errorf("minio subtitle storage unavailable")
	}
	file, err := os.CreateTemp("", "vidlens-source-*.srt")
	if err != nil {
		return "", fmt.Errorf("create raw subtitle file failed")
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err = file.Write(raw); err != nil {
		file.Close()
		return "", fmt.Errorf("save raw subtitle temporary file failed")
	}
	if err = file.Close(); err != nil {
		return "", fmt.Errorf("close raw subtitle temporary file failed")
	}
	key := fmt.Sprintf("sources/%d/%d/%s.srt", task.UserID, task.ID, uuid.NewString())
	if err := requireProcessingLease(ctx); err != nil {
		return "", err
	}
	if err := c.uploadLocalFile(ctx, path, key, "application/x-subrip"); err != nil {
		return "", fmt.Errorf("minio raw subtitle upload failed")
	}
	return key, nil
}

func (c *Consumer) deleteUnpublishedSubtitle(key string) {
	if strings.TrimSpace(key) == "" || (c.storage == nil && c.deleteRawSubtitle == nil) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var err error
	if c.deleteRawSubtitle != nil {
		err = c.deleteRawSubtitle(ctx, key)
	} else {
		err = c.storage.DeleteObject(ctx, key)
	}
	if err != nil {
		observability.Log(ctx, slog.Default(), slog.LevelWarn, "unpublished subtitle object cleanup failed")
	}
}
