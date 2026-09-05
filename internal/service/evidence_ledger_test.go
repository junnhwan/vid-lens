package service

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

func TestEvidenceLedgerRecordsVerifiedUncertainAndUnsupportedClaims(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	if err := repos.VisualFrame.ReplaceTaskFrames(42, []model.VideoVisualFrame{{
		TaskID: 42, FrameIndex: 1, TimeMs: 12500, ObjectKey: "frames/42/1.jpg",
		OCRText: "[画面] 可复核的视觉引用", Source: "scene", Status: model.VisualFrameStatusCompleted,
	}}); err != nil {
		t.Fatal(err)
	}
	frames, err := repos.VisualFrame.ListCompletedWithText(42)
	if err != nil || len(frames) != 1 {
		t.Fatalf("frames = %+v, %v", frames, err)
	}
	visualEvidenceID := fmt.Sprintf("visual-frame:%d", frames[0].ID)
	visualRefs, _ := MarshalChunkSourceRefs([]ChunkSourceRef{{SourceType: model.ChunkModalityVisualOCR, StableID: visualEvidenceID, SourceRowID: frames[0].ID, StartMS: 12500, EndMS: 12501, TimeRangeStatus: model.ChunkTimeRangeExact, ObjectKey: frames[0].ObjectKey}})
	if err := repos.VideoChunk.ReplaceTaskChunks(42, "embed", []model.VideoChunk{
		{UserID: 7, TaskID: 42, ChunkIndex: 3, Content: "[画面] 可复核的视觉引用", ContentHash: "visual", EmbeddingModel: "embed", EmbeddingDim: 3, VectorID: "visual-evidence", Modality: model.ChunkModalityVisualOCR, StartMS: 12500, EndMS: 12501, TimeRangeStatus: model.ChunkTimeRangeExact, SourceMappingStatus: model.ChunkSourceMapped, SourceRefs: visualRefs},
		{UserID: 7, TaskID: 42, ChunkIndex: 4, Content: "本轮检索到但未引用的片段", ContentHash: "unused", EmbeddingModel: "embed", EmbeddingDim: 3, VectorID: "unused-retrieval"},
	}); err != nil {
		t.Fatal(err)
	}
	stored, _ := repos.VideoChunk.ListByTaskID(7, 42, "embed")
	ledger := NewEvidenceLedgerService(repos)
	req := EvidenceLedgerRecordRequest{
		UserID: 7, SessionID: 9, MessageID: 11, TaskID: 42, RunID: "11111111-1111-1111-1111-111111111111",
		RawAnswer: "已确认事实。[C1]\n可能还有第二个原因。\n没有引用的断言。",
		Evidence:  []Citation{{TaskID: 42, CitationID: "C1", EvidenceID: stored[0].VectorID, ChunkID: stored[0].ID, ChunkIndex: 3, Content: "[画面] 可复核的视觉引用", Source: RetrievalSourceVector}},
		Retrieved: []Citation{
			{TaskID: 42, CitationID: "C1", EvidenceID: stored[0].VectorID, ChunkID: stored[0].ID, ChunkIndex: 3, Content: "[画面] 可复核的视觉引用", Source: RetrievalSourceVector},
			{TaskID: 42, CitationID: "C2", EvidenceID: stored[1].VectorID, ChunkID: stored[1].ID, ChunkIndex: 4, Content: "本轮检索到但未引用的片段", Source: RetrievalSourceVector},
		},
	}
	if err := ledger.RecordAnswer(context.Background(), req); err != nil {
		t.Fatalf("RecordAnswer() error = %v", err)
	}
	if err := ledger.RecordAnswer(context.Background(), req); err != nil {
		t.Fatalf("RecordAnswer() retry error = %v", err)
	}

	view, err := ledger.GetRun(context.Background(), 7, req.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if view == nil || len(view.Claims) != 3 || len(view.Evidence) != 2 || len(view.ClaimEvidence) != 1 {
		t.Fatalf("ledger view = %+v", view)
	}
	statuses := map[string]string{}
	for _, claim := range view.Claims {
		statuses[claim.Text] = claim.Status
		if claim.Status == model.ClaimStatusVerified && !strings.Contains(claim.ValidationNote, "semantic truth was not evaluated") {
			t.Fatalf("verified claim overstates validation: %+v", claim)
		}
	}
	if statuses["已确认事实。"] != model.ClaimStatusVerified {
		t.Fatalf("verified status map = %+v", statuses)
	}
	if statuses["可能还有第二个原因。"] != model.ClaimStatusUncertain {
		t.Fatalf("uncertain status map = %+v", statuses)
	}
	if statuses["没有引用的断言。"] != model.ClaimStatusUnsupported {
		t.Fatalf("unsupported status map = %+v", statuses)
	}

	var bound model.AgentEvidence
	for _, evidence := range view.Evidence {
		if evidence.SourceRef == stored[0].VectorID {
			bound = evidence
		}
	}
	if bound.SourceType != "visual_ocr" || bound.TimeRangeStatus != model.EvidenceTimeRangeKnown || bound.StartMS != 12500 || bound.EndMS != 12501 {
		t.Fatalf("bound evidence = %+v", bound)
	}
	if bound.TaskID != 42 || bound.DocumentID == "" || bound.QuoteText != "[画面] 可复核的视觉引用" || bound.ContentHash == "" || bound.StableLocator == "" {
		t.Fatalf("evidence provenance incomplete: %+v", bound)
	}
	if bound.SourceRevision != "" || bound.SourceRevisionStatus != model.EvidenceSourceRevisionUnavailable || !strings.Contains(bound.StableLocator, `"source_ref_kind":"rag_evidence_id"`) {
		t.Fatalf("source revision provenance = %+v", bound)
	}

	other, err := ledger.GetRun(context.Background(), 8, req.RunID)
	if err != nil || other != nil {
		t.Fatalf("cross-owner view = %+v, err=%v", other, err)
	}
}

func TestEvidenceLedgerUsesVisualOCRProvenanceWhenTextAlsoMatchesTranscript(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	sharedText := "画面和转写都包含的 owner 校验"
	if err := repos.TranscriptionChunk.UpsertCompletedWithRange(42, 0, "audio/chunk-0.mp3", sharedText, 10, 20); err != nil {
		t.Fatal(err)
	}
	if err := repos.VisualFrame.ReplaceTaskFrames(42, []model.VideoVisualFrame{{
		TaskID: 42, FrameIndex: 7, TimeMs: 45500, ObjectKey: "frames/42/7.jpg", OCRText: sharedText,
		Source: "scene", CaptionMethod: "ocr", Status: model.VisualFrameStatusCompleted,
	}}); err != nil {
		t.Fatal(err)
	}
	frames, err := repos.VisualFrame.ListCompletedWithText(42)
	if err != nil || len(frames) != 1 {
		t.Fatalf("frames = %+v, %v", frames, err)
	}
	runID := "22222222-2222-2222-2222-222222222222"
	stableID := fmt.Sprintf("visual-frame:%d", frames[0].ID)
	refs, _ := MarshalChunkSourceRefs([]ChunkSourceRef{{SourceType: model.ChunkModalityVisualOCR, StableID: stableID, SourceRowID: frames[0].ID, StartMS: 45500, EndMS: 45501, TimeRangeStatus: model.ChunkTimeRangeExact, ObjectKey: frames[0].ObjectKey, CaptionMethod: "ocr"}})
	if err := repos.VideoChunk.ReplaceTaskChunks(42, "embed", []model.VideoChunk{{UserID: 7, TaskID: 42, ChunkIndex: 7, Content: sharedText, ContentHash: "visual", EmbeddingModel: "embed", EmbeddingDim: 3, VectorID: "visual-mapped", Modality: model.ChunkModalityVisualOCR, StartMS: 45500, EndMS: 45501, TimeRangeStatus: model.ChunkTimeRangeExact, SourceMappingStatus: model.ChunkSourceMapped, SourceRefs: refs}}); err != nil {
		t.Fatal(err)
	}
	storedVisual, _ := repos.VideoChunk.ListByTaskID(7, 42, "embed")
	citation := Citation{
		TaskID: 42, CitationID: "C1", EvidenceID: storedVisual[0].VectorID, ChunkID: storedVisual[0].ID,
		ChunkIndex: frames[0].FrameIndex, Content: sharedText, Source: RetrievalSourceVector,
	}
	if err := NewEvidenceLedgerService(repos).RecordAnswer(context.Background(), EvidenceLedgerRecordRequest{
		UserID: 7, SessionID: 9, MessageID: 11, TaskID: 42, RunID: runID,
		RawAnswer: "画面确认 owner 校验。[C1]", Evidence: []Citation{citation}, Retrieved: []Citation{citation},
	}); err != nil {
		t.Fatalf("RecordAnswer() error = %v", err)
	}
	view, err := NewEvidenceLedgerService(repos).GetRun(context.Background(), 7, runID)
	if err != nil || view == nil || len(view.Evidence) != 1 {
		t.Fatalf("ledger view = %+v, %v", view, err)
	}
	evidence := view.Evidence[0]
	if evidence.SourceType != "visual_ocr" || evidence.DocumentID != stableID || evidence.StartMS != 45500 || evidence.EndMS != 45501 || evidence.TimeRangeStatus != model.EvidenceTimeRangeKnown {
		t.Fatalf("visual OCR evidence = %+v", evidence)
	}
	for _, want := range []string{`"retrieval_source":"vector"`, `"stable_id":"` + stableID + `"`, `"start_ms":45500`, `"object_key":"frames/42/7.jpg"`} {
		if !strings.Contains(evidence.StableLocator, want) {
			t.Fatalf("stable locator %s missing %s", evidence.StableLocator, want)
		}
	}
}

func TestEvidenceLedgerResolvesDuplicateTranscriptTextByStableIdentity(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	sharedText := "重复出现的 owner 校验说明"
	ref1, _ := MarshalChunkSourceRefs([]ChunkSourceRef{{SourceType: model.ChunkModalityTranscript, StableID: "segment-1", SegmentKey: "segment-1", SourceRowID: 1, StartMS: 10000, EndMS: 20000, TimeRangeStatus: model.ChunkTimeRangeCoarse}})
	ref2, _ := MarshalChunkSourceRefs([]ChunkSourceRef{{SourceType: model.ChunkModalityTranscript, StableID: "segment-2", SegmentKey: "segment-2", SourceRowID: 2, StartMS: 30000, EndMS: 40000, TimeRangeStatus: model.ChunkTimeRangeCoarse}})
	videoChunks := []model.VideoChunk{
		{UserID: 7, TaskID: 42, ChunkIndex: 1, Content: sharedText, ContentHash: "11111111111111111111111111111111", EmbeddingModel: "embed", EmbeddingDim: 3, VectorID: "duplicate-transcript-1", Modality: model.ChunkModalityTranscript, StartMS: 10000, EndMS: 20000, TimeRangeStatus: model.ChunkTimeRangeCoarse, SourceMappingStatus: model.ChunkSourceMapped, SourceRefs: ref1},
		{UserID: 7, TaskID: 42, ChunkIndex: 2, Content: sharedText, ContentHash: "22222222222222222222222222222222", EmbeddingModel: "embed", EmbeddingDim: 3, VectorID: "duplicate-transcript-2", Modality: model.ChunkModalityTranscript, StartMS: 30000, EndMS: 40000, TimeRangeStatus: model.ChunkTimeRangeCoarse, SourceMappingStatus: model.ChunkSourceMapped, SourceRefs: ref2},
	}
	if err := repos.VideoChunk.ReplaceTaskChunks(42, "embed", videoChunks); err != nil {
		t.Fatal(err)
	}
	if err := repos.TranscriptionChunk.UpsertCompletedWithRange(42, 1, "audio/1.mp3", sharedText, 10, 20); err != nil {
		t.Fatal(err)
	}
	if err := repos.TranscriptionChunk.UpsertCompletedWithRange(42, 2, "audio/2.mp3", sharedText, 30, 40); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		citation  Citation
		wantKnown bool
	}{
		{name: "evidence id", citation: Citation{TaskID: 42, EvidenceID: videoChunks[1].VectorID, Content: sharedText}, wantKnown: true},
		{name: "chunk id", citation: Citation{TaskID: 42, ChunkID: videoChunks[1].ID, Content: sharedText}, wantKnown: true},
		{name: "chunk index is not identity", citation: Citation{TaskID: 42, ChunkIndex: 2, Content: sharedText}, wantKnown: false},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.citation.CitationID = "C1"
			runID := fmt.Sprintf("duplicate-transcript-identity-%d", index)
			ledger := NewEvidenceLedgerService(repos)
			if err := ledger.RecordAnswer(context.Background(), EvidenceLedgerRecordRequest{
				UserID: 7, SessionID: 9, MessageID: int64(20 + index), TaskID: 42, RunID: runID,
				RawAnswer: "引用第二处重复转写。[C1]", Evidence: []Citation{test.citation},
			}); err != nil {
				t.Fatalf("RecordAnswer() error = %v", err)
			}
			view, err := ledger.GetRun(context.Background(), 7, runID)
			if err != nil || view == nil || len(view.Evidence) != 1 {
				t.Fatalf("ledger view = %+v, %v", view, err)
			}
			got := view.Evidence[0]
			if test.wantKnown && (got.DocumentID != "segment-2" || got.StartMS != 30000 || got.EndMS != 40000 || got.TimeRangeStatus != model.EvidenceTimeRangeKnown) {
				t.Fatalf("identity %s resolved duplicate text to %+v, want mapped second transcript range", test.name, got)
			}
			if !test.wantKnown && got.TimeRangeStatus != model.EvidenceTimeRangeUnknown {
				t.Fatalf("chunk index inferred a source range: %+v", got)
			}
		})
	}
}

