package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

func TestChatDoesNotReretrieveAfterAnswerAndKeepsOnlyAvailableCitations(t *testing.T) {
	repos, task, session := newVideoAgentTestSession(t)
	retriever := &pipelineTestRetriever{results: [][]RetrievedChunk{{{TaskID: task.ID, ChunkID: 1, EvidenceID: "ev-chat", Content: "owner 校验"}}}}
	client := &scriptedChatClient{responses: []string{"not-json", "回答 [C1]，未提供的引用 [C99]。"}}
	svc := NewChatService(repos, retriever, ChatConfig{TopK: 1})
	result, err := svc.AskWithMode(context.Background(), ChatModeNatural, 7, session.ID, "owner 校验如何实现？", 1, &fakeEmbeddingClient{dim: 3}, client, ai.Profile{EmbeddingModel: "embed"})
	if err != nil {
		t.Fatal(err)
	}
	if len(retriever.requests) != 1 || len(client.messages) != 2 || len(result.Citations) != 1 || result.Citations[0].EvidenceID != "ev-chat" || strings.Contains(result.Answer, "[C") {
		t.Fatalf("unexpected second retrieval or citation: %+v calls=%d", result, len(retriever.requests))
	}
}

func TestConversationRejectsRetiredModesBeforePreparingClients(t *testing.T) {
	execution := NewConversationExecution(nil, nil, nil, nil)
	for _, mode := range []string{"strict_rag", "video_assistant", "research", "evidence_funnel", "unknown"} {
		for _, kind := range []ConversationKind{ConversationKindChat, ConversationKindAgent} {
			if _, err := execution.Execute(context.Background(), ConversationRequest{Kind: kind, Mode: mode}); err == nil {
				t.Fatalf("accepted %s %s", kind, mode)
			}
		}
	}
}

type incrementalAgentClient struct {
	scriptedChatClient
	streaming bool
	streams   int
}

func (c *incrementalAgentClient) StreamChat(ctx context.Context, messages []ai.ChatMessage, emit func(string) error) error {
	c.streams++
	c.streaming = true
	defer func() { c.streaming = false }()
	for _, delta := range []string{"最终", "回答 [C1]"} {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := emit(delta); err != nil {
			return err
		}
	}
	return nil
}

