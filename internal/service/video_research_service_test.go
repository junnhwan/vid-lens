package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

func TestVideoAgentAskResearchRunsPlannerToolAndPersistsAnswer(t *testing.T) {
	repos, task, session := newVideoAgentTestSession(t)
	retriever := &fakeRetriever{results: []RetrievedChunk{{
		TaskID: task.ID, EvidenceID: "ev-research-1", ChunkID: 1, ChunkIndex: 2, Content: "owner 校验证据",
	}}}
	chatClient := &scriptedChatClient{responses: []string{
		`{"done":false,"tool":"search_transcript","reason":"先定位转写证据","arguments":{"question":"owner 校验","top_k":1}}`,
		"not-json",
		`{"done":false,"tool":"build_cited_answer","reason":"证据已经足够，生成回答","arguments":{"question":"owner 校验","intermediate":"视频明确要求校验 owner。","citations":[{"task_id":999,"evidence_id":"ev-research-1","chunk_id":999,"chunk_index":999,"content":"被篡改的引用","source":"planner-forged"}]}}`,
		"最终研究答案 [C1]",
		`{"done":true,"stop_reason":"已生成带引用回答"}`,
	}}
	chatSvc := NewChatService(repos, retriever, ChatConfig{TopK: 5, CandidateK: 5, MinScore: 0.3})
	ledger := NewEvidenceLedgerService(repos)
	chatSvc.SetEvidenceLedger(ledger)
	agent := NewVideoAgentService(chatSvc)

	result, err := agent.AskResearch(context.Background(), VideoResearchRequest{
		UserID: 7, SessionID: session.ID, Goal: "请研究 owner 校验", TopK: 1,
	}, &fakeEmbeddingClient{dim: 3}, chatClient, ai.Profile{
		EmbeddingModel: "text-embedding-3-small", LLMModel: "chat-model",
	})
	if err != nil {
		t.Fatalf("AskResearch() error = %v", err)
	}
	if result.Answer != "最终研究答案" || result.Template != string(VideoAgentResearchTemplate) || result.Model != "chat-model" {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Citations) != 1 || result.Citations[0].CitationID != "C1" || result.Citations[0].TaskID != task.ID || result.Citations[0].ChunkID != 1 || result.Citations[0].Content != "owner 校验证据" || result.Citations[0].Source == "planner-forged" {
		t.Fatalf("citations = %+v", result.Citations)
	}
	if traceTools(result.Trace) != "search_transcript|build_cited_answer" {
		t.Fatalf("trace = %+v", result.Trace)
	}

	messages, err := repos.Chat.ListMessages(7, session.ID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 2 || messages[1].Content != "最终研究答案" || messages[1].RetrievalSnapshot == nil {
		t.Fatalf("messages = %+v", messages)
	}
	var snapshot struct {
		Template  string           `json:"template"`
		Trace     []VideoAgentStep `json:"trace"`
		Citations []Citation       `json:"citations"`
	}
	if err := json.Unmarshal([]byte(*messages[1].RetrievalSnapshot), &snapshot); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if snapshot.Template != string(VideoAgentResearchTemplate) || traceTools(snapshot.Trace) != "search_transcript|build_cited_answer" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if len(snapshot.Citations) != 1 || snapshot.Citations[0].TaskID != task.ID || snapshot.Citations[0].ChunkID != 1 || snapshot.Citations[0].Content != "owner 校验证据" || snapshot.Citations[0].Source == "planner-forged" {
		t.Fatalf("snapshot citations are not canonical: %+v", snapshot.Citations)
	}
	if strings.Contains(messages[1].Content, "[C") {
		t.Fatalf("stored answer leaked citation markers: %q", messages[1].Content)
	}
	ledgerView, err := ledger.GetRun(context.Background(), 7, result.RunID)
	if err != nil || ledgerView == nil || len(ledgerView.Claims) != 1 || len(ledgerView.Evidence) != 1 {
		t.Fatalf("research ledger = %+v err=%v", ledgerView, err)
	}
	if ledgerView.Evidence[0].TaskID != task.ID || ledgerView.Evidence[0].QuoteText != "owner 校验证据" || ledgerView.Evidence[0].SourceRef != "ev-research-1" || strings.Contains(ledgerView.Evidence[0].StableLocator, "planner-forged") {
		t.Fatalf("research ledger evidence is not canonical: %+v", ledgerView.Evidence[0])
	}
	execution, err := repos.AgentExecution.GetExecution(context.Background(), 7, result.RunID)
	if err != nil || execution == nil || execution.Run.Status != "completed" || len(execution.Steps) != 5 || len(execution.ToolCalls) != 5 {
		t.Fatalf("research execution = %+v err=%v", execution, err)
	}
	plannerCalls := 0
	for _, call := range execution.ToolCalls {
		if strings.Contains(call.InputSummary, "owner 校验") || strings.Contains(call.InputSummary, "被篡改") {
			t.Fatalf("tool input summary leaked raw planner/tool input: %s", call.InputSummary)
		}
		if call.ArgumentsDigest == "" || call.CallDigest == "" || call.ResultDigest == "" {
			t.Fatalf("tool call digests are incomplete: %+v", call)
		}
		if call.CallKind == model.AgentCallKindPlannerLLM {
			plannerCalls++
			if call.ToolName != videoResearchPlannerCall || call.UsageSource != model.AgentCallUsageEstimated || !call.TokenEstimated || call.PromptTokens <= 0 || call.Status != model.AgentToolCallStatusCompleted {
				t.Fatalf("planner call metadata = %+v", call)
			}
		}
	}
	if plannerCalls != 3 || execution.Run.ToolCallsUsed != 2 || execution.Run.LLMCallsUsed != 4 {
		t.Fatalf("planner/tool counters = planner:%d run:%+v", plannerCalls, execution.Run)
	}
	if execution.Run.RetrievalCallsUsed != 1 || execution.Run.VisualCallsUsed != 0 || execution.Run.FramesUsed != 0 ||
		execution.Run.PromptTokensUsed <= 0 || execution.Run.CompletionTokensUsed <= 0 || execution.Run.CostMicrosUsed != 0 ||
		execution.Run.DurationMsUsed < 0 || execution.Run.ContextCharsUsed <= 0 || execution.Run.TokenUsageSource != model.AgentCallUsageEstimated ||
		execution.Run.CostUsageSource != model.AgentCallUsageUnknown || execution.Run.ContextUsageSource != model.AgentCallUsageEstimated {
		t.Fatalf("research budget usage = %+v", execution.Run)
	}
	if execution.Run.MaxRetrievalCalls != 8 || execution.Run.MaxVisualCalls != 0 || execution.Run.MaxFrames != 0 || execution.Run.MaxPromptTokens <= 0 ||
		execution.Run.MaxCompletionTokens <= 0 || execution.Run.MaxCostMicros <= 0 || execution.Run.MaxDurationMs <= 0 || execution.Run.MaxContextChars <= 0 {
		t.Fatalf("research budget limits = %+v", execution.Run)
	}
}

func TestVideoAgentResearchResumePersistsOneExchangePerRun(t *testing.T) {
	repos, task, session := newVideoAgentTestSession(t)
	retriever := &fakeRetriever{results: []RetrievedChunk{{
		TaskID: task.ID, EvidenceID: "ev-idempotent", ChunkID: 1, ChunkIndex: 1, Content: "owner 校验证据",
	}}}
	chatSvc := NewChatService(repos, retriever, ChatConfig{TopK: 1, CandidateK: 1, MinScore: 0.1})
	agent := NewVideoAgentService(chatSvc)
	profile := ai.Profile{EmbeddingModel: "embed", LLMModel: "chat-model"}
	req := VideoResearchRequest{UserID: 7, SessionID: session.ID, Goal: "请研究 owner 校验", TopK: 1, RunID: "research-exchange-idempotency"}
	first, err := agent.AskResearch(context.Background(), req, &fakeEmbeddingClient{dim: 3}, &scriptedChatClient{responses: []string{
		`{"done":false,"tool":"search_transcript","reason":"定位证据","arguments":{"question":"owner 校验","top_k":1}}`,
		"not-json",
		`{"done":false,"tool":"build_cited_answer","reason":"生成回答","arguments":{"question":"owner 校验","intermediate":"视频要求校验 owner。","citations":[{"task_id":999,"evidence_id":"ev-idempotent","chunk_id":999,"chunk_index":999,"content":"不可信引用"}]}}`,
		"最终研究答案 [C1]",
		`{"done":true,"stop_reason":"已生成回答"}`,
	}}, profile)
	if err != nil {
		t.Fatalf("first AskResearch() error = %v", err)
	}

	resumed, err := agent.ResumeResearch(context.Background(), 7, req.RunID, &fakeEmbeddingClient{dim: 3}, &scriptedChatClient{}, profile)
	if err != nil {
		t.Fatalf("ResumeResearch() error = %v", err)
	}
	if resumed.MessageID != first.MessageID || resumed.Answer != first.Answer {
		t.Fatalf("resumed result = %+v, first = %+v", resumed, first)
	}
	// This invalid request policy must be ignored for the already-terminal run;
	// loading the frozen result is also the final duplicate-write guard.
	repeated, err := agent.AskResearch(context.Background(), VideoResearchRequest{
		UserID: 7, SessionID: session.ID, Goal: req.Goal, TopK: 1, RunID: req.RunID,
		Policy: VideoResearchPolicy{MaxSteps: 0, MaxReplans: -1},
	}, &fakeEmbeddingClient{dim: 3}, &scriptedChatClient{}, profile)
	if err != nil {
		t.Fatalf("repeated AskResearch() error = %v", err)
	}
	if repeated.MessageID != first.MessageID || repeated.Answer != first.Answer {
		t.Fatalf("repeated result = %+v, first = %+v", repeated, first)
	}
	messages, err := repos.Chat.ListMessages(7, session.ID)
	if err != nil || len(messages) != 2 || messages[0].Role != "user" || messages[1].Role != "assistant" {
		t.Fatalf("run exchange messages = %+v, %v", messages, err)
	}
}

func TestVideoAgentResearchUsesFrozenPolicyWhenCurrentPolicyIsInvalid(t *testing.T) {
	repos, task, session := newVideoAgentTestSession(t)
	retriever := &fakeRetriever{results: []RetrievedChunk{{
		TaskID: task.ID, EvidenceID: "ev-frozen-policy", ChunkID: 1, ChunkIndex: 1, Content: "owner 校验证据",
	}}}
	chatSvc := NewChatService(repos, retriever, ChatConfig{TopK: 1, CandidateK: 1, MinScore: 0.1})
	agent := NewVideoAgentService(chatSvc)
	profile := ai.Profile{EmbeddingModel: "embed", LLMModel: "chat-model"}
	frozenPolicy, frozenBudget := researchAgentPolicy(1, VideoResearchPolicy{MaxSteps: 3, MaxReplans: 1})
	const runID = "research-frozen-policy"
	created, err := agent.ensureAgentRun(context.Background(), runID, 7, session, "请研究 owner 校验", string(VideoAgentResearchTemplate), "default", profile, frozenPolicy, frozenBudget)
	if err != nil {
		t.Fatalf("ensureAgentRun() error = %v", err)
	}
	if created.MaxSteps != frozenBudget.MaxSteps || created.MaxRetrievalCalls != frozenBudget.MaxRetrievalCalls {
		t.Fatalf("stored frozen budget = %+v", created)
	}

	result, err := agent.AskResearch(context.Background(), VideoResearchRequest{
		UserID: 7, SessionID: session.ID, Goal: created.Goal, TopK: 1, RunID: runID,
		Policy: VideoResearchPolicy{MaxSteps: 0, MaxReplans: -1},
	}, &fakeEmbeddingClient{dim: 3}, &scriptedChatClient{responses: []string{
		`{"done":false,"tool":"search_transcript","reason":"定位证据","arguments":{"question":"owner 校验","top_k":1}}`,
		"not-json",
		`{"done":false,"tool":"build_cited_answer","reason":"生成回答","arguments":{"question":"owner 校验","intermediate":"视频要求校验 owner。","citations":[{"task_id":999,"evidence_id":"ev-frozen-policy","chunk_id":999,"chunk_index":999,"content":"不可信引用"}]}}`,
		"冻结 policy 仍可恢复 [C1]",
		`{"done":true,"stop_reason":"已生成回答"}`,
	}}, profile)
	if err != nil {
		t.Fatalf("AskResearch() with invalid current policy error = %v", err)
	}
	if result.Answer != "冻结 policy 仍可恢复" {
		t.Fatalf("frozen-policy result = %+v", result)
	}
	run, err := repos.AgentExecution.GetRun(context.Background(), 7, runID)
	if err != nil || run == nil || run.MaxSteps != frozenBudget.MaxSteps || run.MaxRetrievalCalls != frozenBudget.MaxRetrievalCalls {
		t.Fatalf("resumed run changed frozen budget = %+v, %v", run, err)
	}
}
