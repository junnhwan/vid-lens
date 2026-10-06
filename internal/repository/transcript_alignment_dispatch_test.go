package repository

import (
	"testing"
	"time"
	"vid-lens/internal/model"
)

func TestAlignmentOnlyIntentSurvivesDispatchClaimAndResetsForNewASR(t *testing.T) {
	repos := newTestRepositories(t)
	now := time.Now()
	task := &model.VideoTask{UserID: 1, FileMD5: "alignment-dispatch", Status: model.TaskStatusCompleted, Stage: model.TaskStageNone, MaxRetries: 3}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	req := InitialTaskDispatchRequest{Task: task, AllowedStatuses: []int8{model.TaskStatusCompleted, model.TaskStatusQueued}, JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing, Now: now, LeaseUntil: now.Add(time.Minute), Token: "align", TranscriptAlignmentOnly: true}
	if _, err := repos.PrepareInitialTaskDispatch(req); err != nil {
		t.Fatal(err)
	}
	// Claim/redispatch update the existing job rather than forgetting its mode.
	if err := repos.TaskJob.UpsertDispatching(task, model.TaskJobTypeTranscribe, model.TaskStatusRunning, model.TaskStageTranscribing); err != nil {
		t.Fatal(err)
	}
	job, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeTranscribe)
	if job == nil || !job.TranscriptAlignmentOnly {
		t.Fatal("retry lost alignment-only intent")
	}
	req.TranscriptAlignmentOnly = false
	req.Token = "new-asr"
	if _, err := repos.PrepareInitialTaskDispatch(req); err != nil {
		t.Fatal(err)
	}
	job, _ = repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeTranscribe)
	if job.TranscriptAlignmentOnly {
		t.Fatal("fresh ASR request inherited alignment-only mode")
	}
}
