package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
	"vid-lens/internal/repository"
)

func TestSummaryGenerationReadOwnerTaskScopeAndOrderedCursorRecovery(t *testing.T) {
	f := newGenerationFixture(t, false)
	ctx := context.Background()
	reader := NewSummaryGenerationReadService(f.repos)
	queued, err := reader.Latest(ctx, 7, f.task.ID)
	if err != nil || queued.GenerationID != f.job.GenerationID || len(queued.Activities) != 0 || queued.EventHighWatermark != 0 {
		t.Fatalf("queued snapshot=%+v %v", queued, err)
	}
	if err = f.svc.Generate(ctx, f.task, f.job, f.job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	view, err := reader.Latest(ctx, 7, f.task.ID)
	if err != nil || view.Status != "completed" || view.TextState != "ready" || view.ResultState != "ready" || view.Source == nil || view.Source.ID != f.source.ID || len(view.Activities) != 2 {
		t.Fatalf("view=%+v %v", view, err)
	}
	for _, activity := range view.Activities {
		if activity.Title == "" || activity.State != "done" || activity.StartedAt.IsZero() || activity.FinishedAt == nil {
			t.Fatalf("activity=%+v", activity)
		}
	}
	encoded, _ := json.Marshal(view)
	for _, private := range []string{"fixture-secret", "profile_snapshot", "policy_snapshot", "raw_object_key", "result_checkpoint", "canonical_text", "processing_token"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("exposed %s", private)
		}
	}
	after := int64(0)
	seen := int64(0)
	for {
		page, err := reader.Events(ctx, 7, f.task.ID, f.job.GenerationID, after, 2)
		if err != nil {
			t.Fatal(err)
		}
		if page.CursorGap || len(page.Events) > 2 || page.HighWatermark != view.EventHighWatermark {
			t.Fatal("cursor changed or gap invented")
		}
		for _, event := range page.Events {
			seen++
			if event.Seq != seen {
				t.Fatal("events reordered")
			}
		}
		if page.NextAfterSeq < after {
			t.Fatal("cursor regressed")
		}
		after = page.NextAfterSeq
		if !page.HasMore {
			break
		}
	}
	if seen != view.EventHighWatermark {
		t.Fatal("lost completion events")
	}
	empty, err := reader.Events(ctx, 7, f.task.ID, f.job.GenerationID, after, 2)
	if err != nil || len(empty.Events) != 0 || empty.HasMore {
		t.Fatal("caught-up cursor failed")
	}
	if _, err = reader.Events(ctx, 7, f.task.ID, f.job.GenerationID, after+1, 2); err == nil {
		t.Fatal("ahead cursor accepted")
	}
	if _, err = reader.Latest(ctx, 8, f.task.ID); err == nil {
		t.Fatal("foreign owner snapshot")
	}
	if _, err = reader.Events(ctx, 8, f.task.ID, f.job.GenerationID, 0, 2); err == nil {
		t.Fatal("foreign owner events")
	}
	if _, err = reader.Events(ctx, 7, f.task.ID+1, f.job.GenerationID, 0, 2); err == nil {
		t.Fatal("wrong task events")
	}
	if err = f.db.Where("run_id=? AND seq=?", f.job.GenerationID, 2).Delete(&model.RunEvent{}).Error; err != nil {
		t.Fatal(err)
	}
	page, err := reader.Events(ctx, 7, f.task.ID, f.job.GenerationID, 1, 100)
	if err != nil || !page.CursorGap {
		t.Fatalf("missing cursor gap: %+v %v", page, err)
	}
	if err = f.db.Delete(f.task).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = reader.Events(ctx, 7, f.task.ID, f.job.GenerationID, 0, 2); err == nil {
		t.Fatal("deleted task retained event access")
	}
}
func TestSummaryGenerationReadRefreshCancelsBegunActivities(t *testing.T) {
	f := newGenerationFixture(t, false)
	f.chat.afterCall = func() { f.task = publishReadFixture(t, f.repos, f.task, 15) }
	if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err == nil {
		t.Fatal("refresh did not cancel")
	}
	view, err := NewSummaryGenerationReadService(f.repos).Latest(context.Background(), 7, f.task.ID)
	if err != nil || view.Status != "cancelled" || view.TextState != "cancelled" || len(view.Activities) != 1 || view.Activities[0].State != "cancelled" || view.Activities[0].FinishedAt == nil {
		t.Fatalf("cancelled snapshot=%+v %v", view, err)
	}
}
func TestSummaryGenerationQueueFailureIsAtomicAndRetryableCheckpointsRemainLive(t *testing.T) {
	for _, retry := range []bool{true, false} {
		t.Run(map[bool]string{true: "retry", false: "terminal"}[retry], func(t *testing.T) {
			f := newGenerationFixture(t, false)
			f.chat.failAt = 1
			if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err == nil {
				t.Fatal("no fixture failure")
			}
			req := repository.TaskProcessingFailureRequest{TaskID: f.task.ID, JobType: model.TaskJobTypeSummary, Token: f.job.ProcessingToken, Status: model.TaskStatusFailed, ErrorCode: "private-error-detail", ErrorMessage: "fixture-secret", RetryCount: 1, MaxRetries: 3, Now: time.Now().UTC()}
			if retry {
				next := time.Now().Add(time.Minute)
				req.NextRetryAt = &next
			}
			owned, err := f.repos.FailTaskProcessing(req)
			if err != nil || !owned {
				t.Fatalf("queue failure=%t %v", owned, err)
			}
			view, err := NewSummaryGenerationReadService(f.repos).Latest(context.Background(), 7, f.task.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := "failed"
			if retry {
				want = "retry_waiting"
			}
			if view.Status != want {
				t.Fatalf("view=%+v", view)
			}
			page, err := NewSummaryGenerationReadService(f.repos).Events(context.Background(), 7, f.task.ID, f.job.GenerationID, 0, 100)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(page)
			if strings.Contains(string(raw), "fixture-secret") || strings.Contains(string(raw), "private-error-detail") {
				t.Fatal("raw queue error exposed")
			}
			store := repository.NewSummaryGenerationExecutionStore(f.repos, 7, f.task.ID, f.job.GenerationID)
			run, _ := store.GetRun(context.Background(), 7, f.job.GenerationID)
			if retry && run.FinishedAt != nil {
				t.Fatal("retry terminalized run")
			}
			if !retry && run.FinishedAt == nil {
				t.Fatal("terminal job left live run")
			}
		})
	}
}
func TestSummaryGenerationReadLegacyHasOnlyCoarseFacts(t *testing.T) {
	f := newGenerationFixture(t, false)
	if err := f.db.Model(f.task).Updates(map[string]any{"processing_intent_json": "", "active_text_source_id": ""}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Delete(f.job).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Create(&model.AISummary{TaskID: f.task.ID, FileMD5: f.task.FileMD5, Content: "已有文字摘要"}).Error; err != nil {
		t.Fatal(err)
	}
	view, err := NewSummaryGenerationReadService(f.repos).Latest(context.Background(), 7, f.task.ID)
	if err != nil || !view.Legacy || view.GenerationID != "" || view.TextState != "ready" || view.ResultState != "ready" || len(view.Activities) != 0 {
		t.Fatalf("legacy=%+v %v", view, err)
	}
	if _, err = NewSummaryGenerationReadService(f.repos).Events(context.Background(), 7, f.task.ID, "missing-generation", 0, 1); err == nil {
		t.Fatal("legacy fabricated generation")
	}
}
func TestSummaryGenerationPublicEventDropsUnknownDataAndUnsafeActivity(t *testing.T) {
	raw := artifact.JSON(map[string]any{"title": "https://private/provider?api_key=secret", "detail": "sk-secret", "profile_snapshot": map[string]any{"api_key": "secret"}, "status": "running", "activity_id": "started", "arguments": "private"})
	data := publicSummaryGenerationEventData(raw)
	if data["status"] != "running" || data["activity_id"] != "started" || len(data) != 2 {
		t.Fatalf("unsafe projection=%+v", data)
	}
}

func TestSummaryGenerationReadMindmapUsesFrozenIntentAndLegacyStaysUnset(t *testing.T) {
	f := newGenerationFixture(t, false)
	f.options(t, func(options *processing.Options) { options.MindmapEnabled = true })
	var current processing.Intent
	if err := json.Unmarshal([]byte(f.task.ProcessingIntentJSON), &current); err != nil {
		t.Fatal(err)
	}
	current.Options.MindmapEnabled = false
	if err := f.db.Model(f.task).Update("processing_intent_json", artifact.JSON(current)).Error; err != nil {
		t.Fatal(err)
	}
	view, err := NewSummaryGenerationReadService(f.repos).Latest(context.Background(), 7, f.task.ID)
	if err != nil || view.MindmapEnabled == nil || !*view.MindmapEnabled {
		t.Fatalf("frozen display option changed %+v %v", view, err)
	}
	if err = f.db.Model(f.task).Updates(map[string]any{"processing_intent_json": "", "active_text_source_id": ""}).Error; err != nil {
		t.Fatal(err)
	}
	if err = f.db.Delete(f.job).Error; err != nil {
		t.Fatal(err)
	}
	legacy, err := NewSummaryGenerationReadService(f.repos).Latest(context.Background(), 7, f.task.ID)
	if err != nil || !legacy.Legacy || legacy.MindmapEnabled != nil {
		t.Fatal("legacy invented an explicit display setting")
	}
}
