package repository

import (
	"context"
	"testing"

	"gorm.io/gorm"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/summarydoc"
)

func revisionDocumentFixture(t *testing.T, db *gorm.DB) (*Repositories, model.VideoTask, summarydoc.Document) {
	t.Helper()
	repos := NewRepositories(db)
	task := createSourceTask(t, db, 141, 17)
	source, err := repos.PublishTextSource(context.Background(), PublishTextSourceRequest{UserID: task.UserID, TaskID: task.ID, Snapshot: sourceFixture(t, "旧名称配置说明。", 3000)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	start, end := int64(0), int64(3000)
	doc := summarydoc.Document{SchemaVersion: summarydoc.SchemaVersion, DocumentID: "document-revision", SourceID: source.ID, SourceDigest: source.SourceDigest, MediaRevision: task.FileMD5, PresentationMode: "text", Title: "配置说明", Overview: "解释配置条件。", Blocks: []summarydoc.Block{{ID: "config", Order: 1, Title: "配置", BodyMarkdown: "旧名称配置说明。", SourceRefs: []summarydoc.SourceRef{{SourceID: source.ID, CueIDs: []string{"cue-1"}, StartMS: &start, EndMS: &end, TimingMethod: "subtitle_cue"}}}}}
	return repos, task, doc
}

func seedRevisionDocument(t *testing.T, db *gorm.DB, task model.VideoTask, doc summarydoc.Document) model.AISummary {
	t.Helper()
	canonical, err := summarydoc.CanonicalJSON(doc)
	if err != nil {
		t.Fatal(err)
	}
	content, _ := summarydoc.Markdown(doc)
	hash, _ := summarydoc.Digest(doc)
	row := model.AISummary{TaskID: task.ID, FileMD5: task.FileMD5, DocumentJSON: string(canonical), SchemaVersion: summarydoc.SchemaVersion, SourceID: doc.SourceID, SourceDigest: doc.SourceDigest, Content: content, ContentDigest: hash, ContentHashKind: summarydoc.HashKind, GenerationID: "generation-revision", GeneratedVersion: 1}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	return row
}

func TestSummaryDocumentRevisionAtomicHashKindsAndUndo(t *testing.T) {
	runSummaryDocumentRevisionAtomic(t, summaryRevisionDB(t))
}
func TestPostgresSummaryDocumentRevisionAtomicHashKindsAndUndo(t *testing.T) {
	runSummaryDocumentRevisionAtomic(t, openPostgresRepositoryTestDB(t).db)
}
func runSummaryDocumentRevisionAtomic(t *testing.T, db *gorm.DB) {
	repos, task, doc := revisionDocumentFixture(t, db)
	generated := seedRevisionDocument(t, db, task, doc)
	ctx := context.Background()
	effective, err := repos.SummaryRevision.Effective(ctx, task.UserID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if effective.Document == nil || effective.ContentDigest != generated.ContentDigest {
		t.Fatal("document not authoritative")
	}
	op, run := summaryEditFixture(task.UserID, task.ID, effective.Content)
	op.BaseContentHash = effective.ContentDigest
	op.BaseContentHashKind = summarydoc.HashKind
	op.BaseDocumentJSON = effective.DocumentJSON
	op.BaseGeneratedHash = effective.BaseHash
	op.BaseGeneratedHashKind = effective.BaseHashKind
	op.BaseGenerationID = effective.GenerationID
	op.BaseSourceID = doc.SourceID
	op.BaseSourceDigest = doc.SourceDigest
	if _, err = repos.SummaryRevision.Begin(ctx, op, run); err != nil {
		t.Fatal(err)
	}
	nextText := "新名称配置说明。"
	patch := summarydoc.Patch{BaseContentHashKind: summarydoc.HashKind, BaseContentHash: op.BaseContentHash, Operations: []summarydoc.Operation{{Op: summarydoc.OpUpdateBlock, BlockID: "config", BodyMarkdown: &nextText}}}
	validation, err := repos.SummaryRevision.DocumentContext(ctx, task.UserID, task.ID, op.BaseDocumentJSON, op.BaseGenerationID)
	if err != nil {
		t.Fatal(err)
	}
	next, _, err := summarydoc.ApplyPatch(doc, patch, validation)
	if err != nil {
		t.Fatal(err)
	}
	canonical, _ := summarydoc.CanonicalJSON(next)
	if _, err = repos.SummaryRevision.Commit(ctx, op.ID, nextText, artifact.JSON(patch)); err == nil {
		t.Fatal("v2 accepted a text-only publication")
	}
	revision, err := repos.SummaryRevision.CommitDocument(ctx, op.ID, string(canonical), artifact.JSON(patch))
	if err != nil {
		t.Fatal(err)
	}
	projection, _ := summarydoc.Markdown(next)
	if revision.Content != projection || revision.DocumentJSON != string(canonical) || revision.ContentHashKind != summarydoc.HashKind || revision.BaseGeneratedHashKind != summarydoc.HashKind {
		t.Fatal("document/projection/hashkind transaction inconsistent")
	}
	effective, err = repos.SummaryRevision.Effective(ctx, task.UserID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	undone, err := repos.SummaryRevision.Undo(ctx, task.UserID, task.ID, op.ID, 1, effective.ContentDigest, "wrong text ignored")
	if err != nil {
		t.Fatal(err)
	}
	if undone.DocumentJSON != generated.DocumentJSON || undone.Content != generated.Content || undone.ContentDigest != generated.ContentDigest {
		t.Fatal("undo did not restore complete document")
	}
	if _, err = repos.SummaryRevision.Effective(ctx, 18, task.ID); err == nil {
		t.Fatal("cross-user document read")
	}
}

func TestLegacyRevisionAcceptsNewDocumentBaseWithoutChangingFormat(t *testing.T) {
	runLegacyRevisionDocumentBase(t, summaryRevisionDB(t))
}
func TestPostgresLegacyRevisionAcceptsNewDocumentBaseWithoutChangingFormat(t *testing.T) {
	runLegacyRevisionDocumentBase(t, openPostgresRepositoryTestDB(t).db)
}
func runLegacyRevisionDocumentBase(t *testing.T, db *gorm.DB) {
	repos, task, doc := revisionDocumentFixture(t, db)
	ctx := context.Background()
	legacy := model.AISummary{TaskID: task.ID, FileMD5: task.FileMD5, Content: "旧摘要。"}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	op, run := summaryEditFixture(task.UserID, task.ID, legacy.Content)
	if _, err := repos.SummaryRevision.Begin(ctx, op, run); err != nil {
		t.Fatal(err)
	}
	manual, err := repos.SummaryRevision.Commit(ctx, op.ID, "用户旧格式修订。", `{"edits":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	canonical, _ := summarydoc.CanonicalJSON(doc)
	projection, _ := summarydoc.Markdown(doc)
	digest, _ := summarydoc.Digest(doc)
	if err = db.Model(&legacy).Updates(map[string]any{"document_json": string(canonical), "schema_version": summarydoc.SchemaVersion, "source_id": doc.SourceID, "source_digest": doc.SourceDigest, "content": projection, "content_digest": digest, "content_hash_kind": summarydoc.HashKind, "generation_id": "generation-revision"}).Error; err != nil {
		t.Fatal(err)
	}
	effective, err := repos.SummaryRevision.Effective(ctx, task.UserID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if effective.SourceStatus != "needs_merge" || effective.Document != nil || effective.ContentHashKind != model.SummaryHashMarkdown || effective.CurrentGeneratedHashKind != summarydoc.HashKind {
		t.Fatal("legacy/v2 baseline conflated")
	}
	kept, err := repos.SummaryRevision.ResolveBase(ctx, task.UserID, task.ID, 1, "keep_revision")
	if err != nil {
		t.Fatal(err)
	}
	if kept.Content != manual.Content || kept.DocumentJSON != "" || kept.ContentHashKind != model.SummaryHashMarkdown || kept.BaseGeneratedHashKind != summarydoc.HashKind || kept.BaseGeneratedHash != digest {
		t.Fatal("keep changed user format or baseline kind")
	}
	// Change only the generated document. A subsequent explicit adoption copies
	// the whole v2 document and its compatible Markdown atomically.
	doc.Overview = "新版本概览。"
	canonical, _ = summarydoc.CanonicalJSON(doc)
	projection, _ = summarydoc.Markdown(doc)
	digest, _ = summarydoc.Digest(doc)
	if err = db.Model(&legacy).Updates(map[string]any{"document_json": string(canonical), "content": projection, "content_digest": digest}).Error; err != nil {
		t.Fatal(err)
	}
	adopted, err := repos.SummaryRevision.ResolveBase(ctx, task.UserID, task.ID, 2, "use_generated")
	if err != nil {
		t.Fatal(err)
	}
	if adopted.DocumentJSON != string(canonical) || adopted.Content != projection || adopted.ContentHashKind != summarydoc.HashKind || adopted.ContentDigest != digest {
		t.Fatal("adoption lost document")
	}
}

func TestSummaryDocumentRevisionCannotBorrowMissingOrOtherOwnerFigures(t *testing.T) {
	runSummaryRevisionFigureAuthorization(t, summaryRevisionDB(t))
}
func TestPostgresSummaryDocumentRevisionCannotBorrowMissingOrOtherOwnerFigures(t *testing.T) {
	runSummaryRevisionFigureAuthorization(t, openPostgresRepositoryTestDB(t).db)
}
func runSummaryRevisionFigureAuthorization(t *testing.T, db *gorm.DB) {
	repos, task, doc := revisionDocumentFixture(t, db)
	capture := int64(1000)
	doc.PresentationMode = "image_text"
	doc.Blocks[0].Figures = []summarydoc.Figure{{ID: "figure-config", ScreenshotRef: "screen-config", CaptureMS: &capture, Caption: "配置界面", Alt: "连接池配置"}}
	canonical, _ := summarydoc.CanonicalJSON(doc)
	ctx := context.Background()
	if _, err := repos.SummaryRevision.DocumentContext(ctx, task.UserID, task.ID, string(canonical), "generation-revision"); err == nil {
		t.Fatal("unregistered base screenshot accepted")
	}
	row := model.SummaryScreenshotRef{ID: "screen-config", UserID: 18, TaskID: task.ID, GenerationID: "generation-revision", SourceID: doc.SourceID, SourceDigest: doc.SourceDigest, MediaRevision: doc.MediaRevision, BlockID: "config", CaptureMS: capture, Inspected: true, Status: "ready"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repos.SummaryRevision.DocumentContext(ctx, task.UserID, task.ID, string(canonical), "generation-revision"); err == nil {
		t.Fatal("other owner screenshot accepted")
	}
	if err := db.Model(&row).Update("user_id", task.UserID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repos.SummaryRevision.DocumentContext(ctx, task.UserID, task.ID, string(canonical), "generation-revision"); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&row).Update("status", "revoked").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repos.SummaryRevision.DocumentContext(ctx, task.UserID, task.ID, string(canonical), "generation-revision"); err == nil {
		t.Fatal("revoked figure accepted")
	}
}

func TestSummaryRevisionCacheFallbackExcludesPrivateDocuments(t *testing.T) {
	runSummaryRevisionPrivateCache(t, summaryRevisionDB(t))
}
func TestPostgresSummaryRevisionCacheFallbackExcludesPrivateDocuments(t *testing.T) {
	runSummaryRevisionPrivateCache(t, openPostgresRepositoryTestDB(t).db)
}
func runSummaryRevisionPrivateCache(t *testing.T, db *gorm.DB) {
	_, task, doc := revisionDocumentFixture(t, db)
	seedRevisionDocument(t, db, task, doc)
	other := model.VideoTask{ID: 142, UserID: 18, FileMD5: task.FileMD5, Filename: "other.mp4", Status: model.TaskStatusCompleted}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	effective, err := NewSummaryRevisionRepository(db).Effective(context.Background(), other.UserID, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if effective.Generated != nil || effective.Content != "" {
		t.Fatal("private v2 result leaked via legacy cache")
	}
}