func TestEvidenceLedgerNeverInfersSourceFromTranscriptText(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	if err := repos.TranscriptionChunk.UpsertCompletedWithRange(42, 1, "audio/1.mp3", "重复转写", 10, 20); err != nil {
		t.Fatal(err)
	}
	if err := repos.TranscriptionChunk.UpsertCompletedWithRange(42, 2, "audio/2.mp3", "重复转写", 30, 40); err != nil {
		t.Fatal(err)
	}
	if err := repos.TranscriptionChunk.UpsertCompletedWithRange(42, 3, "audio/3.mp3", "唯一转写", 50, 60); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name       string
		runID      string
		text       string
		wantStatus string
		wantStart  int64
	}{
		{name: "duplicate rejected", runID: "text-fallback-duplicate", text: "重复转写", wantStatus: model.EvidenceTimeRangeUnknown},
		{name: "unique remains unknown", runID: "text-fallback-unique", text: "唯一转写", wantStatus: model.EvidenceTimeRangeUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			ledger := NewEvidenceLedgerService(repos)
			citation := Citation{TaskID: 42, CitationID: "C1", Content: test.text}
			if err := ledger.RecordAnswer(context.Background(), EvidenceLedgerRecordRequest{
				UserID: 7, SessionID: 9, MessageID: 30, TaskID: 42, RunID: test.runID,
				RawAnswer: "转写证据。[C1]", Evidence: []Citation{citation},
			}); err != nil {
				t.Fatalf("RecordAnswer() error = %v", err)
			}
			view, err := ledger.GetRun(context.Background(), 7, test.runID)
			if err != nil || view == nil || len(view.Evidence) != 1 {
				t.Fatalf("ledger view = %+v, %v", view, err)
			}
			if got := view.Evidence[0]; got.TimeRangeStatus != test.wantStatus || got.StartSecond != test.wantStart {
				t.Fatalf("fallback evidence = %+v", got)
			}
		})
	}
}

