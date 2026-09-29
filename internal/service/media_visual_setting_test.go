package service

import (
	"context"
	"testing"

	"vid-lens/internal/model"
	"vid-lens/internal/mq"
)

func TestSetTaskVisualDisabledPreservesExistingResults(t *testing.T) {
	repos := newMediaTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", Filename: "video.mp4", Status: model.TaskStatusCompleted}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.Transcription.Create(&model.VideoTranscription{TaskID: task.ID, FileMD5: task.FileMD5, Content: "转写正文"}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Summary.Create(&model.AISummary{TaskID: task.ID, FileMD5: task.FileMD5, Content: "摘要正文"}); err != nil {
		t.Fatal(err)
	}
	if err := repos.VisualFrame.ReplaceTaskFrames(task.ID, []model.VideoVisualFrame{{TaskID: task.ID, FrameIndex: 0, TimeMs: 1000, OCRText: "课件", Status: model.VisualFrameStatusCompleted}}); err != nil {
		t.Fatal(err)
	}
	svc := &MediaService{repo: repos}
	updated, err := svc.SetTaskVisualDisabled(context.Background(), 7, task.ID, true)
	if err != nil || !updated.VisualDisabled {
		t.Fatalf("setting update = %+v, %v", updated, err)
	}
	transcription, _ := repos.Transcription.FindByTaskID(task.ID)
	summary, _ := repos.Summary.FindByTaskID(task.ID)
	frames, _ := repos.VisualFrame.ListByTaskID(task.ID)
	if transcription == nil || transcription.Content != "转写正文" || summary == nil || summary.Content != "摘要正文" || len(frames) != 1 || frames[0].OCRText != "课件" {
		t.Fatalf("existing results changed: transcription=%+v summary=%+v frames=%+v", transcription, summary, frames)
	}
}

type visualDispatchProducer struct {
	recordingMediaProducer
	visuals    int
	claimToken string
}

func (p *visualDispatchProducer) EnqueueVisual(ctx context.Context, _ int64) error {
	p.visuals++
	p.claimToken = mq.ClaimTokenFromContext(ctx)
	return nil
}

func TestVisualBuildUsesOwnDispatchAndPreservesASR(t *testing.T) {
	repos := newMediaTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "standalone-visual", Filename: "slides.mp4", FileURL: "videos/slides", Status: model.TaskStatusCompleted, VisualMode: model.VisualModeOff, VisualDisabled: true}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	seedMediaTestTranscription(t, repos, task)
	producer := &visualDispatchProducer{}
	svc := &MediaService{repo: repos, mq: producer}
	if err := svc.RequestVisualBuild(context.Background(), 7, task.ID); err == nil {
		t.Fatal("off mode queued visual work")
	}
	if _, err := svc.SetTaskVisualMode(context.Background(), 8, task.ID, model.VisualModeOCR); err == nil {
		t.Fatal("other owner modified mode")
	}
	if _, err := svc.SetTaskVisualMode(context.Background(), 7, task.ID, "invalid"); err == nil {
		t.Fatal("invalid mode accepted")
	}
	if _, err := svc.SetTaskVisualMode(context.Background(), 7, task.ID, model.VisualModeOCR); err != nil {
		t.Fatal(err)
	}
	if err := svc.RequestVisualBuild(context.Background(), 7, task.ID); err != nil {
		t.Fatal(err)
	}
	current, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.LastJobType != model.TaskJobTypeVisual || current.Status != model.TaskStatusQueued || producer.visuals != 1 || producer.claimToken == "" {
		t.Fatalf("dispatch task=%+v producer=%+v", current, producer)
	}
	if len(producer.transcribes) != 0 || len(producer.analyzes) != 0 {
		t.Fatal("visual build queued ASR or summary")
	}
	transcription, _ := repos.Transcription.FindByTaskID(task.ID)
	if transcription == nil || transcription.Content != "已完成的完整转写" {
		t.Fatalf("ASR changed: %+v", transcription)
	}
	if _, err := svc.SetTaskVisualMode(context.Background(), 7, task.ID, model.VisualModeCaption); err == nil {
		t.Fatal("mode changed during dispatch")
	}
	if err := svc.RequestVisualBuild(context.Background(), 7, task.ID); err == nil || producer.visuals != 1 {
		t.Fatal("duplicate build queued")
	}
}

func TestNewAssetImportDefaultsToOffWithoutChangingLegacyTask(t *testing.T) {
	repos := newMediaTestRepositories(t)
	asset := createMediaTestAsset(t, repos, "default-off-upload", "videos/upload.mp4")
	legacy := createMediaTestTask(t, repos, 7, asset, "legacy.mp4")
	svc := &MediaService{repo: repos}
	result, err := svc.createTaskFromAsset(7, "new.mp4", asset, model.TaskStatusPending)
	if err != nil {
		t.Fatal(err)
	}
	created, err := repos.Task.FindByID(result.TaskID)
	if err != nil || created.VisualMode != model.VisualModeOff || !created.VisualDisabled {
		t.Fatalf("new video mode=%+v err=%v", created, err)
	}
	old, err := repos.Task.FindByID(legacy.ID)
	if err != nil || old.EffectiveVisualMode() != model.VisualModeBoth {
		t.Fatalf("legacy video mode=%+v err=%v", old, err)
	}
}
