package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

func TestVideoEvidenceFunnelRunsFixedOrderAndPersistsCoverage(t *testing.T) {
	repos, task, session := newVideoAgentTestSession(t)
	if err := repos.Summary.Create(&model.AISummary{TaskID: task.ID, FileMD5: task.FileMD5, Content: "全局摘要只用于定位", ModelName: "summary-model"}); err != nil {
		t.Fatal(err)
	}
	chunks := []model.VideoChunk{
		{UserID: 7, TaskID: task.ID, ChunkIndex: 0, Content: "前置背景", ContentHash: "00000000000000000000000000000000", EmbeddingModel: "embed", EmbeddingDim: 3, VectorID: "funnel-0"},
		{UserID: 7, TaskID: task.ID, ChunkIndex: 1, Content: "owner 校验来自 transcript", ContentHash: "11111111111111111111111111111111", EmbeddingModel: "embed", EmbeddingDim: 3, VectorID: "funnel-1"},
		{UserID: 7, TaskID: task.ID, ChunkIndex: 2, Content: "后置背景", ContentHash: "22222222222222222222222222222222", EmbeddingModel: "embed", EmbeddingDim: 3, VectorID: "funnel-2"},
	}
	if err := repos.VideoChunk.ReplaceTaskChunks(task.ID, "embed", chunks); err != nil {
		t.Fatal(err)
	}
	for index, content := range []string{"前置背景", "owner 校验来自 transcript", "后置背景"} {
		if err := repos.TranscriptionChunk.UpsertCompleted(task.ID, index, fmt.Sprintf("audio-%d", index), content); err != nil {
			t.Fatal(err)
		}
	}
	if err := repos.VisualFrame.ReplaceTaskFrames(task.ID, []model.VideoVisualFrame{{
		TaskID: task.ID, FrameIndex: 4, TimeMs: 42000, ObjectKey: "frames/4.jpg", OCRText: "画面显示 owner 校验通过", Source: "scene", CaptionMethod: "ocr", Status: model.VisualFrameStatusCompleted,
	}}); err != nil {
		t.Fatal(err)
	}
	frames, err := repos.VisualFrame.ListCompletedWithText(task.ID)
	if err != nil || len(frames) != 1 {
		t.Fatalf("visual frames = %+v, %v", frames, err)
	}

	retriever := &fakeRetriever{results: []RetrievedChunk{{TaskID: task.ID, EvidenceID: "transcript-hit-1", ChunkID: chunks[1].ID, ChunkIndex: 1, Content: chunks[1].Content, Source: "vector"}}}
	chatClient := &scriptedChatClient{responses: []string{
		`{"done":false,"candidate_ids":["transcript-1"]}`,
		fmt.Sprintf(`{"done":false,"candidate_ids":["visual-%d"]}`, frames[0].ID),
		"transcript 与画面共同确认 owner 校验 [C1][C5]。",
	}}
	chatSvc := NewChatService(repos, retriever, ChatConfig{TopK: 1, CandidateK: 1, MinScore: 0.1})
	agent := NewVideoAgentService(chatSvc)
	result, err := agent.AskEvidenceFunnel(context.Background(), EvidenceFunnelRequest{UserID: 7, SessionID: session.ID, Goal: "核验 owner", TopK: 1}, &fakeEmbeddingClient{dim: 3}, chatClient, ai.Profile{EmbeddingModel: "embed", LLMModel: "chat-model"})
	if err != nil {
		t.Fatalf("AskEvidenceFunnel() error = %v", err)
	}
	if result.Template != string(VideoAgentEvidenceFunnelTemplate) || result.Mode != string(VideoAgentEvidenceFunnelTemplate) || result.Answer != "transcript 与画面共同确认 owner 校验。" || len(result.Citations) != 2 {
		t.Fatalf("funnel result = %+v", result)
	}
	if got := traceTools(result.Trace); got != strings.Join(evidenceFunnelActionOrder, "|") {
		t.Fatalf("funnel trace = %s", got)
	}

	execution, err := repos.AgentExecution.GetExecution(context.Background(), 7, result.RunID)
	if err != nil || execution == nil || execution.Run.Status != model.AgentRunStatusCompleted || len(execution.Steps) != 8 || len(execution.ToolCalls) != 8 {
		t.Fatalf("funnel execution = %+v, %v", execution, err)
	}
	if execution.Run.ToolCallsUsed != 5 || execution.Run.LLMCallsUsed != 3 || execution.Run.VisionCallsUsed != 0 {
		t.Fatalf("funnel budget counters = %+v", execution.Run)
	}
	for index, call := range execution.ToolCalls {
		if call.ToolName != evidenceFunnelActionOrder[index] || call.CallDigest == "" || call.ArgumentsDigest == "" || call.Status != model.AgentToolCallStatusCompleted || call.MetricsJSON == "" || call.MetricsJSON == "{}" || strings.Contains(call.InputSummary, "核验 owner") || strings.Contains(call.InputSummary, "全局摘要") {
			t.Fatalf("funnel call %d = %+v", index, call)
		}
	}
	visualCall := execution.ToolCalls[5]
	if !strings.Contains(visualCall.EvidenceRefs, "visual-frame") || !strings.Contains(visualCall.FinalEvidenceRefs, "visual-frame") {
		t.Fatalf("visual evidence projection = %+v", visualCall)
	}
	plannerCall := execution.ToolCalls[2]
	if plannerCall.CallKind != model.AgentCallKindPlannerLLM || plannerCall.UsageSource != model.AgentCallUsageEstimated || plannerCall.PromptTokens <= 0 || !plannerCall.TokenEstimated {
		t.Fatalf("bounded planner call = %+v", plannerCall)
	}
	validationCall := execution.ToolCalls[7]
	if validationCall.CallKind != model.AgentCallKindValidation || !strings.Contains(validationCall.MetricsJSON, `"claims":1`) {
		t.Fatalf("validation call = %+v", validationCall)
	}
	ledger, err := NewEvidenceLedgerService(repos).GetRun(context.Background(), 7, result.RunID)
	if err != nil || ledger == nil || len(ledger.Claims) != 1 || len(ledger.Evidence) < 2 || len(ledger.ClaimEvidence) != 2 {
		t.Fatalf("funnel ledger = %+v, %v", ledger, err)
	}
	messages, err := repos.Chat.ListMessages(7, session.ID)
	if err != nil || len(messages) != 2 || messages[1].RetrievalSnapshot == nil {
		t.Fatalf("funnel messages = %+v, %v", messages, err)
	}
	snapshot, err := DecodeAgentSnapshot(*messages[1].RetrievalSnapshot)
	if err != nil || snapshot.RunID != result.RunID || len(snapshot.Steps) != 8 {
		t.Fatalf("funnel compatibility snapshot = %+v, %v", snapshot, err)
	}
}