func TestEvidenceLedgerDegradesVisualCitationWithoutChunkProvenance(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	if err := repos.VisualFrame.ReplaceTaskFrames(42, []model.VideoVisualFrame{{
		TaskID: 42, FrameIndex: 1, TimeMs: 12000, ObjectKey: "frames/42/1.jpg", OCRText: "相同 OCR 文本",
		Source: "scene", CaptionMethod: "ocr", Status: model.VisualFrameStatusCompleted,
	}}); err != nil {
		t.Fatal(err)
	}
	citation := Citation{TaskID: 42, CitationID: "C1", Content: "相同 OCR 文本", Source: "visual_ocr"}
	ledger := NewEvidenceLedgerService(repos)
	err := ledger.RecordAnswer(context.Background(), EvidenceLedgerRecordRequest{
		UserID: 7, SessionID: 9, MessageID: 11, TaskID: 42, RunID: "visual-without-frame-id",
		RawAnswer: "画面证据。[C1]", Evidence: []Citation{citation},
	})
	if err != nil {
		t.Fatalf("RecordAnswer() error = %v", err)
	}
	view, getErr := ledger.GetRun(context.Background(), 7, "visual-without-frame-id")
	if getErr != nil || view == nil || view.Evidence[0].TimeRangeStatus != model.EvidenceTimeRangeUnknown || view.Claims[0].Status != model.ClaimStatusUncertain {
		t.Fatalf("unmapped visual evidence was not explicitly degraded: %+v, %v", view, getErr)
	}
}

