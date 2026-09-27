package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

func TestStudyFlowPersistsPositionAndImportsOnlyMappedCompletedAnswer(t *testing.T) {
	svc, db, _ := artifactFixture(t, artifactModelResponse)
	ctx := context.Background()
	source, err := svc.Source(ctx, 7, 42)
	if err != nil || len(source.Evidence) == 0 {
		t.Fatalf("source: %+v %v", source, err)
	}
	evidence := source.Evidence[0]
	body := artifact.Body{SchemaVersion: 1, Kind: "study", Title: "事务教程", Blocks: []artifact.Block{{BlockID: "one", Type: "concept", Title: "原子性", Content: "先核对", ClaimOrigin: "user", EvidenceRefs: []artifact.Ref{}}}, Warnings: []string{}}
	detail, err := svc.Create(ctx, 7, []int64{42}, body)
	if err != nil || detail.Version == nil {
		t.Fatalf("create: %+v %v", detail, err)
	}
	position, err := svc.SaveLearningPosition(ctx, 7, 0, 42, detail.ID, detail.Version.ID, "one", 0)
	if err != nil || position.Revision != 1 {
		t.Fatalf("position: %+v %v", position, err)
	}
	newer, err := svc.SaveLearningPosition(ctx, 7, 1, 42, "", "", "", 60000)
	if err != nil || newer.Revision != 2 {
		t.Fatalf("newer: %+v %v", newer, err)
	}
	_, err = svc.SaveLearningPosition(ctx, 7, 1, 42, detail.ID, detail.Version.ID, "one", 0)
	requireArtifactCode(t, err, "position_conflict")
	current, err := svc.LearningPosition(ctx, 7)
	if err != nil || current.TimeMS != 60000 || current.ArtifactID != "" {
		t.Fatalf("late write won: %+v %v", current, err)
	}
	_, err = svc.SaveLearningPosition(ctx, 7, 2, 42, detail.ID, detail.Version.ID, "one", 0)
	if err != nil {
		t.Fatal(err)
	}
	contextBlock, err := svc.BlockContext(ctx, 7, detail.ID, detail.Version.ID, "one")
	if err != nil || contextBlock.TaskID != 42 || contextBlock.Block.Content != "先核对" {
		t.Fatalf("context: %+v %v", contextBlock, err)
	}
	if _, err = svc.BlockContext(ctx, 8, detail.ID, detail.Version.ID, "one"); err == nil {
		t.Fatal("other user read block")
	}
	if err = db.Create(&model.ChatSession{ID: 90, UserID: 7, TaskID: 42, ScopeType: model.ChatScopeVideo, Title: "追问"}).Error; err != nil {
		t.Fatal(err)
	}
	stable := strings.TrimPrefix(evidence.SourceIdentity, evidence.Modality+":")
	makeMessage := func(id int64, hash string) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"citations": []any{map[string]any{"citation_id": "C1", "task_id": 42, "source_refs": []any{map[string]any{"source_type": evidence.Modality, "stable_id": stable, "content_hash": hash}}}}})
		snapshot := string(raw)
		if err := db.Create(&model.ChatMessage{ID: id, UserID: 7, SessionID: 90, Role: "assistant", Content: "提交后才生效 [C1]", RetrievalSnapshot: &snapshot}).Error; err != nil {
			t.Fatal(err)
		}
	}
	makeMessage(91, evidence.ContentHash)
	preview, err := svc.PreviewAnswer(ctx, 7, 91, detail.ID, "one", detail.HeadVersion)
	if err != nil || len(preview.Mapped) != 1 || len(preview.Unmapped) != 0 || preview.Mapped[0].CitationID != "C1" || preview.Mapped[0].EvidenceID != evidence.ID {
		t.Fatalf("mapping: %+v %v", preview, err)
	}
	imported, err := svc.ImportAnswer(ctx, 7, 91, detail.ID, "one", detail.HeadVersion, "study-key-1", false)
	if err != nil || imported.HeadVersion != 2 || len(imported.Version.Body.Blocks) != 2 {
		t.Fatalf("import: %+v %v", imported, err)
	}
	added := imported.Version.Body.Blocks[1]
	if added.ClaimOrigin != "user" || added.EvidenceRefs[0].CitationID != "C1" {
		t.Fatalf("citation link lost: %+v", added)
	}
	second, err := svc.ImportAnswer(ctx, 7, 91, detail.ID, "one", 1, "study-key-1", false)
	if err != nil || second.HeadVersion != 2 {
		t.Fatalf("duplicate: %+v %v", second, err)
	}
	_, err = svc.ImportAnswer(ctx, 7, 91, detail.ID, "one", 1, "study-key-2", false)
	requireArtifactCode(t, err, "version_conflict")
	pos, err := svc.LearningPosition(ctx, 7)
	if err != nil || pos.Fallback != "version_changed" || pos.BlockID != "one" {
		t.Fatalf("version fallback: %+v %v", pos, err)
	}
	// Removing a saved block falls back to a real block in the new version.
	removed := imported.Version.Body
	removed.Blocks = []artifact.Block{added}
	revised, err := svc.Save(ctx, 7, detail.ID, 2, &removed, "")
	if err != nil {
		t.Fatal(err)
	}
	pos, err = svc.LearningPosition(ctx, 7)
	if err != nil || pos.Fallback != "block_removed" || pos.BlockID != revised.Version.Body.Blocks[0].BlockID {
		t.Fatalf("block fallback: %+v %v", pos, err)
	}
}

