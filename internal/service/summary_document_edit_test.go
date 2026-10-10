package service

import (
	"context"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/secret"
	"vid-lens/internal/repository"
	"vid-lens/internal/summarydoc"
	"vid-lens/internal/textsource"
)

func documentEditFixture(t *testing.T) (*SummaryRevisionService, *gorm.DB, summarydoc.Document, *summaryChatFixture) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = model.Migrate(db); err != nil {
		t.Fatal(err)
	}
	repos := repository.NewRepositories(db)
	codec, err := secret.NewCodec("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	profiles := NewAIProfileService(repos.AIProfile, codec, nil)
	if _, err = profiles.Create(7, validAIProfileRequest()); err != nil {
		t.Fatal(err)
	}
	task := model.VideoTask{ID: 142, UserID: 7, FileMD5: "77777777777777777777777777777777", Filename: "lesson.mp4", FileURL: "videos/lesson.mp4", Status: model.TaskStatusCompleted}
	if err = db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	body := "Old 安装步骤。\n> Old 原话引文。\n代码 `Old` 保持原样。"
	snapshot, err := textsource.Canonicalize(textsource.Snapshot{Kind: textsource.KindASR, Identity: textsource.Identity{Platform: "local", MediaFingerprint: task.FileMD5}, ParserVersion: "asr-test-v1", Cues: []textsource.Cue{{ID: "cue-1", Order: 1, RawText: body, Text: body, TimingMethod: "unknown"}}}, textsource.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	source, err := repos.PublishTextSource(context.Background(), repository.PublishTextSourceRequest{UserID: task.UserID, TaskID: task.ID, Snapshot: snapshot}, nil)
	if err != nil {
		t.Fatal(err)
	}
	doc := summarydoc.Document{SchemaVersion: summarydoc.SchemaVersion, DocumentID: "document-service", SourceID: source.ID, SourceDigest: source.SourceDigest, MediaRevision: task.FileMD5, PresentationMode: "text", Title: "Old 安装说明", Overview: "介绍 Old 的安装。", Blocks: []summarydoc.Block{{ID: "install", Order: 1, Title: "安装", BodyMarkdown: body, SourceRefs: []summarydoc.SourceRef{{SourceID: source.ID, CueIDs: []string{"cue-1"}, TimingMethod: "unknown"}}}}}
	canonical, _ := summarydoc.CanonicalJSON(doc)
	projection, _ := summarydoc.Markdown(doc)
	digest, _ := summarydoc.Digest(doc)
	if err = db.Create(&model.AISummary{TaskID: task.ID, FileMD5: task.FileMD5, DocumentJSON: string(canonical), SchemaVersion: summarydoc.SchemaVersion, SourceID: doc.SourceID, SourceDigest: doc.SourceDigest, Content: projection, ContentDigest: digest, ContentHashKind: summarydoc.HashKind, GenerationID: "generation-service", GeneratedVersion: 1}).Error; err != nil {
		t.Fatal(err)
	}
	chat := &summaryChatFixture{}
	svc := NewSummaryRevisionService(repos, profiles, nil)
	svc.chat = chat
	return svc, db, doc, chat
}

func TestSummaryDocumentEditPreviewApplyUndoPreservesSourceAndQuotes(t *testing.T) {
	svc, db, base, chat := documentEditFixture(t)
	ctx := context.Background()
	view, err := svc.Edit(ctx, 7, 142, "document-preview", SummaryEditInput{Instruction: "Old改为New", Mode: "preview"})
	if err != nil {
		t.Fatal(err)
	}
	if chat.calls != 0 || view.Preview == nil || view.Document == nil || view.Status != "proposed" || len(view.Operations) < 2 {
		t.Fatal("literal document edit did not produce deterministic typed preview")
	}
	if view.Document.Blocks[0].ID != "install" || view.Document.SourceID != base.SourceID || !strings.Contains(view.Document.Blocks[0].BodyMarkdown, "> Old 原话引文。") || !strings.Contains(view.Document.Blocks[0].BodyMarkdown, "`Old`") {
		t.Fatal("literal edit changed original quotes/code/IDs")
	}
	effective, err := svc.Effective(ctx, 7, 142)
	if err != nil {
		t.Fatal(err)
	}
	if effective.Version != 0 || effective.Document.Title != base.Title {
		t.Fatal("preview changed user head")
	}
	if _, err = svc.Apply(ctx, 7, 142, view.ID, 0); err != nil {
		t.Fatal(err)
	}
	effective, err = svc.Effective(ctx, 7, 142)
	if err != nil {
		t.Fatal(err)
	}
	if effective.Version != 1 || effective.Document.Title != "New 安装说明" || effective.ContentHashKind != summarydoc.HashKind {
		t.Fatal("apply did not atomically publish v2")
	}
	if _, err = svc.Undo(ctx, 7, 142, view.ID, 1); err != nil {
		t.Fatal(err)
	}
	effective, err = svc.Effective(ctx, 7, 142)
	if err != nil {
		t.Fatal(err)
	}
	baseHash, _ := summarydoc.Digest(base)
	if effective.Version != 2 || effective.ContentDigest != baseHash || effective.Document.Title != base.Title {
		t.Fatal("undo did not restore full original document")
	}
	var cue model.VideoTextCue
	if err = db.Where("source_id = ?", base.SourceID).First(&cue).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cue.Text, "Old 安装步骤") {
		t.Fatal("term correction changed frozen source text")
	}
	if _, err = svc.Operation(ctx, 8, 142, view.ID); err == nil {
		t.Fatal("other owner fetched document preview")
	}
}

