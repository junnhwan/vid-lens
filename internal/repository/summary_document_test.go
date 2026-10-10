package repository

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/summarydoc"
)

func TestSummaryDocumentPublishFencesSourceGenerationAndVersions(t *testing.T) {
	runSummaryDocumentPublish(t, summaryRevisionDB(t))
}
func TestPostgresSummaryDocumentPublishFencesSourceGenerationAndVersions(t *testing.T) {
	runSummaryDocumentPublish(t, openPostgresRepositoryTestDB(t).db)
}

func runSummaryDocumentPublish(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	repos := NewRepositories(db)
	task := createSourceTask(t, db, 91, 9)
	source, err := repos.PublishTextSource(ctx, PublishTextSourceRequest{UserID: task.UserID, TaskID: task.ID, Snapshot: sourceFixture(t, "检查连接池配置及适用条件。", 3000)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	old := model.AISummary{TaskID: task.ID, FileMD5: task.FileMD5, Content: "已有摘要，重新生成期间可继续阅读。"}
	if err := db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	head := model.SummaryRevisionHead{UserID: task.UserID, TaskID: task.ID, Version: 1, CurrentRevisionID: "user-revision"}
	revision := model.SummaryRevision{ID: head.CurrentRevisionID, UserID: task.UserID, TaskID: task.ID, Version: 1, Content: "用户已经修改的内容。", BaseGeneratedHash: artifact.Hash(old.Content), Origin: "agent", CreatedAt: time.Now()}
	if err := db.Create(&revision).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&head).Error; err != nil {
		t.Fatal(err)
	}
	expiry := time.Now().Add(time.Minute)
	job := model.TaskJob{TaskID: task.ID, UserID: task.UserID, JobType: model.TaskJobTypeSummary, GenerationID: "generation-1", InputSourceID: source.ID, Status: model.TaskStatusRunning, ProcessingToken: "worker-1", LeaseKind: model.TaskLeaseKindProcessing, LeaseExpiresAt: &expiry}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	start, end := int64(0), int64(3000)
	doc := summarydoc.Document{SchemaVersion: summarydoc.SchemaVersion, DocumentID: "document-1", SourceID: source.ID, SourceDigest: source.SourceDigest, MediaRevision: task.FileMD5, PresentationMode: "text", Title: "连接池配置", Overview: "检查配置，并核对适用条件。", Blocks: []summarydoc.Block{{ID: "configuration", Order: 1, Title: "设置", BodyMarkdown: "根据连接池配置调整参数。", SourceRefs: []summarydoc.SourceRef{{SourceID: source.ID, CueIDs: []string{"cue-1"}, StartMS: &start, EndMS: &end, TimingMethod: "subtitle_cue"}}}}}
	req := PublishSummaryDocumentRequest{UserID: task.UserID, TaskID: task.ID, GenerationID: job.GenerationID, SourceID: source.ID, SourceDigest: source.SourceDigest, LeaseToken: job.ProcessingToken, ExpectedGeneratedVersion: 0, ExpectedGeneratedHash: artifact.Hash(old.Content), ExpectedGeneratedHashKind: model.SummaryHashMarkdown, Document: doc, ModelName: "test"}
	bad := req
	bad.LeaseToken = "old-worker"
	if _, err := repos.PublishSummaryDocument(ctx, bad); err == nil {
		t.Fatal("old worker published")
	}
	bad = req
	bad.GenerationID = "wrong-generation"
	if _, err := repos.PublishSummaryDocument(ctx, bad); err == nil {
		t.Fatal("wrong generation published")
	}
	bad = req
	bad.ExpectedGeneratedHash = "wrong-base"
	if _, err := repos.PublishSummaryDocument(ctx, bad); err == nil {
		t.Fatal("changed base published")
	}
	bad = req
	bad.Document = doc
	bad.Document.SourceID = "wrong-source"
	if _, err := repos.PublishSummaryDocument(ctx, bad); err == nil {
		t.Fatal("wrong source in doc published")
	}
	kept, err := repos.Summary.FindByTaskID(task.ID)
	if err != nil || kept.Content != old.Content {
		t.Fatalf("old result lost on failed publication: %+v,%v", kept, err)
	}
	result, err := repos.PublishSummaryDocument(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	projected, err := summarydoc.Markdown(doc)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != projected || result.GeneratedVersion != 1 || result.ContentHashKind != summarydoc.HashKind || result.DocumentJSON == "" {
		t.Fatalf("inconsistent projection: %+v", result)
	}
	if err := db.First(&revision, "id = ?", revision.ID).Error; err != nil || revision.Content != "用户已经修改的内容。" {
		t.Fatalf("generation changed user revision: %+v,%v", revision, err)
	}
	replayed, err := repos.PublishSummaryDocument(ctx, req)
	if err != nil || replayed.GeneratedVersion != 1 {
		t.Fatalf("publication replay = %+v,%v", replayed, err)
	}
	// A new source closes publication rights even if the old job lease is live.
	changed := sourceFixture(t, "检查连接池配置及适用条件。", 4500)
	if _, err := repos.PublishTextSource(ctx, PublishTextSourceRequest{UserID: task.UserID, TaskID: task.ID, ExpectedActiveSourceID: source.ID, Snapshot: changed}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := repos.PublishSummaryDocument(ctx, req); err == nil {
		t.Fatal("old source republished after refresh")
	}
}

func TestSummaryDocumentRejectsUnregisteredAndUninspectedImages(t *testing.T) {
	ctx := context.Background()
	db := summaryRevisionDB(t)
	repos := NewRepositories(db)
	task := createSourceTask(t, db, 92, 9)
	source, err := repos.PublishTextSource(ctx, PublishTextSourceRequest{UserID: task.UserID, TaskID: task.ID, Snapshot: sourceFixture(t, "查看配置画面。", 3000)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	validation, err := repos.SummaryValidationContext(ctx, task.UserID, task.ID, source.ID, source.SourceDigest, "generation-2")
	if err != nil {
		t.Fatal(err)
	}
	capture := int64(1000)
	doc := summarydoc.Document{SchemaVersion: summarydoc.SchemaVersion, DocumentID: "document-2", SourceID: source.ID, SourceDigest: source.SourceDigest, MediaRevision: task.FileMD5, PresentationMode: "image_text", Title: "配置", Overview: "配置说明", Blocks: []summarydoc.Block{{ID: "configuration", Order: 1, BodyMarkdown: "查看配置。", Figures: []summarydoc.Figure{{ID: "figure-1", ScreenshotRef: "resource-1", CaptureMS: &capture, Caption: "配置画面", Alt: "参数设置界面"}}}}}
	if err := summarydoc.Validate(doc, validation); err == nil {
		t.Fatal("unregistered image accepted")
	}
	figure := model.SummaryScreenshotRef{ID: "resource-1", UserID: task.UserID, TaskID: task.ID, GenerationID: "generation-2", SourceID: source.ID, SourceDigest: source.SourceDigest, MediaRevision: task.FileMD5, BlockID: "configuration", ObservationID: "observation-1", ObjectKey: "frames/owned.png", CaptureMS: capture, Status: "ready", Inspected: false, CreatedAt: time.Now()}
	if err := db.Create(&figure).Error; err != nil {
		t.Fatal(err)
	}
	validation, err = repos.SummaryValidationContext(ctx, task.UserID, task.ID, source.ID, source.SourceDigest, "generation-2")
	if err != nil {
		t.Fatal(err)
	}
	if err := summarydoc.Validate(doc, validation); err == nil {
		t.Fatal("uninspected image accepted")
	}
	if err := db.Model(&figure).Update("inspected", true).Error; err != nil {
		t.Fatal(err)
	}
	validation, err = repos.SummaryValidationContext(ctx, task.UserID, task.ID, source.ID, source.SourceDigest, "generation-2")
	if err != nil {
		t.Fatal(err)
	}
	if err := summarydoc.Validate(doc, validation); err != nil {
		t.Fatalf("registered inspected image rejected: %v", err)
	}
	other, err := repos.SummaryValidationContext(ctx, task.UserID, task.ID, source.ID, source.SourceDigest, "generation-3")
	if err != nil {
		t.Fatal(err)
	}
	if err := summarydoc.Validate(doc, other); err == nil {
		t.Fatal("image leaked between generations")
	}
}
