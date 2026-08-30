package service

import (
	"context"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

func TestEvidenceLedgerRecordsVerifiedUncertainAndUnsupportedClaims(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	if err := repos.TranscriptionChunk.UpsertCompleted(42, 0, "audio/chunk-0.mp3", "可复核的转写引用"); err != nil {
		t.Fatal(err)
	}
	ledger := NewEvidenceLedgerService(repos)
	req := EvidenceLedgerRecordRequest{
		UserID: 7, SessionID: 9, MessageID: 11, TaskID: 42, RunID: "11111111-1111-1111-1111-111111111111",
		RawAnswer: "已确认事实。[C1]\n可能还有第二个原因。\n没有引用的断言。",
		Evidence:  []Citation{{TaskID: 42, CitationID: "C1", EvidenceID: "stable-evidence-1", ChunkID: 5, ChunkIndex: 3, Content: "可复核的转写引用", Source: RetrievalSourceHybrid}},
		Retrieved: []Citation{
			{TaskID: 42, CitationID: "C1", EvidenceID: "stable-evidence-1", ChunkID: 5, ChunkIndex: 3, Content: "可复核的转写引用", Source: RetrievalSourceHybrid},
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
	if bound.SourceType != "transcript" || bound.TimeRangeStatus != model.EvidenceTimeRangeKnown || bound.StartSecond != 0 || bound.EndSecond != 300 {
		t.Fatalf("bound evidence = %+v", bound)
	}
	if bound.TaskID != 42 || bound.DocumentID == "" || bound.QuoteText != "可复核的转写引用" || bound.ContentHash == "" || bound.StableLocator == "" {
		t.Fatalf("evidence provenance incomplete: %+v", bound)
	}

	other, err := ledger.GetRun(context.Background(), 8, req.RunID)
	if err != nil || other != nil {
		t.Fatalf("cross-owner view = %+v, err=%v", other, err)
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
	if err != nil || view == nil || len(view.Claims) != 1 || view.Claims[0].Status != model.ClaimStatusVerified {
		t.Fatalf("agent ledger = %+v err=%v", view, err)
	}
}
