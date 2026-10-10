package repository

import (
	"context"
	"testing"

	"gorm.io/gorm"
	"vid-lens/internal/model"
	"vid-lens/internal/summarydoc"
)

func TestSummaryGenerationEffectiveSourceStatusPreservesOldBody(t *testing.T) {
	runEffectiveSummarySourceStatus(t, summaryRevisionDB(t))
}
func TestPostgresSummaryGenerationEffectiveSourceStatusPreservesOldBody(t *testing.T) {
	runEffectiveSummarySourceStatus(t, openPostgresRepositoryTestDB(t).db)
}
func runEffectiveSummarySourceStatus(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	repos := NewRepositories(db)
	task := createSourceTask(t, db, 91, 9)
	source, err := repos.PublishTextSource(ctx, PublishTextSourceRequest{UserID: task.UserID, TaskID: task.ID, Snapshot: sourceFixture(t, "第一版完整来源。", 3000)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	doc := summarydoc.Document{SchemaVersion: summarydoc.SchemaVersion, DocumentID: "status-generation", SourceID: source.ID, SourceDigest: source.SourceDigest, MediaRevision: task.FileMD5, PresentationMode: "text", Title: "旧原稿", Overview: "原稿可读。", Blocks: []summarydoc.Block{{ID: "kept", Order: 0, Title: "保留", BodyMarkdown: "用户仍可阅读此段。"}}}
	canonical, err := summarydoc.CanonicalJSON(doc)
	if err != nil {
		t.Fatal(err)
	}
	content, _ := summarydoc.Markdown(doc)
	digest, _ := summarydoc.Digest(doc)
	generated := model.AISummary{TaskID: task.ID, FileMD5: task.FileMD5, Content: content, DocumentJSON: string(canonical), SchemaVersion: summarydoc.SchemaVersion, SourceID: source.ID, SourceDigest: source.SourceDigest, ContentDigest: digest, ContentHashKind: summarydoc.HashKind, GeneratedVersion: 1, GenerationID: "status-generation"}
	if err = db.Create(&generated).Error; err != nil {
		t.Fatal(err)
	}
	before, err := repos.SummaryRevision.Effective(ctx, task.UserID, task.ID)
	if err != nil || before.SourceStatus != "current" {
		t.Fatalf("initial status=%+v %v", before, err)
	}
	next, err := repos.PublishTextSource(ctx, PublishTextSourceRequest{UserID: task.UserID, TaskID: task.ID, ExpectedActiveSourceID: source.ID, Snapshot: sourceFixture(t, "替换后完整来源。", 4500)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	after, err := repos.SummaryRevision.Effective(ctx, task.UserID, task.ID)
	if err != nil || after.SourceStatus != "source_changed" || after.Content != content || after.Generated.ContentDigest != digest {
		t.Fatalf("source replacement lost old readable result %+v %v", after, err)
	}
	revision := model.SummaryRevision{ID: "status-user-revision", UserID: task.UserID, TaskID: task.ID, Version: 1, Content: content, DocumentJSON: string(canonical), ContentHashKind: summarydoc.HashKind, ContentDigest: digest, BaseGeneratedHash: digest, BaseGeneratedHashKind: summarydoc.HashKind, Origin: "manual"}
	if err = db.Create(&revision).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&model.SummaryRevisionHead{UserID: task.UserID, TaskID: task.ID, Version: 1, CurrentRevisionID: revision.ID}).Error; err != nil {
		t.Fatal(err)
	}
	// A newer generated document cannot make the still-selected user revision's
	// old frozen source current. Its base conflict metadata stays intact.
	doc.SourceID = next.ID
	doc.SourceDigest = next.SourceDigest
	doc.Title = "新原稿"
	newJSON, _ := summarydoc.CanonicalJSON(doc)
	newDigest, _ := summarydoc.Digest(doc)
	newContent, _ := summarydoc.Markdown(doc)
	if err = db.Model(&generated).Updates(map[string]any{"source_id": next.ID, "source_digest": next.SourceDigest, "document_json": string(newJSON), "content": newContent, "content_digest": newDigest, "generated_version": 2}).Error; err != nil {
		t.Fatal(err)
	}
	revised, err := repos.SummaryRevision.Effective(ctx, task.UserID, task.ID)
	if err != nil || revised.SourceStatus != "source_changed" || revised.Content != content || revised.BaseHash != digest || revised.CurrentGeneratedHash != newDigest {
		t.Fatalf("typed revision source/merge facts lost %+v %v", revised, err)
	}
	// When the active source is again the revision's own source, the existing
	// needs_merge fact remains the status; this change does not clear that state.
	if err = db.Model(task).Update("active_text_source_id", source.ID).Error; err != nil {
		t.Fatal(err)
	}
	merged, err := repos.SummaryRevision.Effective(ctx, task.UserID, task.ID)
	if err != nil || merged.SourceStatus != "needs_merge" {
		t.Fatalf("needs_merge changed %+v %v", merged, err)
	}
}