func TestAgentStreamsProviderDeltasAndOnlyFinishesAfterPersistence(t *testing.T) {
	for _, failSave := range []bool{false, true} {
		t.Run(fmt.Sprint(failSave), func(t *testing.T) {
			repos, task, session := newVideoAgentTestSession(t)
			chatSvc := NewChatService(repos, &fakeRetriever{results: []RetrievedChunk{{TaskID: task.ID, ChunkID: 1, EvidenceID: "ev-stream", Content: "证据"}}}, ChatConfig{TopK: 1})
			client := &incrementalAgentClient{scriptedChatClient: scriptedChatClient{responses: []string{testSearchDecision, testAnswerDecision("ev-stream", task.ID, 1)}}}
			deltas, dones := 0, 0
			result, err := NewVideoAgentService(chatSvc).Stream(context.Background(), VideoAgentStreamRequest{UserID: 7, SessionID: session.ID, Question: "owner"}, &fakeEmbeddingClient{dim: 3}, client, ai.Profile{EmbeddingModel: "embed"}, func(event AgentStreamEvent) error {
				if event.Type == AgentEventAnswer {
					if !client.streaming {
						t.Fatal("answer was buffered until provider completion")
					}
					deltas++
					if failSave {
						chatSvc.repos.Chat = nil
					}
				}
				if event.Type == AgentEventDone {
					dones++
					done := event.Data.(AgentDoneEvent)
					messages, err := repos.Chat.ListMessages(7, session.ID)
					if err != nil || len(messages) != 2 || messages[1].ID != done.MessageID || done.Answer != "最终回答" {
						t.Fatalf("done before authoritative persistence: %+v %v", done, err)
					}
				}
				return nil
			})
			if deltas != 2 || client.streams != 1 || len(client.messages) != 2 {
				t.Fatalf("calls/deltas: %+v %d", client, deltas)
			}
			if failSave {
				if err == nil || dones != 0 {
					t.Fatalf("failed save emitted success: %v %d", err, dones)
				}
			} else if err != nil || dones != 1 || result.Answer != "最终回答" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

type recordingPreferenceCapture struct{ requests []MemoryExtractionRequest }

func (c *recordingPreferenceCapture) EnqueueExtraction(req MemoryExtractionRequest) MemoryEnqueueResult {
	c.requests = append(c.requests, req)
	return MemoryEnqueueResult{Accepted: true}
}

func TestAgentReplayDoesNotRecallOrExtractMemoryAgain(t *testing.T) {
	repos, task, session := newVideoAgentTestSession(t)
	policy := NewMemoryPolicyService(repos.Memory, true)
	if _, err := policy.UpdateSessionPolicy(context.Background(), 7, session.ID, model.MemorySessionPolicyEnabled, 0); err != nil {
		t.Fatal(err)
	}
	provider := &policyCountingMemoryProvider{}
	capture := &recordingPreferenceCapture{}
	svc := NewChatServiceWithDependencies(repos, &fakeRetriever{results: []RetrievedChunk{{TaskID: task.ID, ChunkID: 1, EvidenceID: "ev-memory", Content: "证据"}}}, ChatConfig{TopK: 1}, ChatDependencies{MemoryPolicy: policy, LongTermMemory: provider, MemoryCapture: capture})
	agent := NewVideoAgentService(svc)
	req := VideoAgentLoopRequest{UserID: 7, SessionID: session.ID, Goal: "请简洁回答", RunID: "memory-replay"}
	client := &scriptedChatClient{responses: []string{testSearchDecision, testAnswerDecision("ev-memory", task.ID, 1), "回答 [C1]"}}
	profile := ai.Profile{EmbeddingModel: "embed"}
	first, err := agent.RunAgent(context.Background(), req, &fakeEmbeddingClient{dim: 3}, client, profile)
	if err != nil {
		t.Fatal(err)
	}
	calls := len(client.messages)
	second, err := agent.RunAgent(context.Background(), req, &fakeEmbeddingClient{dim: 3}, client, profile)
	if err != nil {
		t.Fatal(err)
	}
	messages, _ := repos.Chat.ListMessages(7, session.ID)
	if first.MessageID != second.MessageID || len(messages) != 2 || provider.calls != 1 || len(client.messages) != calls || len(capture.requests) != 1 || capture.requests[0].SourceRef != fmt.Sprintf("chat_message:%d", messages[0].ID) {
		t.Fatalf("duplicate memory or exchange: %+v %+v", messages, capture.requests)
	}
}

func TestAgentReservesFinalStepAndReportsMissingEvidence(t *testing.T) {
	tools := NewVideoAgentTools(nil, nil, &scriptedChatClient{responses: []string{"无法从当前视频确认。"}})
	runner, err := NewVideoAgentLoopRunner(tools.Registry(), &scriptedVideoAgentLoopPlanner{}, DefaultVideoAgentLoopObserver{}, VideoAgentLoopPolicy{MaxSteps: 1})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.Run(context.Background(), "视频中是什么颜色？", VideoAgentToolRuntime{TaskID: 1})
	if err != nil || result.State.Answer != "无法从当前视频确认。" || len(result.State.Citations) != 0 || len(result.State.Steps) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runner.Run(ctx, "cancel", VideoAgentToolRuntime{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}

func TestAgentModelBudgetReturnsEvidenceAndPreservesTerminalOnReplay(t *testing.T) {
	repos, task, session := newVideoAgentTestSession(t)
	agent := NewVideoAgentService(NewChatService(repos, &fakeRetriever{results: []RetrievedChunk{{TaskID: task.ID, ChunkID: 1, EvidenceID: "ev-budget", Content: "已取得的证据"}}}, ChatConfig{TopK: 1}))
	policy, budget := loopAgentPolicy(1, DefaultVideoAgentLoopPolicy())
	budget.MaxLLMCalls = 1
	profile := ai.Profile{EmbeddingModel: "embed"}
	if _, err := agent.ensureAgentRun(context.Background(), "limited-model", 7, session, "owner", AgentStreamMode, "default", profile, policy, budget); err != nil {
		t.Fatal(err)
	}
	client := &scriptedChatClient{responses: []string{testSearchDecision}}
	result, err := agent.RunAgent(context.Background(), VideoAgentLoopRequest{UserID: 7, SessionID: session.ID, Goal: "owner", RunID: "limited-model"}, &fakeEmbeddingClient{dim: 3}, client, profile)
	if err != nil || !result.Degraded || len(result.Citations) != 1 || !strings.Contains(result.Answer, "已取得的证据") || len(client.messages) != 1 {
		t.Fatalf("budget result=%+v err=%v calls=%d", result, err, len(client.messages))
	}
	run, err := repos.AgentExecution.GetRun(context.Background(), 7, result.RunID)
	if err != nil || run.Status != model.AgentRunStatusBudgetExhausted {
		t.Fatalf("run=%+v err=%v", run, err)
	}
	replay, err := agent.ResumeAgent(context.Background(), 7, result.RunID, &fakeEmbeddingClient{dim: 3}, client, profile)
	if err != nil || !replay.Degraded || replay.MessageID != result.MessageID || len(client.messages) != 1 {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
}
