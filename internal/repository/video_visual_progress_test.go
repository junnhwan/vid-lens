package repository

import (
	"testing"
	"time"

	"vid-lens/internal/model"
)

func TestVisualProgressFencesRetryAndPublishesEvidenceAtomically(t *testing.T) {
	repos := newTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Filename: "lesson.mp4", Status: model.TaskStatusQueued}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.VisualFrame.ReplaceTaskFrames(task.ID, []model.VideoVisualFrame{{TaskID: task.ID, FrameIndex: 0, OCRText: "old", Status: model.VisualFrameStatusCompleted}}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	claim := func(token string, at time.Time) TaskProcessingLeaseRequest {
		t.Helper()
		got, err := repos.ClaimTaskProcessing(TaskProcessingClaimRequest{
			TaskID: task.ID, JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing,
			Now: at, LeaseUntil: at.Add(time.Minute), NewToken: token,
		})
		if err != nil || got.Outcome != TaskLeaseAcquired {
			t.Fatalf("claim %q: %+v, %v", token, got, err)
		}
		return TaskProcessingLeaseRequest{TaskID: task.ID, JobType: model.TaskJobTypeTranscribe, Token: token, Now: at}
	}
	first := claim("worker-1", now)
	if ok, err := repos.BeginVisualProgress(first); err != nil || !ok {
		t.Fatalf("begin: %v, %v", ok, err)
	}
	if ok, err := repos.AdvanceVisualProgress(first, VisualProgressUpdate{Phase: "observing_frames", Status: model.VisualProgressRunning, TotalKnown: true, TotalFrames: 2, Processed: 1}); err != nil || !ok {
		t.Fatalf("advance: %v, %v", ok, err)
	}
	// A duplicate or delayed frame notification cannot regress counters.
	if ok, err := repos.AdvanceVisualProgress(first, VisualProgressUpdate{Phase: "extracting", TotalKnown: true, TotalFrames: 2, Processed: 0}); err != nil || !ok {
		t.Fatalf("duplicate advance: %v, %v", ok, err)
	}
	progress, _ := repos.VisualProgress.Find(task.ID)
	if progress.Processed != 1 || progress.Phase != "observing_frames" {
		t.Fatalf("regressed progress: %+v", progress)
	}
	frames, _ := repos.VisualFrame.ListByTaskID(task.ID)
	if len(frames) != 1 || frames[0].OCRText != "old" {
		t.Fatalf("partial evidence leaked: %+v", frames)
	}
	if ok, err := repos.AdvanceVisualProgress(first, VisualProgressUpdate{
		Status: model.VisualProgressFailed, Phase: "observing_frames", TotalKnown: true,
		TotalFrames: 2, Processed: 1, Failed: 1, ErrorCode: "visual_processing_failed",
	}); err != nil || !ok {
		t.Fatalf("record failed attempt: %v, %v", ok, err)
	}
	frames, _ = repos.VisualFrame.ListByTaskID(task.ID)
	if len(frames) != 1 || frames[0].OCRText != "old" {
		t.Fatalf("failed attempt changed published evidence: %+v", frames)
	}

	second := claim("worker-2", now.Add(2*time.Minute))
	if ok, err := repos.BeginVisualProgress(second); err != nil || !ok {
		t.Fatalf("retry begin: %v, %v", ok, err)
	}
	progress, _ = repos.VisualProgress.Find(task.ID)
	if progress.Processed != 0 || progress.TotalKnown || progress.AttemptToken != "worker-2" {
		t.Fatalf("retry inherited progress: %+v", progress)
	}
	if ok, err := repos.AdvanceVisualProgress(first, VisualProgressUpdate{Phase: "observing_frames", Processed: 2}); err != nil || ok {
		t.Fatalf("stale worker updated progress: %v, %v", ok, err)
	}
	if ok, err := repos.PublishVisualFrames(first, []model.VideoVisualFrame{{TaskID: task.ID, FrameIndex: 0, OCRText: "stale"}}, VisualProgressUpdate{TotalKnown: true, TotalFrames: 1, Processed: 1}); err != nil || ok {
		t.Fatalf("stale worker published: %v, %v", ok, err)
	}
	if ok, err := repos.PublishVisualFrames(second, []model.VideoVisualFrame{{TaskID: task.ID + 1, FrameIndex: 0, OCRText: "bad"}}, VisualProgressUpdate{TotalKnown: true, TotalFrames: 1, Processed: 1}); err == nil || ok {
		t.Fatalf("invalid publish unexpectedly succeeded: %v, %v", ok, err)
	}
	frames, _ = repos.VisualFrame.ListByTaskID(task.ID)
	if len(frames) != 1 || frames[0].OCRText != "old" {
		t.Fatalf("failed transaction changed evidence: %+v", frames)
	}
	valid := []model.VideoVisualFrame{{TaskID: task.ID, FrameIndex: 0, OCRText: "new", Status: model.VisualFrameStatusCompleted}}
	if ok, err := repos.PublishVisualFrames(second, valid, VisualProgressUpdate{TotalKnown: true, TotalFrames: 1, Processed: 1}); err != nil || !ok {
		t.Fatalf("publish: %v, %v", ok, err)
	}
	if ok, err := repos.PublishVisualFrames(second, []model.VideoVisualFrame{{TaskID: task.ID, FrameIndex: 0, OCRText: "duplicate"}}, VisualProgressUpdate{TotalKnown: true, TotalFrames: 1, Processed: 1}); err == nil || ok {
		t.Fatalf("duplicate publish unexpectedly succeeded: %v, %v", ok, err)
	}
	if ok, err := repos.BeginVisualProgress(second); err == nil || ok {
		t.Fatalf("terminal attempt restarted: %v, %v", ok, err)
	}
	frames, _ = repos.VisualFrame.ListByTaskID(task.ID)
	progress, _ = repos.VisualProgress.Find(task.ID)
	if len(frames) != 1 || frames[0].OCRText != "new" || progress.Status != model.VisualProgressCompleted {
		t.Fatalf("published state: frames=%+v progress=%+v", frames, progress)
	}
}