func TestStudyFlowRejectsUnmappedAndRevokedSources(t *testing.T) {
	svc, db, _ := artifactFixture(t, artifactModelResponse)
	ctx := context.Background()
	source, err := svc.Source(ctx, 7, 42)
	if err != nil {
		t.Fatal(err)
	}
	body := artifact.Body{SchemaVersion: 1, Kind: "study", Title: "笔记", Blocks: []artifact.Block{{BlockID: "one", Type: "note", Title: "一", ClaimOrigin: "user", EvidenceRefs: []artifact.Ref{}}}, Warnings: []string{}}
	detail, err := svc.Create(ctx, 7, []int64{42}, body)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&model.ChatSession{ID: 100, UserID: 7, TaskID: 42, ScopeType: model.ChatScopeVideo}).Error; err != nil {
		t.Fatal(err)
	}
	snapshot := `{"citations":[{"citation_id":"C1","task_id":42,"source_refs":[{"source_type":"transcript","stable_id":"missing","content_hash":"wrong"}]}]}`
	if err = db.Create(&model.ChatMessage{ID: 101, UserID: 7, SessionID: 100, Role: "assistant", Content: "需要核对 [C1]", RetrievalSnapshot: &snapshot}).Error; err != nil {
		t.Fatal(err)
	}
	preview, err := svc.PreviewAnswer(ctx, 7, 101, detail.ID, "one", 1)
	if err != nil || len(preview.Unmapped) != 1 || len(preview.Mapped) != 0 {
		t.Fatalf("unmapped: %+v %v", preview, err)
	}
	withoutCitations := `{"citations":[]}`
	if err = db.Create(&model.ChatMessage{ID: 102, UserID: 7, SessionID: 100, Role: "assistant", Content: "无引用的概览", RetrievalSnapshot: &withoutCitations}).Error; err != nil {
		t.Fatal(err)
	}
	preview, err = svc.PreviewAnswer(ctx, 7, 102, detail.ID, "one", 1)
	if err != nil || len(preview.Unmapped) != 1 || preview.Unmapped[0] != "回答没有可核对的聊天引用" {
		t.Fatalf("uncited answer must need explicit personal choice: %+v %v", preview, err)
	}
	_, err = svc.ImportAnswer(ctx, 7, 101, detail.ID, "one", 1, "unmapped-key", false)
	requireArtifactCode(t, err, "citations_unmapped")
	personal, err := svc.ImportAnswer(ctx, 7, 101, detail.ID, "one", 1, "personal-key", true)
	if err != nil || len(personal.Version.Body.Blocks[1].EvidenceRefs) != 0 {
		t.Fatalf("personal: %+v %v", personal, err)
	}
	now := time.Now()
	if err = db.Model(&model.SourceManifest{}).Where("id=?", source.ManifestID).Update("revoked_at", now).Error; err != nil {
		t.Fatal(err)
	}
	_, err = svc.PreviewAnswer(ctx, 7, 101, detail.ID, "one", 2)
	requireArtifactCode(t, err, "source_deleted")
}

func TestArtifactSourceReadsIdenticalMediaDedupTranscript(t *testing.T) {
	svc, db, _ := artifactFixture(t, artifactModelResponse)
	if err := db.Create(&model.VideoTask{ID: 43, UserID: 8, FileMD5: "fixture", Filename: "同一教程", Status: model.TaskStatusCompleted}).Error; err != nil {
		t.Fatal(err)
	}
	source, err := svc.Source(context.Background(), 8, 43)
	if err != nil || len(source.Evidence) == 0 || source.Evidence[0].Content == "" || source.Evidence[0].SourceID != 43 {
		t.Fatalf("dedup source: %+v %v", source, err)
	}
	if _, err = svc.Source(context.Background(), 9, 43); err == nil {
		t.Fatal("unowned dedup source readable")
	}
}