func TestEvidenceLedgerRejectsCrossTaskEvidenceAtRecordBoundary(t *testing.T) {
	for _, test := range []struct {
		name      string
		evidence  []Citation
		retrieved []Citation
	}{
		{name: "published evidence", evidence: []Citation{{TaskID: 99, CitationID: "C1", EvidenceID: "cross-evidence", Content: "其他视频证据"}}},
		{name: "retrieved evidence", evidence: []Citation{{TaskID: 42, CitationID: "C1", EvidenceID: "current-evidence", Content: "当前视频证据"}}, retrieved: []Citation{{TaskID: 99, EvidenceID: "cross-retrieved", Content: "其他视频检索结果"}}},
		{name: "missing task identity", evidence: []Citation{{CitationID: "C1", EvidenceID: "missing-task", Content: "没有 task 身份"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repos := newChatServiceTestRepositories(t)
			runID := "task-boundary-" + strings.ReplaceAll(test.name, " ", "-")
			ledger := NewEvidenceLedgerService(repos)
			err := ledger.RecordAnswer(context.Background(), EvidenceLedgerRecordRequest{
				UserID: 7, SessionID: 9, MessageID: 11, TaskID: 42, RunID: runID,
				RawAnswer: "必须只使用当前视频。[C1]", Evidence: test.evidence, Retrieved: test.retrieved,
			})
			if err == nil || !strings.Contains(err.Error(), "task") {
				t.Fatalf("RecordAnswer() error = %v, want current-task boundary rejection", err)
			}
			view, getErr := ledger.GetRun(context.Background(), 7, runID)
			if getErr != nil || view != nil {
				t.Fatalf("cross-task evidence polluted ledger: %+v, %v", view, getErr)
			}
		})
	}
}

func TestEvidenceLedgerDoesNotResolveVisualFrameByLabelOrText(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	if err := repos.VisualFrame.ReplaceTaskFrames(42, []model.VideoVisualFrame{{
		TaskID: 42, FrameIndex: 1, TimeMs: 12000, ObjectKey: "frames/42/1.jpg", OCRText: "实际 OCR 文本",
		Source: "scene", CaptionMethod: "ocr", Status: model.VisualFrameStatusCompleted,
	}}); err != nil {
		t.Fatal(err)
	}
	frames, err := repos.VisualFrame.ListCompletedWithText(42)
	if err != nil || len(frames) != 1 {
		t.Fatalf("frames = %+v, %v", frames, err)
	}

	for _, test := range []struct {
		name       string
		evidenceID string
		content    string
	}{
		{name: "missing frame", evidenceID: "visual-frame:999999", content: "实际 OCR 文本"},
		{name: "text mismatch", evidenceID: fmt.Sprintf("visual-frame:%d", frames[0].ID), content: "伪造 OCR 文本"},
	} {
		t.Run(test.name, func(t *testing.T) {
			runID := "visual-binding-" + strings.ReplaceAll(test.name, " ", "-")
			ledger := NewEvidenceLedgerService(repos)
			citation := Citation{TaskID: 42, CitationID: "C1", EvidenceID: test.evidenceID, Content: test.content, Source: "visual_ocr"}
			err := ledger.RecordAnswer(context.Background(), EvidenceLedgerRecordRequest{
				UserID: 7, SessionID: 9, MessageID: 11, TaskID: 42, RunID: runID,
				RawAnswer: "画面证据。[C1]", Evidence: []Citation{citation}, Retrieved: []Citation{citation},
			})
			if err != nil {
				t.Fatalf("RecordAnswer() error = %v", err)
			}
			view, getErr := ledger.GetRun(context.Background(), 7, runID)
			if getErr != nil || view == nil || view.Evidence[0].TimeRangeStatus != model.EvidenceTimeRangeUnknown || view.Claims[0].Status != model.ClaimStatusUncertain {
				t.Fatalf("unmapped visual evidence was not degraded: %+v, %v", view, getErr)
			}
		})
	}
}

func TestEvidenceLedgerDoesNotInferTimeRangeFromChunkIndex(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	if err := repos.TranscriptionChunk.UpsertCompleted(42, 17, "audio/chunk-17.mp3", "没有真实时间范围的转写"); err != nil {
		t.Fatal(err)
	}
	if err := repos.VideoChunk.ReplaceTaskChunks(42, "embed", []model.VideoChunk{{UserID: 7, TaskID: 42, ChunkIndex: 17, Content: "没有真实时间范围的转写", ContentHash: "legacy", EmbeddingModel: "embed", EmbeddingDim: 3, VectorID: "stable-unknown-time", Modality: model.ChunkModalityTranscript, TimeRangeStatus: model.ChunkTimeRangeUnknown, SourceMappingStatus: model.ChunkSourceUnmapped, SourceRefs: "[]"}}); err != nil {
		t.Fatal(err)
	}
	legacyChunks, _ := repos.VideoChunk.ListByTaskID(7, 42, "embed")
	ledger := NewEvidenceLedgerService(repos)
	req := EvidenceLedgerRecordRequest{
		UserID: 7, SessionID: 9, MessageID: 11, TaskID: 42, RunID: "33333333-3333-3333-3333-333333333333",
		RawAnswer: "该转写没有真实时间码。[C1]",
		Evidence: []Citation{{
			TaskID: 42, CitationID: "C1", EvidenceID: legacyChunks[0].VectorID, ChunkID: legacyChunks[0].ID, ChunkIndex: 17,
			Content: "没有真实时间范围的转写", Source: RetrievalSourceHybrid,
		}},
	}
	if err := ledger.RecordAnswer(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	view, err := ledger.GetRun(context.Background(), 7, req.RunID)
	if err != nil || view == nil || len(view.Claims) != 1 || len(view.Evidence) != 1 {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	evidence := view.Evidence[0]
	if evidence.SourceType != "transcript" || evidence.StartSecond != 0 || evidence.EndSecond != 0 || evidence.TimeRangeStatus != model.EvidenceTimeRangeUnknown {
		t.Fatalf("chunk index produced a fabricated time range: %+v", evidence)
	}
	if view.Claims[0].Status != model.ClaimStatusUncertain {
		t.Fatalf("claim status = %s, want uncertain", view.Claims[0].Status)
	}
}

func TestEvidenceLedgerKeepsUnknownTimeRangeUncertainAndAppendsCorrection(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	ledger := NewEvidenceLedgerService(repos)
	hypothesis, err := ledger.CreateHypothesis(context.Background(), EvidenceHypothesisRequest{
		UserID: 7, SessionID: 9, MessageID: 11, RunID: "hypothesis-run", Text: "等待核验的假设", Confidence: 0.25,
	})
	if err != nil || hypothesis == nil || hypothesis.Status != model.ClaimStatusHypothesized {
		t.Fatalf("CreateHypothesis() = %+v, %v", hypothesis, err)
	}
	req := EvidenceLedgerRecordRequest{
		UserID: 7, SessionID: 9, MessageID: 11, TaskID: 42, RunID: "22222222-2222-2222-2222-222222222222",
		RawAnswer: "有稳定来源但缺少时间定位。[C1] 引用了不存在的来源。[C9]",
		Evidence:  []Citation{{TaskID: 42, CitationID: "C1", EvidenceID: "stable-no-time", ChunkID: 8, ChunkIndex: 5, Content: "稳定引用文本"}},
	}
	if err := ledger.RecordAnswer(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	view, err := ledger.GetRun(context.Background(), 7, req.RunID)
	if err != nil || view == nil || len(view.Claims) != 2 {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	if view.Claims[0].Status != model.ClaimStatusUncertain || view.Evidence[0].TimeRangeStatus != model.EvidenceTimeRangeUnknown {
		t.Fatalf("unknown-time records = %+v / %+v", view.Claims, view.Evidence)
	}
	if view.Claims[1].Status != model.ClaimStatusUnsupported {
		t.Fatalf("invalid citation claim = %+v", view.Claims[1])
	}

	corrected, err := ledger.CorrectClaim(context.Background(), 7, view.Claims[0].ID, EvidenceClaimCorrectionRequest{Text: "更正后的事实。", Reason: "人工复核原视频"})
	if err != nil || corrected == nil {
		t.Fatalf("CorrectClaim() = %+v, %v", corrected, err)
	}
	if corrected.Status != model.ClaimStatusCorrected || corrected.Revision != 2 || corrected.SupersedesClaimID != view.Claims[0].ID {
		t.Fatalf("correction = %+v", corrected)
	}
	updated, err := ledger.GetRun(context.Background(), 7, req.RunID)
	if err != nil || len(updated.Claims) != 3 || len(updated.ClaimEvidence) != 2 {
		t.Fatalf("updated ledger = %+v err=%v", updated, err)
	}
	if crossOwner, err := ledger.CorrectClaim(context.Background(), 8, view.Claims[0].ID, EvidenceClaimCorrectionRequest{Text: "越权", Reason: "越权"}); err != nil || crossOwner != nil {
		t.Fatalf("cross-owner correction = %+v err=%v", crossOwner, err)
	}
}

func TestVideoAgentPersistsCandidateButBlocksUninspectedAnswer(t *testing.T) {
	repos, task, session := newVideoAgentTestSession(t)
	if err := repos.TranscriptionChunk.UpsertCompleted(task.ID, 0, "audio/chunk-0.mp3", "owner 校验引用片段"); err != nil {
		t.Fatal(err)
	}
	ledger := NewEvidenceLedgerService(repos)
	chatSvc := NewChatServiceWithDependencies(repos, &fakeRetriever{results: []RetrievedChunk{{
		TaskID: task.ID, EvidenceID: "ev-agent-ledger", ChunkID: 1, ChunkIndex: 2, Content: "owner 校验引用片段",
	}}}, ChatConfig{TopK: 5, CandidateK: 5, MinScore: 0.3}, ChatDependencies{EvidenceLedger: ledger})
	agent := NewVideoAgentService(chatSvc)
	result, err := agent.Ask(context.Background(), VideoAgentRequest{UserID: 7, SessionID: session.ID, Question: "为什么要校验 owner？", TopK: 1},
		&fakeEmbeddingClient{dim: 3}, &scriptedChatClient{responses: []string{"not-json", "owner 必须校验。[C1]"}},
		ai.Profile{EmbeddingModel: "text-embedding-3-small", LLMModel: "chat-model"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Answer != inspectorBlockedAnswer {
		t.Fatalf("answer changed by ledger = %q", result.Answer)
	}
	view, err := ledger.GetRun(context.Background(), 7, result.RunID)
	if err != nil || view == nil || len(view.Claims) != 1 || view.Claims[0].Status != model.ClaimStatusUncertain {
		t.Fatalf("agent ledger = %+v err=%v", view, err)
	}
}
