package repository

import (
	"context"
	"encoding/json"
	"gorm.io/gorm"
	"strings"
	"testing"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/summarydoc"
)

func TestSummarySelectionsVersionUnicodeScopeAndFrozenHistory(t *testing.T) {
	runSummarySelections(t, summaryRevisionDB(t))
}
func TestPostgresSummarySelectionsVersionUnicodeScopeAndFrozenHistory(t *testing.T) {
	runSummarySelections(t, openPostgresRepositoryTestDB(t).db)
}
func runSummarySelections(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	repos, task, doc := revisionDocumentFixture(t, db)
	doc.Blocks[0].BodyMarkdown = "中文😀 **连接池** [配置](https://example.test)"
	row := seedRevisionDocument(t, db, task, doc)
	session := model.ChatSession{UserID: task.UserID, TaskID: task.ID, ScopeType: model.ChatScopeVideo}
	if err := repos.Chat.CreateSession(&session); err != nil {
		t.Fatal(err)
	}
	version := int64(1)
	block, err := repos.SummaryBlockContext(ctx, task.UserID, task.ID, "config", model.SummaryVersionRef{GeneratedVersion: &version})
	if err != nil {
		t.Fatal(err)
	}
	if block.CanonicalText != "中文😀 连接池 配置" {
		t.Fatal(block.CanonicalText)
	}
	ref := model.SummaryContextRef{Kind: "summary_selection", TaskID: task.ID, VersionRef: block.VersionRef, DocumentDigest: block.DocumentDigest, BlockID: block.BlockID, BlockDigest: block.BlockDigest, TextStart: 2, TextEnd: 3, Quote: "😀"}
	frozen, err := repos.FreezeSummarySelections(ctx, task.UserID, session.ID, []model.SummaryContextRef{ref})
	if err != nil || frozen[0].Quote != "😀" || frozen[0].Provenance != "derived_summary" {
		t.Fatal(frozen, err)
	}
	bad := ref
	bad.TextEnd = 4
	if _, err = repos.FreezeSummarySelections(ctx, task.UserID, session.ID, []model.SummaryContextRef{bad}); err == nil {
		t.Fatal("UTF16 offset accepted")
	}
	bad = ref
	bad.Quote = "伪造"
	if _, err = repos.FreezeSummarySelections(ctx, task.UserID, session.ID, []model.SummaryContextRef{bad}); err == nil {
		t.Fatal("forged quote accepted")
	}
	if _, err = repos.FreezeSummarySelections(ctx, task.UserID, session.ID, []model.SummaryContextRef{ref, ref, ref, ref}); err == nil {
		t.Fatal("selection count cap absent")
	}
	other := createSourceTask(t, db, 151, task.UserID)
	bad = ref
	bad.TaskID = other.ID
	if _, err = repos.FreezeSummarySelections(ctx, task.UserID, session.ID, []model.SummaryContextRef{bad}); err == nil {
		t.Fatal("attachment widened video scope")
	}
	if _, err = repos.FreezeSummarySelections(ctx, task.UserID+1, session.ID, []model.SummaryContextRef{ref}); err == nil {
		t.Fatal("other owner accepted selection")
	}
	revision := model.SummaryRevision{ID: "snapshot-revision", UserID: task.UserID, TaskID: task.ID, Version: 1, Content: row.Content, DocumentJSON: row.DocumentJSON, ContentDigest: row.ContentDigest, ContentHashKind: row.ContentHashKind, GenerationID: row.GenerationID, SourceID: row.SourceID, SourceDigest: row.SourceDigest, Origin: "edit"}
	if err = db.Create(&revision).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Model(&row).Update("generated_version", 2).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = repos.FreezeSummarySelections(ctx, task.UserID, session.ID, []model.SummaryContextRef{ref}); err == nil {
		t.Fatal("discarded generated version substituted newest")
	}
	if err = repos.AuthorizeFrozenAnnotations(ctx, task.UserID, session.ID, frozen); err != nil {
		t.Fatal("accepted snapshot re-read current body", err)
	}
	old, err := repos.SummaryBlockContext(ctx, task.UserID, task.ID, "config", model.SummaryVersionRef{RevisionID: revision.ID})
	if err != nil || old.CanonicalText != block.CanonicalText {
		t.Fatal("saved revision no longer selectable", err)
	}
	raw := artifact.JSON(frozen)
	question := &model.ChatMessage{UserID: task.UserID, SessionID: session.ID, Role: "user", Content: "这个怎么设置", ContextAnnotationsJSON: &raw}
	answer := &model.ChatMessage{UserID: task.UserID, SessionID: session.ID, Role: "assistant", Content: "回答"}
	if err = repos.Chat.CreateExchange(task.UserID, question, answer, nil); err != nil {
		t.Fatal(err)
	}
	saved, err := repos.Chat.ListMessages(task.UserID, session.ID)
	if err != nil || saved[0].ContextAnnotationsJSON == nil || !strings.Contains(*saved[0].ContextAnnotationsJSON, "😀") {
		t.Fatal("snapshot not persisted", err)
	}
	var roundtrip []model.ContextAnnotation
	if err = json.Unmarshal([]byte(*saved[0].ContextAnnotationsJSON), &roundtrip); err != nil || roundtrip[0].DocumentDigest != ref.DocumentDigest {
		t.Fatal("snapshot changed", err)
	}
	if err = db.Delete(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err = repos.AuthorizeFrozenAnnotations(ctx, task.UserID, session.ID, frozen); err == nil {
		t.Fatal("history bypassed deleted video")
	}
}
func TestSummaryScreenshotSelectionRequiresRegisteredBoundImage(t *testing.T) {
	db := summaryRevisionDB(t)
	repos, task, doc := revisionDocumentFixture(t, db)
	ctx := context.Background()
	capture := int64(1000)
	image := model.SummaryScreenshotRef{ID: "registered-frame", UserID: task.UserID, TaskID: task.ID, GenerationID: "generation-revision", SourceID: doc.SourceID, SourceDigest: doc.SourceDigest, MediaRevision: doc.MediaRevision, BlockID: "config", ObservationID: "observed-frame", CaptureMS: capture, Inspected: true, Status: "ready", ObjectKey: "private/object"}
	if err := db.Create(&image).Error; err != nil {
		t.Fatal(err)
	}
	doc.PresentationMode = "image_text"
	doc.Blocks[0].Figures = []summarydoc.Figure{{ID: "figure-config", ScreenshotRef: image.ID, CaptureMS: &capture, Caption: "**截图**图注", Alt: "配置画面", Supports: "配置项"}}
	seedRevisionDocument(t, db, task, doc)
	session := model.ChatSession{UserID: task.UserID, TaskID: task.ID, ScopeType: model.ChatScopeVideo}
	if err := repos.Chat.CreateSession(&session); err != nil {
		t.Fatal(err)
	}
	version := int64(1)
	block, err := repos.SummaryBlockContext(ctx, task.UserID, task.ID, "config", model.SummaryVersionRef{GeneratedVersion: &version})
	if err != nil {
		t.Fatal(err)
	}
	if len(block.Figures) != 1 || block.Figures[0].CanonicalCaption != "截图图注" {
		t.Fatal("caption projection not authoritative", block.Figures)
	}
	ref := model.SummaryContextRef{Kind: "summary_screenshot", TaskID: task.ID, VersionRef: block.VersionRef, DocumentDigest: block.DocumentDigest, BlockID: block.BlockID, BlockDigest: block.BlockDigest, TextStart: 0, TextEnd: len([]rune(block.CanonicalText)), Quote: block.CanonicalText, ScreenshotRef: image.ID}
	accepted, err := repos.FreezeSummarySelections(ctx, task.UserID, session.ID, []model.SummaryContextRef{ref})
	if err != nil || len(accepted[0].Images) != 1 || accepted[0].Images[0].InputMode != "caption_only" || accepted[0].Images[0].ObservationID != image.ObservationID {
		t.Fatal(accepted, err)
	}
	raw := artifact.JSON(accepted)
	if strings.Contains(raw, "private/object") {
		t.Fatal("private image location exposed")
	}
	ref.ScreenshotRef = "https://arbitrary.test/frame"
	if _, err = repos.FreezeSummarySelections(ctx, task.UserID, session.ID, []model.SummaryContextRef{ref}); err == nil {
		t.Fatal("arbitrary image accepted")
	}
	if err = db.Model(&image).Update("status", "revoked").Error; err != nil {
		t.Fatal(err)
	}
	if err = repos.AuthorizeFrozenAnnotations(ctx, task.UserID, session.ID, accepted); err == nil {
		t.Fatal("revoked image replay accepted")
	}
}
