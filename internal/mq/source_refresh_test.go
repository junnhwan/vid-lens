package mq

import (
	"context"
	"github.com/google/uuid"
	"testing"
	"time"
	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/ffmpeg"
	"vid-lens/internal/processing"
	"vid-lens/internal/repository"
	"vid-lens/internal/summarydoc"
)

func TestSourceRefreshWorkerUnchangedSubtitleDoesNotRegenerateOrRunASR(t *testing.T) {
	c, repos, db, task, payload, adapter, producer, _ := sourceWorkerFixture(t, true, "prefer_platform")
	ctx := context.Background()
	if err := c.handleTextSource(ctx, payload); err != nil {
		t.Fatal(err)
	}
	current, _ := repos.Task.FindByID(task.ID)
	source, _ := repos.TextSource.Active(ctx, task.UserID, task.ID)
	old, _ := processing.Decode(current.ProcessingIntentJSON)
	db.Model(&model.TaskJob{}).Where("task_id=? AND job_type=?", task.ID, model.TaskJobTypeSummary).Updates(map[string]any{"status": model.TaskStatusCompleted, "processing_token": "", "lease_kind": "", "lease_expires_at": nil})
	doc := summarydoc.Document{SchemaVersion: summarydoc.SchemaVersion, DocumentID: old.GenerationID, SourceID: source.ID, SourceDigest: source.SourceDigest, MediaRevision: task.FileMD5, PresentationMode: "text", Title: "旧摘要", Overview: "来源中的文字", Blocks: []summarydoc.Block{{ID: "one", Order: 0, BodyMarkdown: "已生成内容"}}}
	raw, _ := summarydoc.CanonicalJSON(doc)
	digest, _ := summarydoc.Digest(doc)
	markdown, _ := summarydoc.Markdown(doc)
	base := model.AISummary{TaskID: task.ID, FileMD5: task.FileMD5, Content: markdown, DocumentJSON: string(raw), SchemaVersion: summarydoc.SchemaVersion, ContentDigest: digest, ContentHashKind: summarydoc.HashKind, GenerationID: old.GenerationID, GeneratedVersion: 1, SourceID: source.ID, SourceDigest: source.SourceDigest}
	db.Create(&base)
	db.First(&base, "id=?", base.ID)
	before := artifact.JSON(base)
	next := old
	next.ID = uuid.NewString()
	next.GenerationID = uuid.NewString()
	next.Options.AutoTagsEnabled = true
	next.Options.MindmapEnabled = !old.Options.MindmapEnabled
	next.TagVocabulary = &processing.TagVocabularySnapshot{Version: 9}
	frozen := processing.SourceRefreshSnapshot{Operation: processing.OperationSourceRefresh, Intent: next, ExpectedActiveSourceID: source.ID, PreviousInputFingerprint: processing.SourceSummaryInputFingerprint(old)}
	out, err := repos.PrepareSourceRefresh(ctx, repository.PrepareSourceRefreshRequest{UserID: task.UserID, TaskID: task.ID, ExpectedSourceID: source.ID, ExpectedIntentJSON: current.ProcessingIntentJSON, MediaFingerprint: task.FileMD5, Snapshot: frozen, Token: "refresh-source", Now: time.Now(), LeaseUntil: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	refresh := AnalyzePayload{TaskID: task.ID, MD5: task.FileMD5, ClaimToken: out.Token, BudgetID: out.RetryBudgetID}
	if err = c.handleTextSource(ctx, refresh); err != nil {
		t.Fatal(err)
	}
	if err = c.handleTextSource(ctx, refresh); err != nil {
		t.Fatal(err)
	}
	job, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeTextSource)
	after, _ := repos.Summary.FindByTaskID(task.ID)
	if job.Status != model.TaskStatusCompleted || job.LastErrorCode != "source_unchanged" || producer.summaryCalls != 1 || producer.asrCalls != 0 || adapter.fetches != 2 || artifact.JSON(after) != before {
		t.Fatal("unchanged refresh spent ASR/summary or rewrote original")
	}
}

func TestSourceRefreshFallbackCarriesNewFrozenIdentityThroughASRPublication(t *testing.T) {
	c, repos, db, task, payload, adapter, producer, _ := sourceWorkerFixture(t, false, "prefer_platform")
	if err := c.handleTextSource(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	current, _ := repos.Task.FindByID(task.ID)
	oldSource, _ := repos.TextSource.Active(context.Background(), task.UserID, task.ID)
	old, _ := processing.Decode(current.ProcessingIntentJSON)
	next := old
	next.ID = uuid.NewString()
	next.GenerationID = uuid.NewString()
	next.Options.AutoSummary = true
	frozen := processing.SourceRefreshSnapshot{Operation: processing.OperationSourceRefresh, Intent: next, ExpectedActiveSourceID: oldSource.ID, PreviousInputFingerprint: processing.SourceSummaryInputFingerprint(old)}
	out, err := repos.PrepareSourceRefresh(context.Background(), repository.PrepareSourceRefreshRequest{UserID: task.UserID, TaskID: task.ID, ExpectedSourceID: oldSource.ID, ExpectedIntentJSON: current.ProcessingIntentJSON, MediaFingerprint: task.FileMD5, Snapshot: frozen, Token: "new-fallback", Now: time.Now(), LeaseUntil: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	adapter.tracks = nil
	if err = c.handleTextSource(context.Background(), AnalyzePayload{TaskID: task.ID, MD5: task.FileMD5, ClaimToken: out.Token}); err != nil {
		t.Fatal(err)
	}
	asrJob, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeTranscribe)
	if asrJob.GenerationID != next.GenerationID || asrJob.InputSnapshotJSON != artifact.JSON(frozen) {
		t.Fatal("fallback lost accepted new source identity")
	}
	claim, err := c.claimTaskForMessage(task.ID, model.TaskJobTypeTranscribe, model.TaskStageTranscribing, asrJob.ProcessingToken)
	if err != nil || claim.Outcome != repository.TaskLeaseAcquired {
		t.Fatal(err)
	}
	current, _ = repos.Task.FindByID(task.ID)
	row := asrFixtureRow(0, "本次新的完整 ASR 文字。")
	row.TaskID = task.ID
	if err = db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	ctx := withProcessingLeaseOwner(context.Background(), &processingLeaseOwner{repos: repos, taskID: task.ID, jobType: model.TaskJobTypeTranscribe, token: claim.Token, now: c.currentTime})
	if err = c.publishAutomaticASRSource(ctx, current, next, claim.Token, row.Content, []model.VideoTranscriptionChunk{row}); err != nil {
		t.Fatal(err)
	}
	active, _ := repos.TextSource.Active(context.Background(), task.UserID, task.ID)
	summaryJob, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeSummary)
	if active.ID == oldSource.ID || summaryJob.GenerationID != next.GenerationID || summaryJob.InputSourceID != active.ID || producer.summaryCalls != 1 {
		t.Fatal("ASR source did not freeze a fresh summary generation")
	}
	read, err := repos.ReadSummaryGeneration(context.Background(), task.UserID, task.ID, "")
	if err != nil || read.Job == nil || read.Job.JobType != model.TaskJobTypeSummary || read.Job.Status != model.TaskStatusQueued || read.GenerationID != next.GenerationID || read.Run != nil {
		t.Fatalf("queued summary hidden by completed source operation: %+v %v", read, err)
	}
	if _, err = repos.TextSource.Read(context.Background(), task.UserID, task.ID, oldSource.ID); err != nil {
		t.Fatal("old source history lost")
	}
}

func TestSourceRefreshASRWindowsAreAttemptScopedAndRedeliveryReusesPaidWindows(t *testing.T) {
	c, repos, db, task, _, _, _, _ := sourceWorkerFixture(t, false, "force_asr")
	strategy := &recordingAI{transcripts: map[string]string{"window": "有实际识别结果"}}
	c.splitAudioWindows = func(context.Context, string, string, int, int) ([]ffmpeg.AudioSegment, string, error) {
		return []ffmpeg.AudioSegment{{Path: "window", WindowStartMS: 0, WindowEndMS: 10000, CoreStartMS: 0, CoreEndMS: 10000, SegmentKey: "actual-window", Version: ffmpeg.AudioSegmenterVersion}}, "", nil
	}
	ctx := context.Background()
	if _, err := c.transcription().transcribeAudio(ctx, task.ID, "fixture", strategy); err != nil {
		t.Fatal(err)
	}
	if _, err := c.transcription().transcribeAudio(ctx, task.ID, "fixture", strategy); err != nil {
		t.Fatal(err)
	}
	if len(strategy.transcribeInput) != 1 {
		t.Fatal("same generation repeated paid ASR")
	}
	current, _ := repos.Task.FindByID(task.ID)
	old, _ := processing.Decode(current.ProcessingIntentJSON)
	next := old
	next.GenerationID = uuid.NewString()
	next.ID = uuid.NewString()
	// Accept the explicit replacement through the real lease/intent transaction.
	if err := db.Model(&model.VideoTask{}).Where("id=?", task.ID).Updates(map[string]any{"status": model.TaskStatusFailed}).Error; err != nil {
		t.Fatal(err)
	}
	frozen := processing.SourceRefreshSnapshot{Operation: processing.OperationSourceRefresh, Intent: next, PreviousInputFingerprint: processing.SourceSummaryInputFingerprint(old)}
	if _, err := repos.PrepareSourceRefresh(ctx, repository.PrepareSourceRefreshRequest{UserID: task.UserID, TaskID: task.ID, ExpectedIntentJSON: current.ProcessingIntentJSON, MediaFingerprint: task.FileMD5, Snapshot: frozen, Token: "new-scoped-ASR", Now: time.Now(), LeaseUntil: time.Now().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.transcription().transcribeAudio(ctx, task.ID, "fixture", strategy); err != nil {
		t.Fatal(err)
	}
	if len(strategy.transcribeInput) != 2 {
		t.Fatal("new explicit ASR attempt reused old generation windows")
	}
	if _, err := c.transcription().transcribeAudio(ctx, task.ID, "fixture", strategy); err != nil {
		t.Fatal(err)
	}
	if len(strategy.transcribeInput) != 2 {
		t.Fatal("new scoped attempt redelivery repeated completed window")
	}
}

func TestSourceRefreshFailedASRCandidateKeepsPublishedTextAndSource(t *testing.T) {
	c, repos, db, task, payload, _, producer, _ := sourceWorkerFixture(t, false, "prefer_platform")
	ctx := context.Background()
	if err := c.handleTextSource(ctx, payload); err != nil {
		t.Fatal(err)
	}
	current, _ := repos.Task.FindByID(task.ID)
	source, _ := repos.TextSource.Active(ctx, task.UserID, task.ID)
	projection, _ := repos.Transcription.FindByTaskID(task.ID)
	beforeProjection, beforeSource := artifact.JSON(projection), artifact.JSON(source)
	base := model.AISummary{TaskID: task.ID, Content: "此前仍可读的摘要", SourceID: source.ID, SourceDigest: source.SourceDigest}
	if err := db.Create(&base).Error; err != nil {
		t.Fatal(err)
	}
	beforeBody := artifact.JSON(base)
	old, _ := processing.Decode(current.ProcessingIntentJSON)
	next := old
	next.ID, next.GenerationID = uuid.NewString(), uuid.NewString()
	next.Options.TextSourcePolicy = "force_asr"
	frozen := processing.SourceRefreshSnapshot{Operation: processing.OperationSourceRefresh, Intent: next, ExpectedActiveSourceID: source.ID, PreviousInputFingerprint: processing.SourceSummaryInputFingerprint(old)}
	out, err := repos.PrepareSourceRefresh(ctx, repository.PrepareSourceRefreshRequest{UserID: task.UserID, TaskID: task.ID, ExpectedSourceID: source.ID, ExpectedIntentJSON: current.ProcessingIntentJSON, MediaFingerprint: task.FileMD5, Snapshot: frozen, Token: "candidate-dispatch", Now: time.Now(), LeaseUntil: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	// A stale delivery must be rejected before even probing the platform.
	if err = c.handleTextSource(ctx, payload); err != nil && err != errStaleDispatch {
		t.Fatal(err)
	}
	claim, err := c.claimTaskForMessage(task.ID, model.TaskJobTypeTranscribe, model.TaskStageTranscribing, out.Token)
	if err != nil || claim.Outcome != repository.TaskLeaseAcquired {
		t.Fatal(err)
	}
	c.asrConcurrency = 1
	c.splitAudioWindows = func(context.Context, string, string, int, int) ([]ffmpeg.AudioSegment, string, error) {
		return []ffmpeg.AudioSegment{
			{Path: "first", SegmentKey: "first", Version: ffmpeg.AudioSegmenterVersion, WindowEndMS: 10000, CoreEndMS: 10000},
			{Path: "failed", SegmentKey: "second", Version: ffmpeg.AudioSegmenterVersion, WindowStartMS: 10000, WindowEndMS: 20000, CoreStartMS: 10000, CoreEndMS: 20000},
		}, "", nil
	}
	strategy := &recordingAI{transcripts: map[string]string{"first": "候选部分识别文字"}, transcribeErrors: map[string]error{"failed": ai.ErrRetryBudgetExhausted}}
	leaseCtx := withProcessingLeaseOwner(ctx, &processingLeaseOwner{repos: repos, taskID: task.ID, jobType: model.TaskJobTypeTranscribe, token: claim.Token, now: c.currentTime})
	if _, err = c.transcription().transcribeAudio(leaseCtx, task.ID, "fixture", strategy); err == nil {
		t.Fatal("incomplete candidate succeeded")
	}
	if err = c.recordTaskFailure(task.ID, model.TaskJobTypeTranscribe, model.TaskStageTranscribing, ai.ErrRetryBudgetExhausted, claim.Token); err != nil {
		t.Fatal(err)
	}
	projection, _ = repos.Transcription.FindByTaskID(task.ID)
	source, _ = repos.TextSource.Active(ctx, task.UserID, task.ID)
	after, _ := repos.Summary.FindByTaskID(task.ID)
	if artifact.JSON(projection) != beforeProjection || artifact.JSON(source) != beforeSource || artifact.JSON(after) != beforeBody || producer.summaryCalls != 0 {
		t.Fatal("failed candidate replaced readable text/source/body")
	}
}
