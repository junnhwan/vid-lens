package repository

import (
	"context"
	"testing"
	"time"
	"vid-lens/internal/model"
)

func TestDownloadIdentityCannotDriftOrBeWrittenByStaleWorker(t *testing.T) {
	repos := NewRepositories(summaryRevisionDB(t))
	ctx := context.Background()
	now := time.Now()
	task := model.VideoTask{UserID: 7, FileMD5: "11111111111111111111111111111111", SourceType: model.TaskSourceTypeURL, SourceURL: "https://www.bilibili.com/video/BV1xx411c7mD?p=2"}
	dispatch, err := repos.PrepareInitialTaskDispatch(InitialTaskDispatchRequest{Task: &task, CreateTask: true, JobType: model.TaskJobTypeDownload, Stage: model.TaskStageDownloading, Token: "dispatch", Now: now, LeaseUntil: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := repos.ClaimTaskProcessing(TaskProcessingClaimRequest{TaskID: task.ID, JobType: model.TaskJobTypeDownload, Stage: model.TaskStageDownloading, MessageToken: dispatch.Token, NewToken: "worker", Now: now, LeaseUntil: now.Add(time.Minute)})
	if err != nil || claim.Outcome != TaskLeaseAcquired {
		t.Fatalf("claim=%+v %v", claim, err)
	}
	frozen := `{"bvid":"BV1xx411c7mD","cid":222,"part_index":2}`
	if err := repos.FreezeDownloadIdentity(ctx, task.ID, "stale", frozen); err == nil {
		t.Fatal("stale worker saved download identity")
	}
	if err := repos.FreezeDownloadIdentity(ctx, task.ID, claim.Token, frozen); err != nil {
		t.Fatal(err)
	}
	if err := repos.FreezeDownloadIdentity(ctx, task.ID, claim.Token, frozen); err != nil {
		t.Fatal(err)
	}
	if err := repos.FreezeDownloadIdentity(ctx, task.ID, claim.Token, `{"cid":333,"part_index":3}`); err == nil {
		t.Fatal("download identity changed on replay")
	}
	job, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeDownload)
	if job.InputSnapshotJSON != frozen {
		t.Fatal("frozen download identity overwritten")
	}
}
