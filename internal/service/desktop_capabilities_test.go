package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

func TestStudyCapabilityMatchesGenerationSourceForVisualOnlyAndBusy(t *testing.T) {
	repos := newMediaTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "silent-slides", Filename: "slides.mp4", FileURL: "videos/slides", Status: model.TaskStatusCompleted}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	frames := []model.VideoVisualFrame{{TaskID: task.ID, FrameIndex: 0, FrameKey: "slide-1", Status: model.VisualFrameStatusCompleted, OCRText: "事务的原子性", TimeMs: 1000}}
	if err := repos.VisualFrame.ReplaceTaskFrames(task.ID, frames); err != nil {
		t.Fatal(err)
	}
	svc := &MediaService{repo: repos}
	timeline, err := svc.GetVideoTimeline(context.Background(), 7, task.ID)
	if err != nil || !timeline.StudySourceReady {
		t.Fatalf("visual source: %+v %v", timeline, err)
	}
	if _, items, err := artifactSource(context.Background(), repos, 7, task.ID); err != nil || len(items) != 1 {
		t.Fatalf("generation: %v %v", items, err)
	}
	if err := repos.Task.UpdateStatus(task.ID, model.TaskStatusRunning, ""); err != nil {
		t.Fatal(err)
	}
	timeline, err = svc.GetVideoTimeline(context.Background(), 7, task.ID)
	if err != nil || timeline.StudySourceReady || timeline.StudySourceReason != "processing" {
		t.Fatalf("busy: %+v %v", timeline, err)
	}
	if _, _, err := artifactSource(context.Background(), repos, 7, task.ID); err == nil {
		t.Fatal("busy source admitted")
	}
	if err := repos.Task.UpdateStatus(task.ID, model.TaskStatusCompleted, ""); err != nil {
		t.Fatal(err)
	}
	frames[0].OCRText = "  "
	if err := repos.VisualFrame.ReplaceTaskFrames(task.ID, frames); err != nil {
		t.Fatal(err)
	}
	timeline, err = svc.GetVideoTimeline(context.Background(), 7, task.ID)
	if err != nil || timeline.StudySourceReady || timeline.StudySourceReason != "no_content" {
		t.Fatalf("empty frame: %+v %v", timeline, err)
	}
}

func TestStudyCapabilityRejectsIncompleteChunksAndOversizedSource(t *testing.T) {
	task := &model.VideoTask{}
	timeline := VideoTimeline{Atoms: []TimelineAtom{{Content: "valid"}}}
	if got := studySourceReason(task, []model.VideoTranscriptionChunk{{Status: model.TranscriptionChunkStatusFailed}}, timeline); got != "incomplete_transcript" {
		t.Fatal(got)
	}
	timeline.Atoms[0].Content = strings.Repeat("x", 2*1024*1024+1)
	if got := studySourceReason(task, nil, timeline); got != "source_limit_exceeded" {
		t.Fatal(got)
	}
	timeline.Atoms = make([]TimelineAtom, 1001)
	if got := studySourceReason(task, nil, timeline); got != "source_limit_exceeded" {
		t.Fatal(got)
	}
}

func TestVisualBuildAdmitsImportedVideoWithoutTranscription(t *testing.T) {
	repos := newMediaTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "silent", Filename: "silent.mp4", FileURL: "videos/silent", VisualMode: model.VisualModeOCR}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	producer := &visualDispatchProducer{}
	svc := &MediaService{repo: repos, mq: producer}
	if err := svc.RequestVisualBuild(context.Background(), 7, task.ID); err != nil {
		t.Fatal(err)
	}
	if producer.visuals != 1 || len(producer.transcribes) != 0 {
		t.Fatal("visual work coupled to ASR")
	}
}

