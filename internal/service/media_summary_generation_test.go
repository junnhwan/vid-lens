package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
	"vid-lens/internal/repository"
)

type manualGenerationProfiles struct {
	importProfileFixture
	defaultCalls int
	selected     []int64
}

func (p *manualGenerationProfiles) GetDefaultAIProfile(int64) (*ai.Profile, error) {
	p.defaultCalls++
	return nil, ErrAIProfileRequired
}
func (p *manualGenerationProfiles) GetConversationProfile(owner, id int64) (*ResolvedConversationProfile, error) {
	p.selected = append(p.selected, id)
	return p.importProfileFixture.GetConversationProfile(owner, id)
}
func completedManualGenerationFixture(t *testing.T) (*generationFixture, *MediaService, *manualGenerationProfiles, *recordingMediaProducer, *model.AISummary) {
	t.Helper()
	f := newGenerationFixture(t, false)
	f.options(t, func(o *processing.Options) {
		o.SummaryInstruction = "重点总结适用条件"
		o.AutoTagsEnabled = false
		o.MindmapEnabled = true
	})
	if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	owned, err := f.repos.CompleteTaskProcessing(repository.TaskProcessingCompleteRequest{TaskID: f.task.ID, JobType: model.TaskJobTypeSummary, JobStage: model.TaskStageSummarizing, Token: f.job.ProcessingToken, Now: time.Now()})
	if err != nil || !owned {
		t.Fatalf("complete initial job=%t %v", owned, err)
	}
	summary, _ := f.repos.Summary.FindByTaskID(f.task.ID)
	profiles := &manualGenerationProfiles{importProfileFixture: importProfileFixture{profile: f.profiles.profile}}
	producer := &recordingMediaProducer{}
	return f, &MediaService{repo: f.repos, profiles: profiles, mq: producer}, profiles, producer, summary
}
func TestManualSummaryGenerationFreezesNewJobAndKeepsPreviousReadableResult(t *testing.T) {
	f, svc, profiles, producer, old := completedManualGenerationFixture(t)
	profiles.profile.LLMModel = "new-explicit-profile-config"
	revision := model.SummaryRevision{ID: "kept-user-revision", UserID: 7, TaskID: f.task.ID, Version: 1, Content: "用户改好的旧摘要", ContentHashKind: model.SummaryHashMarkdown, BaseGeneratedHash: old.ContentDigest, BaseGeneratedHashKind: old.ContentHashKind, Origin: "manual"}
	if err := f.db.Create(&revision).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Create(&model.SummaryRevisionHead{UserID: 7, TaskID: f.task.ID, Version: 1, CurrentRevisionID: revision.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RequestAnalysis(context.Background(), 7, f.task.ID, true); err != nil {
		t.Fatal(err)
	}
	job, err := f.repos.TaskJob.FindByTaskAndType(f.task.ID, model.TaskJobTypeSummary)
	if err != nil {
		t.Fatal(err)
	}
	var frozen processing.GenerationSnapshot
	if err = json.Unmarshal([]byte(job.InputSnapshotJSON), &frozen); err != nil {
		t.Fatal(err)
	}
	if job.GenerationID == f.job.GenerationID || job.GenerationID == "" || job.InputSourceID != f.source.ID || job.InputText != f.source.CanonicalText || frozen.Intent.GenerationID != job.GenerationID || frozen.SourceDigest != f.source.SourceDigest {
		t.Fatalf("new job not frozen %+v %+v", job, frozen)
	}
	if frozen.ExpectedGeneratedVersion != old.GeneratedVersion || frozen.ExpectedGeneratedHash != old.ContentDigest || frozen.ExpectedGeneratedHashKind != old.ContentHashKind {
		t.Fatal("generated base CAS missing")
	}
	if frozen.Intent.Options.SummaryInstruction != "重点总结适用条件" || !frozen.Intent.Options.MindmapEnabled || frozen.Intent.Options.AutoTagsEnabled || frozen.Intent.ProfileID != 19 || frozen.Intent.ProfileFingerprint != processing.FingerprintProfile(profiles.profile) {
		t.Fatalf("user settings/profile lost %+v", frozen.Intent)
	}
	if len(profiles.selected) != 1 || profiles.selected[0] != 19 || profiles.defaultCalls != 0 || len(producer.analyzes) != 1 {
		t.Fatal("regeneration switched default or duplicated publish")
	}
	effective, err := f.repos.SummaryRevision.Effective(context.Background(), 7, f.task.ID)
	if err != nil || effective.Content != revision.Content || effective.Generated.GenerationID != old.GenerationID {
		t.Fatal("regeneration deleted readable body/user head")
	}
	current, _ := f.repos.Task.FindByID(f.task.ID)
	intent, err := processing.Decode(current.ProcessingIntentJSON)
	if err != nil || intent.GenerationID != job.GenerationID {
		t.Fatal("accepted generation not linked atomically to task")
	}
	if err := svc.RequestAnalysis(context.Background(), 7, f.task.ID, true); err == nil {
		t.Fatal("double dispatch accepted")
	}
	if len(producer.analyzes) != 1 || len(profiles.selected) != 1 {
		t.Fatal("duplicate performed new profile resolution/publish")
	}
	lease := repository.SummaryGenerationLease{UserID: 7, TaskID: f.task.ID, GenerationID: f.job.GenerationID, SourceID: f.source.ID, SourceDigest: f.source.SourceDigest, LeaseToken: f.job.ProcessingToken}
	if err = f.repos.WithSummaryGenerationLease(context.Background(), lease, func(*repository.Repositories) error { return nil }); err == nil {
		t.Fatal("old generation worker retained publication rights")
	}
	view, err := NewSummaryGenerationReadService(f.repos).Latest(context.Background(), 7, f.task.ID)
	if err != nil || view.GenerationID != job.GenerationID || view.ResultGenerationID != old.GenerationID || view.TextState != "pending" || view.ResultState != "ready" {
		t.Fatalf("queued new run mislabeled prior result %+v %v", view, err)
	}
}
func TestManualSummaryGenerationPublishFailurePreservesFrozenRetryWithBackoff(t *testing.T) {
	f, svc, profiles, producer, old := completedManualGenerationFixture(t)
	producer.analyzeErr = ErrTaskDispatchUnavailable
	before := time.Now()
	if err := svc.RequestAnalysis(context.Background(), 7, f.task.ID, true); err != ErrTaskDispatchUnavailable {
		t.Fatalf("publish=%v", err)
	}
	job, _ := f.repos.TaskJob.FindByTaskAndType(f.task.ID, model.TaskJobTypeSummary)
	if job.Status != model.TaskStatusFailed || job.NextRetryAt == nil || job.NextRetryAt.Before(before.Add(50*time.Second)) || job.ProcessingToken != "" || job.LeaseKind != "" || job.GenerationID == "" || job.InputSnapshotJSON == "" {
		t.Fatalf("no durable backoff %+v", job)
	}
	frozen := job.InputSnapshotJSON
	gen := job.GenerationID
	if err := svc.RequestAnalysis(context.Background(), 7, f.task.ID, true); err == nil {
		t.Fatal("manual call bypassed existing retry backoff")
	}
	if len(producer.analyzes) != 1 || len(profiles.selected) != 1 {
		t.Fatal("retry duplicated acceptance")
	}
	due, err := f.repos.TaskJob.DueSummaryTasks(job.NextRetryAt.Add(time.Second), 10)
	if err != nil || len(due) != 1 {
		t.Fatal("failed dispatch invisible to scheduler")
	}
	now := job.NextRetryAt.Add(time.Second)
	owned, err := f.repos.ClaimRetryDispatch(repository.TaskDispatchClaimRequest{TaskID: f.task.ID, JobType: model.TaskJobTypeSummary, Stage: model.TaskStageSummarizing, Token: "retry-summary-dispatch", ExpectedVersion: job.LeaseVersion, Now: now, LeaseUntil: now.Add(time.Minute)})
	if err != nil || !owned {
		t.Fatalf("retry claim=%t %v", owned, err)
	}
	job, _ = f.repos.TaskJob.FindByTaskAndType(f.task.ID, model.TaskJobTypeSummary)
	if job.GenerationID != gen || job.InputSnapshotJSON != frozen {
		t.Fatal("retry replaced frozen generation")
	}
	kept, _ := f.repos.Summary.FindByTaskID(f.task.ID)
	if kept.ContentDigest != old.ContentDigest {
		t.Fatal("publish failure deleted old result")
	}
}
func TestManualSummaryGenerationNoSourceCannotConsumeStaleLegacyText(t *testing.T) {
	f, svc, _, producer, _ := completedManualGenerationFixture(t)
	if err := f.db.Model(f.task).Update("active_text_source_id", "").Error; err != nil {
		t.Fatal(err)
	}
	err := svc.RequestAnalysis(context.Background(), 7, f.task.ID, true)
	assertImportStatus(t, err, 422)
	if len(producer.analyzes) != 0 {
		t.Fatal("new intent used stale ASR projection")
	}
}

func TestAutoImportReplayReturnsOriginalGenerationAfterExplicitRegeneration(t *testing.T) {
	f, svc, _, _, _ := completedManualGenerationFixture(t)
	original, err := processing.Decode(f.task.ProcessingIntentJSON)
	if err != nil {
		t.Fatal(err)
	}
	options := ImportOptions{Options: original.Options, IdempotencyKey: "stable-original-generation"}
	prepared, _, err := svc.lookupImport(context.Background(), 7, "url", "stable-url", options, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, created, err := f.repos.AcceptImport(context.Background(), 7, "url", prepared.key, prepared.hash, func(*repository.Repositories) (*model.VideoTask, error) { return f.task, nil }); err != nil || !created {
		t.Fatalf("receipt=%t %v", created, err)
	}
	if err = svc.RequestAnalysis(context.Background(), 7, f.task.ID, true); err != nil {
		t.Fatal(err)
	}
	_, replay, err := svc.lookupImport(context.Background(), 7, "url", "stable-url", options, false)
	if err != nil || replay == nil || replay.GenerationID != original.GenerationID || replay.TaskID != f.task.ID {
		t.Fatalf("original replay=%+v %v", replay, err)
	}
	current, _ := f.repos.Task.FindByID(f.task.ID)
	intent, _ := processing.Decode(current.ProcessingIntentJSON)
	if intent.GenerationID == original.GenerationID {
		t.Fatal("regeneration fixture did not change workflow identity")
	}
}