func TestSummaryDocumentTypedPlannerAndUndoRejectsLaterEdits(t *testing.T) {
	svc, _, base, chat := documentEditFixture(t)
	ctx := context.Background()
	hash, _ := summarydoc.Digest(base)
	title := "安装及适用条件"
	chat.response = artifact.JSON(summarydoc.Patch{BaseContentHashKind: summarydoc.HashKind, BaseContentHash: hash, Operations: []summarydoc.Operation{{Op: summarydoc.OpUpdateBlock, BlockID: "install", Title: &title}}})
	first, err := svc.Edit(ctx, 7, 142, "document-typed", SummaryEditInput{Instruction: "完善章节标题", Mode: "apply"})
	if err != nil {
		t.Fatal(err)
	}
	if chat.calls != 1 || first.Document.Blocks[0].Title != title {
		t.Fatal("typed planner was not applied")
	}
	second, err := svc.Edit(ctx, 7, 142, "document-later", SummaryEditInput{Instruction: "Old改为New", ExpectedRevision: 1, Mode: "apply"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != "committed" {
		t.Fatal("later edit not committed")
	}
	if _, err = svc.Undo(ctx, 7, 142, first.ID, 2); err == nil {
		t.Fatal("undo silently discarded later changes")
	}
	effective, err := svc.Effective(ctx, 7, 142)
	if err != nil {
		t.Fatal(err)
	}
	if effective.Document.Title != "New 安装说明" || effective.Document.Blocks[0].Title != title {
		t.Fatal("conflicting undo changed document")
	}
}

func TestSummaryDocumentPlannerRepairDoesNotChangeProtectedCode(t *testing.T) {
	svc, _, base, chat := documentEditFixture(t)
	ctx := context.Background()
	hash, _ := summarydoc.Digest(base)
	body := strings.Replace(base.Blocks[0].BodyMarkdown, "`Old`", "`New`", 1)
	chat.response = artifact.JSON(summarydoc.Patch{BaseContentHashKind: summarydoc.HashKind, BaseContentHash: hash, Operations: []summarydoc.Operation{{Op: summarydoc.OpUpdateBlock, BlockID: "install", BodyMarkdown: &body}}})
	if _, err := svc.Edit(ctx, 7, 142, "document-protected", SummaryEditInput{Instruction: "完善安装解释", Mode: "apply"}); err == nil {
		t.Fatal("planner changed protected code")
	}
	if chat.calls != 2 {
		t.Fatalf("repair calls=%d, want bounded2", chat.calls)
	}
	effective, err := svc.Effective(ctx, 7, 142)
	if err != nil {
		t.Fatal(err)
	}
	if effective.Version != 0 || effective.Document.Blocks[0].BodyMarkdown != base.Blocks[0].BodyMarkdown {
		t.Fatal("invalid patch partially published")
	}
}

func TestLegacyContentWithV2BaselineStillUsesLegacyPlanner(t *testing.T) {
	svc, db, base, chat := documentEditFixture(t)
	ctx := context.Background()
	generatedHash, _ := summarydoc.Digest(base)
	manual := model.SummaryRevision{ID: "legacy-user-revision", UserID: 7, TaskID: 142, Version: 1, Content: "Old 的用户修订。", ContentDigest: artifact.Hash("Old 的用户修订。"), ContentHashKind: model.SummaryHashMarkdown, BaseGeneratedHash: generatedHash, BaseGeneratedHashKind: summarydoc.HashKind, Origin: "keep_revision"}
	if err := db.Create(&manual).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SummaryRevisionHead{UserID: 7, TaskID: 142, Version: 1, CurrentRevisionID: manual.ID}).Error; err != nil {
		t.Fatal(err)
	}
	view, err := svc.Edit(ctx, 7, 142, "legacy-on-v2-base", SummaryEditInput{Instruction: "Old改为New", ExpectedRevision: 1, Mode: "apply"})
	if err != nil {
		t.Fatal(err)
	}
	if view.Document != nil || len(view.Operations) != 0 || chat.calls != 0 {
		t.Fatal("v2 baseline coerced legacy content into document planner")
	}
	effective, err := svc.Effective(ctx, 7, 142)
	if err != nil {
		t.Fatal(err)
	}
	if effective.Document != nil || effective.Content != "New 的用户修订。" || effective.ContentHashKind != model.SummaryHashMarkdown || effective.BaseHashKind != summarydoc.HashKind {
		t.Fatal("legacy revision or accepted baseline kind lost")
	}
}