func TestMediaListRetrievableTracksCurrentModel(t *testing.T) {
	repos := newMediaTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "index-model", Filename: "index.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.RAGIndex.Upsert(&model.VideoRAGIndex{TaskID: task.ID, UserID: 7, FileMD5: task.FileMD5, EmbeddingModel: "old", EmbeddingDim: 3, Status: model.RAGIndexStatusIndexed}); err != nil {
		t.Fatal(err)
	}
	svc := (&MediaService{repo: repos}).WithAIProfiles(stubConversationProfileProvider{profile: ai.Profile{EmbeddingModel: "current"}})
	list, _, err := svc.ListTasks(7, 1, 20, "")
	if err != nil || len(list) != 1 || !list[0].HasRAGIndex || list[0].Retrievable {
		t.Fatalf("old model: %+v %v", list, err)
	}
	detail, err := svc.GetTaskDetail(context.Background(), 7, task.ID)
	if err != nil || detail.Retrievable {
		t.Fatalf("old model detail: %+v %v", detail, err)
	}
	svc.WithAIProfiles(stubConversationProfileProvider{profile: ai.Profile{EmbeddingModel: "old"}})
	list, _, err = svc.ListTasks(7, 1, 20, "")
	if err != nil || !list[0].Retrievable {
		t.Fatalf("current model: %+v %v", list, err)
	}
	detail, err = svc.GetTaskDetail(context.Background(), 7, task.ID)
	if err != nil || !detail.Retrievable {
		t.Fatalf("current model detail: %+v %v", detail, err)
	}
}

func TestSuggestedQuestionsModelFailureKeepsTableOutOfFallback(t *testing.T) {
	repos := newMediaTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "table-fallback", Filename: "lesson.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.Summary.Create(&model.AISummary{TaskID: task.ID, FileMD5: task.FileMD5, Content: "| 概念 | 作用 |\n|:---|:---|\n| RRF | 合并排序 |\n\n## 混合检索如何合并结果\n向量与关键词检索互相补充。"}); err != nil {
		t.Fatal(err)
	}
	client := &questionTestClient{err: errors.New("provider unavailable")}
	if err := repos.Transcription.Create(&model.VideoTranscription{TaskID: task.ID, FileMD5: task.FileMD5, Content: "本课解释混合检索如何合并结果。"}); err != nil {
		t.Fatal(err)
	}
	svc := NewQuestionSuggestionService(repos, stubConversationProfileProvider{}, questionTestFactory{client})
	result, err := svc.VideoQuestions(context.Background(), 7, task.ID, true)
	if err != nil || len(result.Questions) == 0 || client.calls != 1 {
		t.Fatalf("result %+v err %v calls %d", result, err, client.calls)
	}
	for _, question := range result.Questions {
		if strings.Contains(question.Question, "|") || strings.Contains(question.Question, ":---") {
			t.Fatal(question.Question)
		}
	}
}

func TestArtifactRunRecoveryOnlyAfterLeaseExpires(t *testing.T) {
	svc, db, _ := artifactFixture(t, artifactModelResponse)
	run, err := svc.Submit(context.Background(), 7, "lease-ui", artifactRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	view, err := svc.Run(context.Background(), 7, run.ID)
	if err != nil || view.CanResume {
		t.Fatalf("pending: %+v %v", view, err)
	}
	future := time.Now().Add(time.Minute)
	if err := db.Model(&model.AgentRun{}).Where("id=?", run.ID).Updates(map[string]any{"status": "running", "run_lease_until": future}).Error; err != nil {
		t.Fatal(err)
	}
	view, err = svc.Run(context.Background(), 7, run.ID)
	if err != nil || view.CanResume {
		t.Fatalf("healthy: %+v %v", view, err)
	}
	past := time.Now().Add(-time.Minute)
	if err := db.Model(&model.AgentRun{}).Where("id=?", run.ID).Update("run_lease_until", past).Error; err != nil {
		t.Fatal(err)
	}
	view, err = svc.Run(context.Background(), 7, run.ID)
	if err != nil || !view.CanResume {
		t.Fatalf("expired: %+v %v", view, err)
	}
}
