package service

import (
	"context"
	"errors"
	"testing"

	"vid-lens/internal/artifact"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
)

func disabledSummaryPolicy() config.SummaryExperienceConfig {
	disabled := false
	return config.SummaryExperienceConfig{V2GenerationEnabled: &disabled, VisualEnrichmentEnabled: &disabled}
}

func assertSummaryDisabled(t *testing.T, err error) {
	t.Helper()
	var typed *artifact.Error
	if !errors.As(err, &typed) || typed.Code != "summary_generation_disabled" || typed.Status != 503 {
		t.Fatalf("new-generation admission error: %v", err)
	}
}

func TestSummaryRolloutStopsNewImportButAllowsAcceptedReplayAndLegacy(t *testing.T) {
	svc, profiles, producer := importFixture(t)
	ctx := context.Background()
	url := "https://www.bilibili.com/video/BV1xx411c7mD?p=2"
	accepted, err := svc.UploadByURLWithOptions(ctx, 7, url, importOpts("before-disabled"))
	if err != nil {
		t.Fatal(err)
	}
	svc.WithSummaryExperience(disabledSummaryPolicy())
	calls := profiles.calls
	replay, err := svc.UploadByURLWithOptions(ctx, 7, url, importOpts("before-disabled"))
	if err != nil || replay.TaskID != accepted.TaskID || replay.GenerationID != accepted.GenerationID || len(producer.downloads) != 1 || profiles.calls != calls {
		t.Fatalf("accepted receipt blocked/re-dispatched: %+v %v", replay, err)
	}
	_, err = svc.UploadByURLWithOptions(ctx, 7, url, importOpts("after-disabled"))
	assertSummaryDisabled(t, err)
	_, err = svc.MergeChunksWithOptions(ctx, 7, "unused-media", "unused.mp4", 1, 1, 1, importOpts("local-after-disabled"))
	assertSummaryDisabled(t, err)
	if profiles.calls != calls || len(producer.downloads) != 1 || len(producer.sourceTasks) != 0 {
		t.Fatal("blocked new intent resolved a profile or dispatched work")
	}
	legacy, err := svc.UploadByURLWithOptions(ctx, 7, url, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.repo.Task.FindByID(legacy.TaskID)
	if err != nil || legacy.GenerationID != "" || task.ProcessingIntentJSON != "" || len(producer.downloads) != 2 {
		t.Fatal("legacy manual import was blocked or converted to new generation")
	}
}

func TestSummaryRolloutRejectsRegenerationWithoutChangingReadableBody(t *testing.T) {
	f, svc, profiles, producer, old := completedManualGenerationFixture(t)
	svc.WithSummaryExperience(disabledSummaryPolicy())
	before, _ := f.repos.TaskJob.FindByTaskAndType(f.task.ID, model.TaskJobTypeSummary)
	assertSummaryDisabled(t, svc.RequestAnalysis(context.Background(), f.task.UserID, f.task.ID, true))
	after, _ := f.repos.TaskJob.FindByTaskAndType(f.task.ID, model.TaskJobTypeSummary)
	if after.GenerationID != before.GenerationID || after.LeaseVersion != before.LeaseVersion || len(profiles.selected) != 0 || len(producer.analyzes) != 0 {
		t.Fatal("disabled regeneration mutated its existing job")
	}
	effective, err := f.repos.SummaryRevision.Effective(context.Background(), f.task.UserID, f.task.ID)
	if err != nil || effective.Content != old.Content || effective.Document == nil {
		t.Fatal("deployment switch changed existing v2 read")
	}
	if _, err := NewSummaryGenerationReadService(f.repos).Latest(context.Background(), f.task.UserID, f.task.ID); err != nil {
		t.Fatal("deployment switch blocked generation history")
	}
}

func TestSummaryRolloutAcceptedGenerationFinishesWithVisualDisabled(t *testing.T) {
	f := newGenerationFixture(t, false)
	f.options(t, func(options *processing.Options) { options.SummaryVisualEnabled = true })
	policy := disabledSummaryPolicy()
	admission := (&MediaService{repo: f.repos}).WithSummaryExperience(policy)
	assertSummaryDisabled(t, admission.requireV2GenerationAdmission())
	visualCalls := 0
	f.svc.WithVisualEnricher(WithSummaryVisualPolicy(generationFixtureVisual(func(context.Context, *model.AISummary) error {
		visualCalls++
		return nil
	}), policy))
	if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	view, err := NewSummaryGenerationReadService(f.repos).Latest(context.Background(), f.task.UserID, f.task.ID)
	if err != nil || view.TextState != "ready" || view.ResultState != "ready" || view.VisualState != "skipped" || view.FallbackReason != "visual_disabled" || view.ResolvedMode != "text" {
		t.Fatalf("visual policy lost truthful text fallback: %+v %v", view, err)
	}
	var run model.AgentRun
	if err := f.db.First(&run, "id = ?", f.job.GenerationID).Error; err != nil {
		t.Fatal(err)
	}
	if run.Status != model.AgentRunStatusCompleted || run.VisionCallsUsed != 0 || run.VisualCallsUsed != 0 || run.FramesUsed != 0 || visualCalls != 0 || len(f.chat.calls) != 1 {
		t.Fatalf("accepted generation did not finish without visual spending: %+v calls=%d", run, visualCalls)
	}
}
