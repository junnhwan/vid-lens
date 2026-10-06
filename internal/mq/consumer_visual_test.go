package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/visualprogress"
	"vid-lens/internal/repository"
)

func TestStandaloneVisualJobPublishesUnderItsLeaseAndRebuildsIndexWithoutASR(t *testing.T) {
	for _, missingEmbedding := range []bool{false, true} {
		t.Run(fmt.Sprintf("missing_embedding_%v", missingEmbedding), func(t *testing.T) {
			repos := newConsumerTestRepositories(t)
			task := &model.VideoTask{UserID: 7, FileMD5: "standalone-visual", Filename: "slides.mp4", FileURL: "videos/slides", Status: model.TaskStatusCompleted, VisualMode: model.VisualModeOCR}
			if err := repos.Task.Create(task); err != nil {
				t.Fatal(err)
			}
			if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, FileMD5: task.FileMD5, Content: "原有转写"}); err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			dispatch, err := repos.PrepareInitialTaskDispatch(repository.InitialTaskDispatchRequest{
				Task: task, AllowedStatuses: []int8{model.TaskStatusCompleted}, JobType: model.TaskJobTypeVisual, Stage: model.TaskStageVisual,
				Now: now, LeaseUntil: now.Add(time.Hour), Token: "dispatch-visual",
			})
			if err != nil {
				t.Fatal(err)
			}
			visualCalls, indexCalls := 0, 0
			consumer := &Consumer{repo: repos}
			if missingEmbedding {
				consumer.profiles = staticProfileResolver{profile: &ai.Profile{LLMModel: "unused"}}
			}
			expectedIndexCalls := 1
			if missingEmbedding {
				expectedIndexCalls = 0
			}
			consumer.visualIndex = func(ctx context.Context, task *model.VideoTask) (int, error) {
				visualCalls++
				if visualprogress.JobType(ctx) != model.TaskJobTypeVisual {
					t.Errorf("job type = %s", visualprogress.JobType(ctx))
				}
				owned, err := repos.PublishVisualFrames(repository.TaskProcessingLeaseRequest{TaskID: task.ID, JobType: model.TaskJobTypeVisual, Token: visualprogress.Attempt(ctx), Now: time.Now()},
					[]model.VideoVisualFrame{{TaskID: task.ID, FrameIndex: 0, TimeMs: 1000, OCRText: "课件文字", Status: model.VisualFrameStatusCompleted}},
					repository.VisualProgressUpdate{TotalKnown: true, TotalFrames: 1, Processed: 1})
				if err != nil {
					return 0, err
				}
				if !owned {
					t.Error("visual publish lease was lost")
				}
				return 1, nil
			}
			consumer.ragIndex = func(ctx context.Context, task *model.VideoTask) error {
				indexCalls++
				if err := requireProcessingLease(ctx); err != nil {
					return err
				}
				frames, err := repos.VisualFrame.ListCompletedWithText(task.ID)
				if err != nil {
					return err
				}
				if len(frames) != 1 || frames[0].OCRText != "课件文字" {
					t.Errorf("index input = %+v", frames)
				}
				return nil
			}
			body, _ := json.Marshal(RAGIndexPayload{TaskID: task.ID, JobType: model.TaskJobTypeVisual, ClaimToken: dispatch.Token})
			if err := consumer.handleRAGIndex(context.Background(), amqp.Delivery{Body: body}); err != nil {
				t.Fatal(err)
			}
			if visualCalls != 1 || indexCalls != expectedIndexCalls {
				t.Fatalf("calls visual=%d index=%d", visualCalls, indexCalls)
			}
			current, _ := repos.Task.FindByID(task.ID)
			job, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeVisual)
			transcription, _ := repos.Transcription.FindByTaskID(task.ID)
			if current.Status != model.TaskStatusCompleted || job.Status != model.TaskStatusCompleted || transcription.Content != "原有转写" {
				t.Fatalf("task=%+v job=%+v transcription=%+v", current, job, transcription)
			}
			if err := consumer.handleRAGIndex(context.Background(), amqp.Delivery{Body: body}); err != nil {
				t.Fatal(err)
			}
			if visualCalls != 1 || indexCalls != expectedIndexCalls {
				t.Fatal("duplicate message reran visual work")
			}
		})
	}

}