func TestEvidenceFunnelRejectsCrossVideoCandidatesAndCitations(t *testing.T) {
	candidates := []EvidenceGapCandidate{{ID: "other", TaskID: 99, Content: "other video"}}
	if _, err := selectGapCandidates(candidates, EvidenceGapDecision{CandidateIDs: []string{"other"}}, 1, 11); err == nil || !strings.Contains(err.Error(), "crosses") {
		t.Fatalf("cross-video candidate error = %v", err)
	}
	if err := validateCitationScope(11, []Citation{{TaskID: 99, EvidenceID: "other"}}); err == nil || !strings.Contains(err.Error(), "cross-video") {
		t.Fatalf("cross-video citation error = %v", err)
	}
}

func TestEvidenceFunnelCompletedCheckpointsAreIdempotent(t *testing.T) {
	runner, chatClient, runID := newDirectEvidenceFunnelRunner(t, 8)
	first, err := runner.Run(context.Background(), "owner")
	if err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	firstCalls := len(chatClient.messages)
	if firstCalls != 2 || first.RawAnswer == "" {
		t.Fatalf("first planner/answer calls = %d result=%+v", firstCalls, first)
	}
	runner.planner = NewLLMEvidenceGapPlanner(&scriptedChatClient{})
	runner.tools.chat = &scriptedChatClient{}
	second, err := runner.Run(context.Background(), "owner")
	if err != nil {
		t.Fatalf("recovered Run() error = %v", err)
	}
	if second.RawAnswer != first.RawAnswer {
		t.Fatalf("recovered result = %+v, want %+v", second, first)
	}
	records, err := runner.repos.AgentExecution.GetExecution(context.Background(), 7, runID)
	if err != nil || records == nil || len(records.ToolCalls) != 7 {
		t.Fatalf("idempotent records = %+v, %v", records, err)
	}
}

