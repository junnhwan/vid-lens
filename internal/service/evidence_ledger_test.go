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
	ledger := NewEvidenceLedgerService(repos)
	req := EvidenceLedgerRecordRequest{
		UserID: 7, SessionID: 9, MessageID: 11, TaskID: 42, RunID: "11111111-1111-1111-1111-111111111111",
		RawAnswer: "已确认事实。[C1]\n可能还有第二个原因。\n没有引用的断言。",
		Evidence:  []Citation{{TaskID: 42, CitationID: "C1", EvidenceID: "stable-evidence-1", ChunkID: 5, ChunkIndex: 3, Content: "[画面] 可复核的视觉引用", Source: RetrievalSourceHybrid}},
		Retrieved: []Citation{
			{TaskID: 42, CitationID: "C1", EvidenceID: "stable-evidence-1", ChunkID: 5, ChunkIndex: 3, Content: "[画面] 可复核的视觉引用", Source: RetrievalSourceHybrid},
			{TaskID: 42, CitationID: "C2", EvidenceID: "unused-retrieval", ChunkID: 6, ChunkIndex: 4, Content: "本轮检索到但未引用的片段", Source: RetrievalSourceVector},
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
		if evidence.SourceRef == "stable-evidence-1" {
			bound = evidence
		}
	}
	if bound.SourceType != "visual_ocr" || bound.TimeRangeStatus != model.EvidenceTimeRangeKnown || bound.StartSecond != 12 || bound.EndSecond != 13 {
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
	citation := Citation{
		TaskID: 42, CitationID: "C1", EvidenceID: fmt.Sprintf("visual-frame:%d", frames[0].ID),
		ChunkIndex: frames[0].FrameIndex, Content: sharedText, Source: "visual_ocr",
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
	if evidence.SourceType != "visual_ocr" || evidence.DocumentID != fmt.Sprintf("visual_frame:%d", frames[0].ID) || evidence.StartSecond != 45 || evidence.EndSecond != 46 || evidence.TimeRangeStatus != model.EvidenceTimeRangeKnown {
		t.Fatalf("visual OCR evidence = %+v", evidence)
	}
	for _, want := range []string{`"retrieval_source":"visual_ocr"`, `"frame_id":`, `"time_ms":45500`, `"object_key":"frames/42/7.jpg"`} {
		if !strings.Contains(evidence.StableLocator, want) {
			t.Fatalf("stable locator %s missing %s", evidence.StableLocator, want)
		}
	}
}

func TestEvidenceLedgerDoesNotInferTimeRangeFromChunkIndex(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	if err := repos.TranscriptionChunk.UpsertCompleted(42, 17, "audio/chunk-17.mp3", "没有真实时间范围的转写"); err != nil {
		t.Fatal(err)
	}
	ledger := NewEvidenceLedgerService(repos)
	req := EvidenceLedgerRecordRequest{
		UserID: 7, SessionID: 9, MessageID: 11, TaskID: 42, RunID: "33333333-3333-3333-3333-333333333333",
		RawAnswer: "该转写没有真实时间码。[C1]",
		Evidence: []Citation{{
			TaskID: 42, CitationID: "C1", EvidenceID: "stable-unknown-time", ChunkID: 17, ChunkIndex: 17,
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

func TestVideoAgentPersistsAnswerFactsWithoutChangingAnswer(t *testing.T) {
	repos, task, session := newVideoAgentTestSession(t)
	if err := repos.TranscriptionChunk.UpsertCompleted(task.ID, 0, "audio/chunk-0.mp3", "owner 校验引用片段"); err != nil {
		t.Fatal(err)
	}
	chatSvc := NewChatService(repos, &fakeRetriever{results: []RetrievedChunk{{
		TaskID: task.ID, EvidenceID: "ev-agent-ledger", ChunkID: 1, ChunkIndex: 2, Content: "owner 校验引用片段",
	}}}, ChatConfig{TopK: 5, CandidateK: 5, MinScore: 0.3})
	ledger := NewEvidenceLedgerService(repos)
	chatSvc.SetEvidenceLedger(ledger)
	agent := NewVideoAgentService(chatSvc)
	result, err := agent.Ask(context.Background(), VideoAgentRequest{UserID: 7, SessionID: session.ID, Question: "为什么要校验 owner？", TopK: 1},
		&fakeEmbeddingClient{dim: 3}, &scriptedChatClient{responses: []string{"not-json", "owner 必须校验。[C1]"}},
		ai.Profile{EmbeddingModel: "text-embedding-3-small", LLMModel: "chat-model"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Answer != "owner 必须校验。" {
		t.Fatalf("answer changed by ledger = %q", result.Answer)
	}
	view, err := ledger.GetRun(context.Background(), 7, result.RunID)
	if err != nil || view == nil || len(view.Claims) != 1 || view.Claims[0].Status != model.ClaimStatusUncertain {
		t.Fatalf("agent ledger = %+v err=%v", view, err)
	}
}
