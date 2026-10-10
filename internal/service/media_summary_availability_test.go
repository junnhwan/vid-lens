package service

import (
	"context"
	"testing"

	"vid-lens/internal/model"
)

func TestSummaryAvailabilityActiveSubtitleWithoutASR(t *testing.T) {
	f := newGenerationFixture(t, false)
	// The published subtitle snapshot is the authority even when no ASR or
	// compatibility transcript projection is present.
	if err := f.db.Where("task_id = ?", f.task.ID).Delete(&model.VideoTranscription{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Where("task_id = ?", f.task.ID).Delete(&model.TaskJob{}).Error; err != nil {
		t.Fatal(err)
	}
	svc := &MediaService{repo: f.repos}
	detail, err := svc.GetTaskDetail(context.Background(), f.task.UserID, f.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.ActiveTextSourceID != f.source.ID || detail.HasTranscription || !detail.CanSummarize {
		t.Fatalf("subtitle-only detail availability: source=%q transcription=%t can_summarize=%t", detail.ActiveTextSourceID, detail.HasTranscription, detail.CanSummarize)
	}
	listed, _, err := svc.ListTasks(f.task.UserID, 1, 10, "")
	if err != nil || len(listed) != 1 || listed[0].HasTranscription || !listed[0].CanSummarize {
		t.Fatalf("subtitle-only list availability: %+v %v", listed, err)
	}
}

func TestSummaryAvailabilityKeepsPrivatePublishedBodyDuringRegeneration(t *testing.T) {
	f, svc, _, _, old := completedManualGenerationFixture(t)
	if err := svc.RequestAnalysis(context.Background(), f.task.UserID, f.task.ID, true); err != nil {
		t.Fatal(err)
	}
	for _, status := range []int8{model.TaskStatusQueued, model.TaskStatusRunning} {
		if err := f.db.Model(&model.TaskJob{}).Where("task_id = ? AND job_type = ?", f.task.ID, model.TaskJobTypeSummary).Update("status", status).Error; err != nil {
			t.Fatal(err)
		}
		detail, err := svc.GetTaskDetail(context.Background(), f.task.UserID, f.task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !detail.HasSummary || detail.Summary == nil || detail.Summary.Content != old.Content || detail.Summary.GenerationID != old.GenerationID || detail.CanSummarize {
			t.Fatalf("private summary hidden or mislabeled at status %d: has_summary=%t summary=%+v can_summarize=%t", status, detail.HasSummary, detail.Summary, detail.CanSummarize)
		}
		listed, _, err := svc.ListTasks(f.task.UserID, 1, 10, "")
		if err != nil || len(listed) != 1 || !listed[0].HasSummary || listed[0].CanSummarize {
			t.Fatalf("private prior body unavailable in list at status %d: %+v %v", status, listed, err)
		}
	}
}

func TestSummaryAvailabilityLegacySharedCacheRemainsHiddenDuringGeneration(t *testing.T) {
	svc, repos, _, _ := newContentDedupTestService(t)
	seedCompletedResults(t, repos, dedupTestMD5, 7)
	task := &model.VideoTask{UserID: 8, FileMD5: dedupTestMD5, Filename: "legacy-repeat.mp4", Status: model.TaskStatusCompleted}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.TaskJob.UpsertQueued(task, model.TaskJobTypeSummary, model.TaskStageSummarizing, 3); err != nil {
		t.Fatal(err)
	}
	detail, err := svc.GetTaskDetail(context.Background(), task.UserID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !detail.HasTranscription || detail.HasSummary || detail.Summary != nil || detail.CanSummarize {
		t.Fatal("pending legacy generation exposed a reused report")
	}
	listed, _, err := svc.ListTasks(task.UserID, 1, 10, "")
	if err != nil || len(listed) != 1 || listed[0].HasSummary || listed[0].CanSummarize {
		t.Fatalf("pending legacy list exposed a reused report: %+v %v", listed, err)
	}
}
