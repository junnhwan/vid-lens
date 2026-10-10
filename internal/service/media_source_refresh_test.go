package service

import (
	"context"
	"testing"
	"time"
	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
	"vid-lens/internal/repository"
)

func sourceRefreshServiceFixture(t *testing.T) (*generationFixture, *MediaService, *visualRetryProfiles, *importProducer, *model.AISummary) {
	f, svc, profiles, _, old := completedManualGenerationFixture(t)
	p := &visualRetryProfiles{manualGenerationProfiles: *profiles}
	producer := &importProducer{}
	svc.profiles = p
	svc.mq = producer
	return f, svc, p, producer, old
}

func TestSourceRefreshAcceptsChangedProfileAndPreservesAllPublishedReadPaths(t *testing.T) {
	f, svc, profiles, producer, old := sourceRefreshServiceFixture(t)
	ctx := context.Background()
	profiles.profile.ASRProvider = "openai_compatible"
	profiles.profile.ASRBaseURL = "https://example.com/v1"
	profiles.profile.ASRModel = "fresh-asr"
	profiles.profile.ASRAPIKey = "private-fixture-key"
	row := model.VideoTranscriptionChunk{TaskID: f.task.ID, ChunkIndex: 0, Status: model.TranscriptionChunkStatusCompleted, Content: f.source.CanonicalText, SegmentKey: "old-window", WindowStartMS: 0, WindowEndMS: 10000}
	if err := f.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	beforeSource := artifact.JSON(f.source)
	beforeSummary := artifact.JSON(old)
	input := SourceRefreshRequest{ExpectedSourceID: &f.source.ID, TextSourcePolicy: "force_asr"}
	accepted, err := svc.RequestSourceRefresh(ctx, 7, f.task.ID, "new-source-profile", input)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := svc.RequestSourceRefresh(ctx, 7, f.task.ID, "new-source-profile", input)
	if err != nil || replay.GenerationID != accepted.GenerationID || len(producer.transcribes) != 1 {
		t.Fatal("idempotency requeued source refresh")
	}
	job, _ := f.repos.TaskJob.FindByTaskAndType(f.task.ID, model.TaskJobTypeTranscribe)
	var frozen processing.SourceRefreshSnapshot
	if err = artifact.Decode([]byte(job.InputSnapshotJSON), &frozen); err != nil {
		t.Fatal(err)
	}
	if frozen.Intent.GenerationID == old.GenerationID || frozen.Intent.ProfileFingerprint != processing.FingerprintProfile(profiles.profile) || frozen.Intent.Options.SummaryInstruction != "重点总结适用条件" || frozen.ASRCheckpointGenerationID != "" {
		t.Fatal("new source operation did not accept a new profile/identity")
	}
	var count int64
	f.db.Model(&model.VideoTranscriptionChunk{}).Where("task_id=?", f.task.ID).Count(&count)
	if count != 1 {
		t.Fatal("acceptance deleted paid old windows")
	}
	current, _ := f.repos.Summary.FindByTaskID(f.task.ID)
	source, _ := f.repos.TextSource.Active(ctx, 7, f.task.ID)
	if artifact.JSON(current) != beforeSummary || artifact.JSON(source) != beforeSource {
		t.Fatal("acceptance changed published source or summary")
	}
	detail, err := svc.GetTaskDetail(ctx, 7, f.task.ID)
	if err != nil || detail.Transcription == nil || detail.Transcription.Content != f.source.CanonicalText {
		t.Fatal("legacy transcript detail became empty during refresh")
	}
	jobToken := job.ProcessingToken
	claim, err := f.repos.ClaimTaskProcessing(repository.TaskProcessingClaimRequest{TaskID: f.task.ID, JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing, MessageToken: jobToken, NewToken: "failed-fresh-worker", Now: time.Now(), LeaseUntil: time.Now().Add(time.Minute)})
	if err != nil || claim.Outcome != repository.TaskLeaseAcquired {
		t.Fatal(err)
	}
	_, err = f.repos.FailTaskProcessing(repository.TaskProcessingFailureRequest{TaskID: f.task.ID, JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing, Token: claim.Token, Status: model.TaskStatusFailed, ErrorCode: "non_retryable_error", ErrorMessage: "fixture ASR failure", Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	detail, err = svc.GetTaskDetail(ctx, 7, f.task.ID)
	if err != nil || detail.Transcription == nil || detail.Transcription.Content != f.source.CanonicalText {
		t.Fatal("failed refresh lost the old transcript")
	}
	current, _ = f.repos.Summary.FindByTaskID(f.task.ID)
	if artifact.JSON(current) != beforeSummary {
		t.Fatal("failed refresh changed original")
	}
	view, err := NewSummaryGenerationReadService(f.repos).Latest(ctx, 7, f.task.ID)
	if err != nil || view.GenerationID != accepted.GenerationID || view.ResultGenerationID != old.GenerationID || view.Operation != processing.OperationSourceRefresh {
		t.Fatalf("latest hid accepted source generation: %+v %v", view, err)
	}
}

func TestV2ForceASRAndMissingCapabilityContinueAcceptNewFrozenIdentity(t *testing.T) {
	f, svc, profiles, producer, _ := sourceRefreshServiceFixture(t)
	ctx := context.Background()
	if err := svc.RequestTranscribe(WithSourceRefreshIdempotencyKey(ctx, "missing-capability"), 7, f.task.ID, true); err == nil {
		t.Fatal("missing ASR accepted forced source")
	}
	profiles.profile.ASRProvider = "openai_compatible"
	profiles.profile.ASRBaseURL = "https://example.com/v1"
	profiles.profile.ASRModel = "configured-later"
	profiles.profile.ASRAPIKey = "private-fixture-key"
	if err := svc.RequestTranscribe(WithSourceRefreshIdempotencyKey(ctx, "force-new-source"), 7, f.task.ID, true); err != nil {
		t.Fatal(err)
	}
	job, _ := f.repos.TaskJob.FindByTaskAndType(f.task.ID, model.TaskJobTypeTranscribe)
	if job.GenerationID == f.job.GenerationID || job.GenerationID == "" || len(producer.transcribes) != 1 {
		t.Fatal("force retained old completed generation")
	}
}

func TestSourceRefreshRejectsStaleOwnerAndReplaysBeforeProfileOrGate(t *testing.T) {
	f, svc, profiles, producer, _ := sourceRefreshServiceFixture(t)
	ctx := context.Background()
	input := SourceRefreshRequest{ExpectedSourceID: &f.source.ID, TextSourcePolicy: "prefer_platform"}
	if _, err := svc.RequestSourceRefresh(ctx, 8, f.task.ID, "foreign", input); err == nil {
		t.Fatal("foreign task accepted")
	}
	wrong := "stale-source"
	bad := input
	bad.ExpectedSourceID = &wrong
	if _, err := svc.RequestSourceRefresh(ctx, 7, f.task.ID, "stale", bad); err == nil {
		t.Fatal("stale source accepted")
	}
	accepted, err := svc.RequestSourceRefresh(ctx, 7, f.task.ID, "subtitle-only", input)
	if err != nil {
		t.Fatal(err)
	}
	// LLM-only profiles can refresh platform subtitles without ASR.
	if len(producer.sourceTasks) != 1 || len(producer.transcribes) != 0 {
		t.Fatal("prefer_platform required/dispatched ASR")
	}
	profiles.profile = ai.Profile{}
	svc.WithSummaryExperience(disabledSummaryPolicy())
	replay, err := svc.RequestSourceRefresh(ctx, 7, f.task.ID, "subtitle-only", input)
	if err != nil || replay.GenerationID != accepted.GenerationID || len(producer.sourceTasks) != 1 {
		t.Fatal("changed config/gate broke immutable replay")
	}
	bad.ProfileID = 123
	if _, err = svc.RequestSourceRefresh(ctx, 7, f.task.ID, "subtitle-only", bad); err == nil {
		t.Fatal("same-key request mismatch accepted")
	}
}

func TestSourceRefreshTranscribeReceiptSurvivesSourceChangeAndCompletedContinue(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(map[bool]string{false: "continue", true: "force"}[force], func(t *testing.T) {
			f, svc, profiles, producer, _ := sourceRefreshServiceFixture(t)
			profiles.profile.ASRProvider, profiles.profile.ASRBaseURL, profiles.profile.ASRModel, profiles.profile.ASRAPIKey = "openai_compatible", "https://example.com/v1", "new-ASR", "fixture-key"
			// Reproduce the failed no-subtitle/missing-ASR path with no active source.
			if err := f.db.Model(&model.VideoTask{}).Where("id=?", f.task.ID).Updates(map[string]any{"status": model.TaskStatusFailed, "last_job_type": model.TaskJobTypeTranscribe, "active_text_source_id": ""}).Error; err != nil {
				t.Fatal(err)
			}
			keyCtx := WithSourceRefreshIdempotencyKey(context.Background(), "stable-transcribe-request")
			if err := svc.RequestTranscribe(keyCtx, 7, f.task.ID, force); err != nil {
				t.Fatal(err)
			}
			job, _ := f.repos.TaskJob.FindByTaskAndType(f.task.ID, model.TaskJobTypeTranscribe)
			var snapshot processing.SourceRefreshSnapshot
			if err := artifact.Decode([]byte(job.InputSnapshotJSON), &snapshot); err != nil {
				t.Fatal(err)
			}
			if snapshot.Intent.ProfileFingerprint != processing.FingerprintProfile(profiles.profile) || snapshot.ASRCheckpointGenerationID != "" {
				t.Fatal("changed ASR config reused old frozen windows")
			}
			// A successful candidate can advance the pointer before a lost HTTP
			// response is replayed. Neither force nor continue may reaccept it.
			if err := f.db.Model(&model.VideoTask{}).Where("id=?", f.task.ID).Updates(map[string]any{"status": model.TaskStatusCompleted, "active_text_source_id": f.source.ID}).Error; err != nil {
				t.Fatal(err)
			}
			profiles.profile = ai.Profile{}
			svc.WithSummaryExperience(disabledSummaryPolicy())
			if err := svc.RequestTranscribe(keyCtx, 7, f.task.ID, force); err != nil {
				t.Fatal(err)
			}
			if len(producer.transcribes) != 1 {
				t.Fatal("replay repeated ASR dispatch")
			}
			if err := svc.RequestTranscribe(keyCtx, 7, f.task.ID, !force); err == nil {
				t.Fatal("same key changed force/resume intent")
			}
		})
	}
}

func TestSourceRefreshReadWithoutSummaryRunReturnsEmptyRecoverableEvents(t *testing.T) {
	for _, auto := range []bool{true, false} {
		t.Run(map[bool]string{true: "unchanged", false: "source-only"}[auto], func(t *testing.T) {
			f, svc, _, _, old := sourceRefreshServiceFixture(t)
			ctx := context.Background()
			accepted, err := svc.RequestSourceRefresh(ctx, 7, f.task.ID, "no-summary-run", SourceRefreshRequest{ExpectedSourceID: &f.source.ID, TextSourcePolicy: "prefer_platform", AutoSummary: &auto})
			if err != nil {
				t.Fatal(err)
			}
			job, _ := f.repos.TaskJob.FindByTaskAndType(f.task.ID, model.TaskJobTypeTextSource)
			claim, err := f.repos.ClaimTaskProcessing(repository.TaskProcessingClaimRequest{TaskID: f.task.ID, JobType: model.TaskJobTypeTextSource, Stage: model.TaskStageTextSource, MessageToken: job.ProcessingToken, NewToken: "source-reader-worker", Now: time.Now(), LeaseUntil: time.Now().Add(time.Minute)})
			if err != nil || claim.Outcome != repository.TaskLeaseAcquired {
				t.Fatalf("claim %+v %v", claim, err)
			}
			if ok, err := f.repos.CompleteTaskProcessing(repository.TaskProcessingCompleteRequest{TaskID: f.task.ID, JobType: model.TaskJobTypeTextSource, JobStage: model.TaskStageTextSource, Token: claim.Token, TaskStatus: model.TaskStatusCompleted, TaskStage: model.TaskStageNone, Now: time.Now()}); err != nil || !ok {
				t.Fatal(err)
			}
			if auto {
				var frozen processing.SourceRefreshSnapshot
				artifact.Decode([]byte(job.InputSnapshotJSON), &frozen)
				frozen.ClassificationChanged = true
				if err := f.db.Model(job).Update("input_snapshot_json", artifact.JSON(frozen)).Error; err != nil {
					t.Fatal(err)
				}
				if err := f.repos.RecordUnchangedSourceRefresh(ctx, f.task.ID, model.TaskJobTypeTextSource, accepted.GenerationID); err != nil {
					t.Fatal(err)
				}
			}
			current, _ := f.repos.Task.FindByID(f.task.ID)
			if !auto {
				current = publishReadFixture(t, f.repos, current, 25)
			}
			reader := NewSummaryGenerationReadService(f.repos)
			view, err := reader.Latest(ctx, 7, f.task.ID)
			if err != nil || view.GenerationID != accepted.GenerationID || view.ResultGenerationID != old.GenerationID || view.Operation != processing.OperationSourceRefresh || view.Status != "completed" || view.ClassificationState != "not_reprocessed" || len(view.Activities) != 0 || view.EventHighWatermark != 0 || view.ResultState != "ready" {
				t.Fatalf("view=%+v %v", view, err)
			}
			if auto && (view.TextState != "ready" || view.FallbackReason != "source_unchanged") {
				t.Fatal("retained body mislabeled")
			}
			if !auto && (view.TextState != "not_requested" || view.StopReason != "auto_summary_disabled") {
				t.Fatal("source-only operation fabricated text generation")
			}
			if view.Source == nil || view.Source.ID != current.ActiveTextSourceID || !auto && view.SourceStatus != "source_changed" {
				t.Fatalf("completed source operation exposed old source metadata: %+v", view)
			}
			for i := 0; i < 2; i++ {
				page, err := reader.Events(ctx, 7, f.task.ID, accepted.GenerationID, 0, 100)
				if err != nil || len(page.Events) != 0 || page.HighWatermark != 0 || page.HasMore || page.CursorGap {
					t.Fatalf("empty recovery failed %+v %v", page, err)
				}
			}
			if _, err = reader.Events(ctx, 8, f.task.ID, accepted.GenerationID, 0, 100); err == nil {
				t.Fatal("empty event access crossed owner")
			}
		})
	}
}

func TestSourceRefreshRepeatedCompatibleContinueRetainsOriginalASRCheckpointNamespace(t *testing.T) {
	for _, scoped := range []bool{false, true} {
		t.Run(map[bool]string{false: "initial-window-format", true: "generation-scoped-format"}[scoped], func(t *testing.T) {
			f, svc, profiles, producer, _ := sourceRefreshServiceFixture(t)
			ctx := context.Background()
			profiles.profile.ASRProvider, profiles.profile.ASRBaseURL, profiles.profile.ASRModel, profiles.profile.ASRAPIKey = "openai_compatible", "https://example.com/v1", "same-asr", "fixture-key"
			current, _ := f.repos.Task.FindByID(f.task.ID)
			old, _ := processing.Decode(current.ProcessingIntentJSON)
			old.ProfileFingerprint = processing.FingerprintProfile(profiles.profile)
			old.Options.TextSourcePolicy = "force_asr"
			if err := f.db.Model(current).Updates(map[string]any{"processing_intent_json": artifact.JSON(old), "status": model.TaskStatusFailed, "last_job_type": model.TaskJobTypeTranscribe}).Error; err != nil {
				t.Fatal(err)
			}
			previous := model.TaskJob{TaskID: f.task.ID, UserID: 7, JobType: model.TaskJobTypeTranscribe, Status: model.TaskStatusFailed, GenerationID: old.GenerationID}
			if scoped {
				previous.InputSnapshotJSON = artifact.JSON(processing.SourceRefreshSnapshot{Operation: processing.OperationSourceRefresh, Intent: old, ExpectedActiveSourceID: f.source.ID})
			}
			if err := f.db.Create(&previous).Error; err != nil {
				t.Fatal(err)
			}
			for index, key := range []string{"continue-one", "continue-two"} {
				if err := svc.RequestTranscribe(WithSourceRefreshIdempotencyKey(ctx, key), 7, f.task.ID, false); err != nil {
					t.Fatal(err)
				}
				job, _ := f.repos.TaskJob.FindByTaskAndType(f.task.ID, model.TaskJobTypeTranscribe)
				var frozen processing.SourceRefreshSnapshot
				artifact.Decode([]byte(job.InputSnapshotJSON), &frozen)
				if frozen.ReuseUnscopedASRWindows == scoped || scoped && frozen.ASRCheckpointGenerationID != old.GenerationID || !scoped && frozen.ASRCheckpointGenerationID != "" {
					t.Fatalf("namespace changed: %+v", frozen)
				}
				if len(producer.transcribes) != index+1 {
					t.Fatal("unexpected continue dispatch")
				}
				claim, err := f.repos.ClaimTaskProcessing(repository.TaskProcessingClaimRequest{TaskID: f.task.ID, JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing, MessageToken: job.ProcessingToken, NewToken: key + "-worker", Now: time.Now(), LeaseUntil: time.Now().Add(time.Minute)})
				if err != nil || claim.Outcome != repository.TaskLeaseAcquired {
					t.Fatal(err)
				}
				if _, err = f.repos.FailTaskProcessing(repository.TaskProcessingFailureRequest{TaskID: f.task.ID, JobType: model.TaskJobTypeTranscribe, Token: claim.Token, Status: model.TaskStatusFailed, ErrorCode: "fixture-failure", Now: time.Now()}); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestSourceRefreshDispatchFailureKeepsOneAcceptedFrozenOperation(t *testing.T) {
	f, svc, profiles, producer, old := sourceRefreshServiceFixture(t)
	ctx := context.Background()
	producer.sourceErr = ErrTaskDispatchUnavailable
	input := SourceRefreshRequest{ExpectedSourceID: &f.source.ID, TextSourcePolicy: "prefer_platform"}
	before := time.Now()
	if _, err := svc.RequestSourceRefresh(ctx, 7, f.task.ID, "dispatch-failed-refresh", input); err == nil {
		t.Fatal("queue failure hidden")
	}
	job, _ := f.repos.TaskJob.FindByTaskAndType(f.task.ID, model.TaskJobTypeTextSource)
	if job.Status != model.TaskStatusFailed || job.NextRetryAt == nil || job.NextRetryAt.Before(before.Add(50*time.Second)) || job.ProcessingToken != "" || job.GenerationID == "" || job.InputSnapshotJSON == "" {
		t.Fatalf("missing recovery backoff %+v", job)
	}
	generation, snapshot := job.GenerationID, job.InputSnapshotJSON
	calls := profiles.calls
	replay, err := svc.RequestSourceRefresh(ctx, 7, f.task.ID, "dispatch-failed-refresh", input)
	if err != nil || replay.GenerationID != generation || len(producer.sourceTasks) != 1 || profiles.calls != calls {
		t.Fatal("HTTP retry created another source operation")
	}
	now := job.NextRetryAt.Add(time.Second)
	owned, err := f.repos.ClaimRetryDispatch(repository.TaskDispatchClaimRequest{TaskID: f.task.ID, JobType: model.TaskJobTypeTextSource, Stage: model.TaskStageTextSource, Token: "source-retry-scheduler", ExpectedVersion: job.LeaseVersion, Now: now, LeaseUntil: now.Add(time.Minute)})
	if err != nil || !owned {
		t.Fatalf("redispatch %+v %v", owned, err)
	}
	job, _ = f.repos.TaskJob.FindByTaskAndType(f.task.ID, model.TaskJobTypeTextSource)
	body, _ := f.repos.Summary.FindByTaskID(f.task.ID)
	if job.GenerationID != generation || job.InputSnapshotJSON != snapshot || artifact.JSON(body) != artifact.JSON(old) {
		t.Fatal("scheduler replaced frozen operation or old body")
	}
}

type sourceRefreshConcurrentProfiles struct {
	*visualRetryProfiles
	acceptedPeer func()
}

func (p *sourceRefreshConcurrentProfiles) GetConversationProfile(owner, id int64) (*ResolvedConversationProfile, error) {
	if p.acceptedPeer != nil {
		callback := p.acceptedPeer
		p.acceptedPeer = nil
		callback()
		return nil, ErrAIProfileRequired
	}
	return p.visualRetryProfiles.GetConversationProfile(owner, id)
}
func TestSourceRefreshSameKeyPeerWinsLatePreflightFailure(t *testing.T) {
	f, svc, profiles, producer, _ := sourceRefreshServiceFixture(t)
	input := SourceRefreshRequest{ExpectedSourceID: &f.source.ID, TextSourcePolicy: "prefer_platform"}
	peer := *svc
	var winner *SourceRefreshResult
	svc.profiles = &sourceRefreshConcurrentProfiles{visualRetryProfiles: profiles, acceptedPeer: func() {
		var err error
		winner, err = peer.RequestSourceRefresh(context.Background(), 7, f.task.ID, "same-key-peer", input)
		if err != nil {
			t.Fatal(err)
		}
	}}
	result, err := svc.RequestSourceRefresh(context.Background(), 7, f.task.ID, "same-key-peer", input)
	if err != nil || result.GenerationID != winner.GenerationID || len(producer.sourceTasks) != 1 {
		t.Fatalf("late peer receipt lost %+v %v", result, err)
	}
}
