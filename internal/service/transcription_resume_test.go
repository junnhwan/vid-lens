package service

import (
	"context"
	"testing"
	"time"

	"vid-lens/internal/model"
)

func TestRequestTranscribeResumesIncompleteRefreshWithPublishedTranscript(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		status    int8
		hasChunks bool
	}{
		{"legacy_false_success", model.TaskStatusCompleted, true},
		{"visible_failure", model.TaskStatusFailed, true},
		{"download_failure_before_chunking", model.TaskStatusFailed, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			repos := newMediaTestRepositories(t)
			task := &model.VideoTask{UserID: 7, FileMD5: "abababababababababababababababab", Filename: "refresh.mp4", Status: scenario.status, LastJobType: model.TaskJobTypeTranscribe}
			if err := repos.Task.Create(task); err != nil {
				t.Fatal(err)
			}
			if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, FileMD5: task.FileMD5, Content: "previous five-minute transcript"}); err != nil {
				t.Fatal(err)
			}
			if scenario.hasChunks {
				if err := repos.TranscriptionChunk.UpsertCompletedWithRange(task.ID, 0, "first.mp3", "completed short window", 0, 22); err != nil {
					t.Fatal(err)
				}
				if err := repos.TranscriptionChunk.UpsertFailed(task.ID, 1, "second.mp3", "retry budget exhausted"); err != nil {
					t.Fatal(err)
				}
			}
			if err := repos.TaskJob.UpsertQueued(task, model.TaskJobTypeTranscribe, model.TaskStageTranscribing, 3); err != nil {
				t.Fatal(err)
			}
			oldBudget, err := repos.EnsureTaskJobRetryBudget(task.ID, model.TaskJobTypeTranscribe, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			producer := &recordingMediaProducer{}
			svc := &MediaService{repo: repos, mq: producer}
			if err := svc.RequestTranscribe(context.Background(), task.UserID, task.ID, false); err != nil {
				t.Fatalf("resume rejected because old transcript exists: %v", err)
			}
			if len(producer.transcribes) != 1 || producer.transcribes[0] != task.ID {
				t.Fatalf("resume not enqueued: %v", producer.transcribes)
			}
			chunks, err := repos.TranscriptionChunk.ListByTaskID(task.ID)
			if err != nil || scenario.hasChunks && (len(chunks) != 2 || chunks[0].Status != model.TranscriptionChunkStatusCompleted || chunks[0].Content != "completed short window") || !scenario.hasChunks && len(chunks) != 0 {
				t.Fatalf("successful paid work erased: %+v %v", chunks, err)
			}
			prior, err := repos.Transcription.FindByTaskID(task.ID)
			if err != nil || prior == nil || prior.Content != "previous five-minute transcript" {
				t.Fatalf("published result erased before replacement: %+v %v", prior, err)
			}
			job, err := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeTranscribe)
			if err != nil || job == nil || job.Status != model.TaskStatusQueued || job.RetryBudgetID == "" || job.RetryBudgetID == oldBudget {
				t.Fatalf("resume missing queue/budget: %+v %v", job, err)
			}
		})
	}
}

func TestRequestTranscribeDoesNotRedoCompletedTranscriptWithoutForce(t *testing.T) {
	repos := newMediaTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "abababababababababababababababab", Filename: "done.mp4", Status: model.TaskStatusCompleted}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "published transcript"}); err != nil {
		t.Fatal(err)
	}
	if err := repos.TranscriptionChunk.UpsertCompleted(task.ID, 0, "first.mp3", "published transcript"); err != nil {
		t.Fatal(err)
	}
	producer := &recordingMediaProducer{}
	svc := &MediaService{repo: repos, mq: producer}
	if err := svc.RequestTranscribe(context.Background(), task.UserID, task.ID, false); err == nil {
		t.Fatal("completed ASR was unnecessarily repeated")
	}
	if len(producer.transcribes) != 0 {
		t.Fatal("completed ASR was enqueued")
	}
}
