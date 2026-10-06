package mq

import (
	"context"
	"errors"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/ffmpeg"
)

// The production refresh completed 17/18 short windows, but visual success
// marked the task completed and left its old five-minute index in service.
func TestTranscriptionRefreshFailureCannotCompleteThroughVisualFallback(t *testing.T) {
	for _, priorTranscript := range []bool{true, false} {
		name := "refresh"
		if !priorTranscript {
			name = "first_transcription_partial_failure"
		}
		t.Run(name, func(t *testing.T) {
			repos, consumer, task, token, _ := newTranscribeCompletionFixture(t)
			consumer.retryPolicy.Now = consumer.currentTime
			if !priorTranscript {
				if err := repos.Transcription.DeleteByTaskID(task.ID); err != nil {
					t.Fatal(err)
				}
			}
			producer := &recordingRAGIndexProducer{}
			consumer.ragProducer = producer
			consumer.asrConcurrency = 1
			consumer.splitAudioWindows = func(context.Context, string, string, int, int) ([]ffmpeg.AudioSegment, string, error) {
				return []ffmpeg.AudioSegment{
					{Path: "first.mp3", SegmentKey: "first", Version: ffmpeg.AudioSegmenterVersion, WindowEndMS: 22000, CoreEndMS: 20000},
					{Path: "failed.mp3", SegmentKey: "second", Version: ffmpeg.AudioSegmenterVersion, WindowStartMS: 18000, WindowEndMS: 42000, CoreStartMS: 20000, CoreEndMS: 40000},
				}, "", nil
			}
			strategy := &recordingAI{transcripts: map[string]string{"first.mp3": "保留已识别的句子。"}, transcribeErrors: map[string]error{"failed.mp3": ai.ErrRetryBudgetExhausted}}
			_, asrErr := consumer.transcription().transcribeAudio(context.Background(), task.ID, "audio.mp3", strategy)
			if !errors.Is(asrErr, ai.ErrRetryBudgetExhausted) {
				t.Fatalf("expected failed short window: %v", asrErr)
			}
			handled, err := consumer.completeTranscribeWithVisualOnly(context.Background(), task, token, asrErr, func() visualIndexOutcome { return visualIndexOutcome{count: 4} })
			if err != nil || handled {
				t.Fatalf("failed ASR was swallowed by visual success: handled=%v err=%v", handled, err)
			}
			// Exercise the caller's failure path so the status is visible/retryable.
			if err := consumer.recordTaskFailure(task.ID, TaskJobTranscribe, model.TaskStageTranscribing, asrErr, token); err != nil {
				t.Fatal(err)
			}
			current, err := repos.Task.FindByID(task.ID)
			if err != nil || current.Status != model.TaskStatusFailed || current.LastErrorMsg == "" {
				t.Fatalf("failure status missing: task=%+v err=%v", current, err)
			}
			if len(producer.taskIDs) != 0 {
				t.Fatalf("failed refresh queued stale-source indexing: %v", producer.taskIDs)
			}
			// Retry only the missing window, preserving successful paid work.
			strategy.transcribeErrors = nil
			strategy.transcripts["failed.mp3"] = "补齐失败的句子。"
			strategy.transcribeInput = nil
			if _, err := consumer.transcription().transcribeAudio(context.Background(), task.ID, "audio.mp3", strategy); err != nil {
				t.Fatal(err)
			}
			if len(strategy.transcribeInput) != 1 || strategy.transcribeInput[0] != "failed.mp3" {
				t.Fatalf("retry repeated completed windows: %v", strategy.transcribeInput)
			}
		})
	}
}

func TestTranscriptionRefreshFailureBeforeChunkingKeepsPreviousResult(t *testing.T) {
	repos, consumer, task, token, _ := newTranscribeCompletionFixture(t)
	handled, err := consumer.completeTranscribeWithVisualOnly(context.Background(), task, token, errors.New("audio download failed"), func() visualIndexOutcome { return visualIndexOutcome{count: 4} })
	if err != nil || handled {
		t.Fatalf("refresh reported success without replacing transcript: handled=%v err=%v", handled, err)
	}
	prior, err := repos.Transcription.FindByTaskID(task.ID)
	if err != nil || prior == nil || prior.Content != "transcript" {
		t.Fatalf("previous published transcript lost: %+v %v", prior, err)
	}
}