func TestEvidenceFunnelStopsBeforePlannerWhenBudgetIsExhausted(t *testing.T) {
	runner, chatClient, _ := newDirectEvidenceFunnelRunner(t, 2)
	_, err := runner.Run(context.Background(), "owner")
	if err == nil || !strings.Contains(err.Error(), "budget exhausted") {
		t.Fatalf("budget Run() error = %v", err)
	}
	if len(chatClient.messages) != 0 {
		t.Fatalf("planner invoked after budget exhaustion: %d", len(chatClient.messages))
	}
}

func newDirectEvidenceFunnelRunner(t *testing.T, maxSteps int) (*evidenceFunnelRunner, *scriptedChatClient, string) {
	t.Helper()
	repos, task, session := newVideoAgentTestSession(t)
	chunks := []model.VideoChunk{{UserID: 7, TaskID: task.ID, ChunkIndex: 0, Content: "owner evidence", ContentHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", EmbeddingModel: "embed", EmbeddingDim: 3, VectorID: "direct-funnel"}}
	if err := repos.VideoChunk.ReplaceTaskChunks(task.ID, "embed", chunks); err != nil {
		t.Fatal(err)
	}
	runID := "funnel-recovery-" + strings.ReplaceAll(t.Name(), "/", "-")
	policy := defaultEvidenceFunnelPolicy(1)
	frozenPolicy, budget := evidenceFunnelAgentPolicy(policy)
	budget.MaxSteps = maxSteps
	run := &model.AgentRun{
		ID: runID, UserID: 7, SessionID: session.ID, ScopeType: model.ChatScopeVideo, TaskID: task.ID, Goal: "owner", Mode: string(VideoAgentEvidenceFunnelTemplate), AgentProfile: "bounded-evidence-funnel",
		ProfileSnapshot: `{}`, PolicySnapshot: mustJSONForTest(t, frozenPolicy), BudgetSnapshot: mustJSONForTest(t, budget), Status: model.AgentRunStatusRunning,
		MaxSteps: budget.MaxSteps, MaxToolCalls: budget.MaxToolCalls, MaxLLMCalls: budget.MaxLLMCalls, MaxVisionCalls: budget.MaxVisionCalls, MaxAttemptsPerStep: budget.MaxAttemptsPerStep, CreatedAt: time.Now().UTC(),
	}
	if created, err := repos.AgentExecution.CreateRun(context.Background(), run); err != nil || !created {
		t.Fatalf("CreateRun() = %v, %v", created, err)
	}
	chatClient := &scriptedChatClient{responses: []string{`{"done":false,"candidate_ids":["transcript-1"]}`, "owner answer [C1]"}}
	tools := NewVideoAgentTools(repos, NewRetrievalPipeline(repos, &fakeRetriever{results: []RetrievedChunk{{TaskID: task.ID, EvidenceID: "direct-hit", ChunkID: chunks[0].ID, ChunkIndex: 0, Content: chunks[0].Content}}}, NoopQueryRewriter{}, nil, DeterministicReranker{}, 1, 0), chatClient)
	runner := &evidenceFunnelRunner{
		repos: repos, tools: tools, planner: NewLLMEvidenceGapPlanner(chatClient), policy: policy,
		runtime:   VideoAgentToolRuntime{UserID: 7, TaskID: task.ID, TopK: 1, EmbeddingModel: "embed", Embedding: &fakeEmbeddingClient{dim: 3}},
		execution: &evidenceFunnelExecution{repo: repos.AgentExecution, userID: 7, runID: runID, now: func() time.Time { return time.Now().UTC() }},
	}
	return runner, chatClient, runID
}

func mustJSONForTest(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
