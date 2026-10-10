package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
	"vid-lens/internal/summarydoc"
)

func TestSummaryGenerationPublicationRunAndTagsCommitAtomically(t *testing.T) {
	runSummaryGenerationAtomic(t, summaryRevisionDB(t))
}
func TestPostgresSummaryGenerationPublicationRunAndTagsCommitAtomically(t *testing.T) {
	runSummaryGenerationAtomic(t, openPostgresRepositoryTestDB(t).db)
}
func runSummaryGenerationAtomic(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	repos := NewRepositories(db)
	task := createSourceTask(t, db, 91, 9)
	source, err := repos.PublishTextSource(ctx, PublishTextSourceRequest{UserID: task.UserID, TaskID: task.ID, Snapshot: sourceFixture(t, "检查连接池配置及条件。", 3000)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	generation := uuid.NewString()
	expires := time.Now().Add(time.Hour)
	frozen := processing.GenerationSnapshot{Intent: processing.Intent{GenerationID: generation}, SourceID: source.ID, SourceDigest: source.SourceDigest}
	job := model.TaskJob{TaskID: task.ID, UserID: task.UserID, JobType: model.TaskJobTypeSummary, GenerationID: generation, InputSourceID: source.ID, InputSnapshotJSON: artifact.JSON(frozen), Status: model.TaskStatusRunning, ProcessingToken: "atomic-worker", LeaseKind: model.TaskLeaseKindProcessing, LeaseExpiresAt: &expires}
	if err = db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	lease := SummaryGenerationLease{UserID: task.UserID, TaskID: task.ID, GenerationID: generation, SourceID: source.ID, SourceDigest: source.SourceDigest, LeaseToken: job.ProcessingToken}
	run := &model.AgentRun{ID: generation, UserID: task.UserID, TaskID: task.ID, SubjectKind: model.AgentRunSubjectSummaryGeneration, SubjectID: generation, ExecutionKind: "artifact", RecipeVersion: processing.Recipe, Status: model.AgentRunStatusRunning, Stage: "text_summary", ProfileSnapshot: "{}", PolicySnapshot: artifact.JSON(map[string]any{"source_id": source.ID}), BudgetSnapshot: "{}"}
	if _, err = repos.StartSummaryGeneration(ctx, lease, run); err != nil {
		t.Fatal(err)
	}
	start, end := int64(0), int64(3000)
	document := summarydoc.Document{SchemaVersion: summarydoc.SchemaVersion, DocumentID: generation, SourceID: source.ID, SourceDigest: source.SourceDigest, MediaRevision: task.FileMD5, PresentationMode: "text", Title: "配置摘要", Overview: "核对配置条件。", Blocks: []summarydoc.Block{{ID: "configuration", Order: 1, Title: "条件", BodyMarkdown: "检查配置与适用条件。", SourceRefs: []summarydoc.SourceRef{{SourceID: source.ID, CueIDs: []string{"cue-1"}, StartMS: &start, EndMS: &end, TimingMethod: "subtitle_cue"}}}}}
	req := PublishSummaryDocumentRequest{UserID: task.UserID, TaskID: task.ID, GenerationID: generation, SourceID: source.ID, SourceDigest: source.SourceDigest, LeaseToken: job.ProcessingToken, ExpectedGeneratedHashKind: model.SummaryHashMarkdown, Document: document, ModelName: "fixture"}
	ungrounded := req
	ungrounded.Document.Blocks = append([]summarydoc.Block(nil), document.Blocks...)
	ungrounded.Document.Blocks[0].SourceRefs = nil
	if _, err = repos.PublishTextSummaryGeneration(ctx, ungrounded); err == nil {
		t.Fatal("generation publisher accepted body without frozen source references")
	}
	tags := PrepareTagIntentRequest{UserID: task.UserID, TaskID: task.ID, SourceDigest: source.SourceDigest, GenerationID: generation, Enabled: true}
	for index := 0; index < 6; index++ {
		tags.Candidates = append(tags.Candidates, TagCandidate{Name: "配置", Reason: "来源主题"})
	}
	if _, err = repos.PublishTextSummaryGeneration(ctx, req, tags); err == nil {
		t.Fatal("invalid tag transaction unexpectedly succeeded")
	}
	if row, _ := repos.Summary.FindByTaskID(task.ID); row != nil {
		t.Fatal("canonical summary escaped rollback")
	}
	rolledBackTask, _ := repos.Task.FindByID(task.ID)
	if rolledBackTask.Title != "" || rolledBackTask.TitleOrigin != "" {
		t.Fatal("generated title escaped summary transaction rollback")
	}
	store := NewSummaryGenerationExecutionStore(repos, task.UserID, task.ID, generation)
	saved, _ := store.GetRun(ctx, task.UserID, generation)
	if saved.Stage != "text_summary" || saved.EventSeq != 1 {
		t.Fatalf("run escaped rollback %+v", saved)
	}
	tags.Candidates = nil
	summary, err := repos.PublishTextSummaryGeneration(ctx, req, tags)
	if err != nil {
		t.Fatal(err)
	}
	publishedTask, _ := repos.Task.FindByID(task.ID)
	if publishedTask.Title != document.Title || publishedTask.TitleOrigin != "auto" {
		t.Fatal("generated title was not published atomically with text")
	}
	saved, _ = store.GetRun(ctx, task.UserID, generation)
	if saved.Status != "running" || saved.Stage != "text_ready" {
		t.Fatal("text readiness prematurely terminalized run")
	}
	high := saved.EventSeq
	if _, err = repos.PublishTextSummaryGeneration(ctx, req, tags); err != nil {
		t.Fatal(err)
	}
	saved, _ = store.GetRun(ctx, task.UserID, generation)
	if saved.EventSeq != high {
		t.Fatal("publication replay invented lifecycle events")
	}
	if db.Dialector.Name() == "postgres" {
		// Race an explicit user edit against the actual leased publication path,
		// with a blank title so both CAS outcomes are possible under row locking.
		if err = db.Model(task).Updates(map[string]any{"title": "", "title_origin": ""}).Error; err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		results := make(chan error, 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, e := repos.PublishTextSummaryGeneration(ctx, req, tags)
			results <- e
		}()
		go func() { defer wg.Done(); <-start; results <- repos.Task.UpdateTitle(task.ID, "用户标题") }()
		close(start)
		wg.Wait()
		for i := 0; i < 2; i++ {
			if e := <-results; e != nil {
				t.Fatal(e)
			}
		}
	} else if err = repos.Task.UpdateTitle(task.ID, "用户标题"); err != nil {
		t.Fatal(err)
	}
	if _, err = repos.PublishTextSummaryGeneration(ctx, req, tags); err != nil {
		t.Fatal(err)
	}
	userTask, _ := repos.Task.FindByID(task.ID)
	if userTask.Title != "用户标题" || userTask.TitleOrigin != "user" {
		t.Fatal("publication replaced an explicit user title")
	}
	if err = repos.Task.UpdateTitle(task.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = repos.PublishTextSummaryGeneration(ctx, req, tags); err != nil {
		t.Fatal(err)
	}
	userTask, _ = repos.Task.FindByID(task.ID)
	if userTask.Title != "" || userTask.TitleOrigin != "user" {
		t.Fatal("publication filled an explicitly cleared user title")
	}
	completed, err := repos.CompleteSummaryGeneration(ctx, lease, "skipped", "visual_not_beneficial", &tags, "invalid_tag_candidates")
	if err != nil {
		t.Fatal(err)
	}
	if completed.GeneratedVersion != summary.GeneratedVersion {
		t.Fatal("completion manufactured document version")
	}
	intent, err := repos.UserTag.ReadGenerationIntent(ctx, task.UserID, task.ID, generation, summary.GeneratedVersion)
	if err != nil || intent == nil || intent.Status != "failed" || intent.ErrorCode != "invalid_tag_candidates" {
		t.Fatalf("failed classification receipt=%+v %v", intent, err)
	}
	saved, _ = store.GetRun(ctx, task.UserID, generation)
	if saved.Status != "completed" || saved.FinishedAt == nil {
		t.Fatal("publication completion did not terminalize actual run")
	}
	var count int64
	if err = db.Model(&model.RunEvent{}).Where("run_id=? AND type=?", generation, "run.completed").Count(&count).Error; err != nil || count != 1 {
		t.Fatal("terminal event did not commit")
	}
}
