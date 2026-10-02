package service

import (
	"context"
	"encoding/json"
	"testing"

	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

func TestNativeAndGapSentenceCitationsImportIntoSameStudySource(t *testing.T) {
	svc, db, _ := artifactFixture(t, artifactModelResponse)
	row := timedSourceRow(t, "第一句。未定时的第二句。第三句。", []model.TranscriptionSegment{
		{Text: "第一句。", StartMS: 5000, EndMS: 7000},
		{Text: "第三句。", StartMS: 15000, EndMS: 17000},
	})
	row.ID, row.TaskID = 0, 42
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.VideoTranscription{}).Where("task_id = ?", 42).Update("content", row.Content).Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	source, err := svc.Source(ctx, 7, 42)
	if err != nil || len(source.Evidence) != 3 {
		t.Fatalf("freeze source=%+v err=%v", source, err)
	}
	body := artifact.Body{SchemaVersion: 1, Kind: "study", Title: "来源核对", Blocks: []artifact.Block{{BlockID: "one", Type: "concept", Title: "依据", Content: "待核对", ClaimOrigin: "user", EvidenceRefs: []artifact.Ref{}}}, Warnings: []string{}}
	detail, err := svc.Create(ctx, 7, []int64{42}, body)
	if err != nil {
		t.Fatal(err)
	}
	chunks := buildTranscriptIndexChunks(row.Content, []model.VideoTranscriptionChunk{row}, 800, 0)
	contexts := make([]RetrievedChunk, 0, len(chunks))
	for _, chunk := range chunks {
		contexts = append(contexts, RetrievedChunk{TaskID: 42, EvidenceID: "source", Content: chunk.Content, Modality: chunk.Modality,
			StartMS: chunk.StartMS, EndMS: chunk.EndMS, TimeRangeStatus: chunk.TimeRangeStatus, SourceMappingStatus: chunk.SourceMappingStatus, SourceRefs: chunk.SourceRefs})
	}
	citations := buildCitations("三句话的依据", contexts)
	if len(citations) != 3 {
		t.Fatalf("sentence citations=%+v", citations)
	}
	raw, _ := json.Marshal(map[string]any{"citations": citations})
	snapshot := string(raw)
	if err := db.Create(&model.ChatSession{ID: 90, UserID: 7, TaskID: 42, ScopeType: model.ChatScopeVideo}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ChatMessage{ID: 91, UserID: 7, SessionID: 90, Role: "assistant", Content: "三个依据 [C1][C2][C3]", RetrievalSnapshot: &snapshot}).Error; err != nil {
		t.Fatal(err)
	}
	preview, err := svc.PreviewAnswer(ctx, 7, 91, detail.ID, "one", detail.HeadVersion)
	if err != nil || len(preview.Mapped) != 3 || len(preview.Unmapped) != 0 {
		t.Fatalf("native/gap canonical sources failed import mapping: %+v err=%v", preview, err)
	}
}
