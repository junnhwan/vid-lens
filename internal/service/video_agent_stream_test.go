package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

func TestVideoAgentStreamEmitsStableEventsForExistingTemplateAgent(t *testing.T) {
	repos, task, session := newVideoAgentTestSession(t)
	chatClient := &scriptedChatClient{responses: []string{"not-json", "直接回答 [C1]"}}
	ledger := NewEvidenceLedgerService(repos)
	policyService := NewMemoryPolicyService(repos.Memory, true)
	chatSvc := NewChatServiceWithDependencies(repos, &fakeRetriever{results: []RetrievedChunk{
		{TaskID: task.ID, EvidenceID: "ev-stream-1", ChunkID: 1, ChunkIndex: 2, Score: 0.91, Content: "stream citation"},
	}}, ChatConfig{TopK: 5, CandidateK: 5, MinScore: 0.3}, ChatDependencies{EvidenceLedger: ledger, MemoryPolicy: policyService})
	agent := NewVideoAgentService(chatSvc)

	var events []AgentStreamEvent
	result, err := agent.Stream(context.Background(), VideoAgentStreamRequest{
		UserID: session.UserID, SessionID: session.ID, Question: "为什么要校验 owner？", TopK: 1, Mode: AgentStreamMode,
	}, &fakeEmbeddingClient{dim: 3}, chatClient, ai.Profile{
		EmbeddingModel: "text-embedding-3-small", LLMModel: "chat-model",
	}, func(event AgentStreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	if result == nil || result.MessageID == 0 || result.RunID == "" || result.Mode != AgentStreamMode {
		t.Fatalf("result = %+v", result)
	}

	wantTypes := []string{
		AgentEventRunStart,
		AgentEventStepStart, AgentEventToolCall, AgentEventToolResult, AgentEventRetrieveHits, AgentEventStepDone,
		AgentEventStepStart, AgentEventToolCall, AgentEventToolResult,
		AgentEventAnswer, AgentEventCitations, AgentEventStepDone, AgentEventDone,
	}
	gotTypes := make([]string, 0, len(events))
	for _, event := range events {
		gotTypes = append(gotTypes, event.Type)
	}
	if len(gotTypes) != len(wantTypes) {
		t.Fatalf("event types = %#v, want %#v", gotTypes, wantTypes)
	}
	for i := range wantTypes {
		if gotTypes[i] != wantTypes[i] {
			t.Fatalf("event[%d] = %q, want %q; all=%#v", i, gotTypes[i], wantTypes[i], gotTypes)
		}
	}

	runStart, ok := events[0].Data.(AgentRunStartEvent)
	if !ok || runStart.RunID != result.RunID || runStart.Mode != AgentStreamMode || runStart.TaskID != task.ID || runStart.MemoryPolicy.Reason != model.MemoryPolicyReasonUserDisabled {
		t.Fatalf("run_start = %#v", events[0].Data)
	}
	startIDs := make(map[string]struct{})
	terminalIDs := make(map[string]string)
	for _, event := range events {
		switch event.Type {
		case AgentEventStepStart:
			step, ok := event.Data.(AgentStepEvent)
			if !ok || step.Status != "running" || step.RunID != result.RunID {
				t.Fatalf("step_start = %#v", event.Data)
			}
			startIDs[step.StepID] = struct{}{}
		case AgentEventStepDone, AgentEventStepError:
			step, ok := event.Data.(AgentStepEvent)
			if !ok || step.RunID != result.RunID {
				t.Fatalf("terminal step = %#v", event.Data)
			}
			if _, exists := terminalIDs[step.StepID]; exists {
				t.Fatalf("step %s has more than one terminal event", step.StepID)
			}
			terminalIDs[step.StepID] = event.Type
		}
	}
	if len(startIDs) != 2 || len(terminalIDs) != len(startIDs) {
		t.Fatalf("step lifecycle starts=%v terminals=%v", startIDs, terminalIDs)
	}
	if done, ok := events[len(events)-1].Data.(AgentDoneEvent); !ok || done.RunID != result.RunID || done.MessageID != result.MessageID || done.TraceSummary.Steps != len(result.Trace) || done.MemoryPolicy != result.MemoryPolicy {
		t.Fatalf("done = %#v, trace=%#v", events[len(events)-1].Data, result.Trace)
	}
	if answer, ok := events[9].Data.(string); !ok || answer != result.Answer {
		t.Fatalf("answer event = %#v, result answer=%q", events[9].Data, result.Answer)
	}
	if ledgerView, err := ledger.GetRun(context.Background(), session.UserID, result.RunID); err != nil || ledgerView == nil || len(ledgerView.Claims) != 1 {
		t.Fatalf("stream ledger = %+v err=%v", ledgerView, err)
	}
}

func TestVideoAgentStreamEmitsStepErrorAndStopsOnToolFailure(t *testing.T) {
	repos, _, session := newVideoAgentTestSession(t)
	agent := NewVideoAgentService(NewChatService(repos, &failingRetriever{err: errors.New("retrieval unavailable")}, ChatConfig{TopK: 5}))
	var events []AgentStreamEvent
	_, err := agent.Stream(context.Background(), VideoAgentStreamRequest{
		UserID: session.UserID, SessionID: session.ID, Question: "测试检索失败", Mode: AgentStreamMode,
	}, &fakeEmbeddingClient{dim: 3}, &scriptedChatClient{responses: []string{"not-json"}}, ai.Profile{
		EmbeddingModel: "text-embedding-3-small", LLMModel: "chat-model",
	}, func(event AgentStreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err == nil || err.Error() != "retrieval unavailable" {
		t.Fatalf("Stream() error = %v", err)
	}
	gotTypes := make([]string, 0, len(events))
	for _, event := range events {
		gotTypes = append(gotTypes, event.Type)
	}
	wantTypes := []string{AgentEventRunStart, AgentEventStepStart, AgentEventToolCall, AgentEventStepError}
	if len(gotTypes) != len(wantTypes) {
		t.Fatalf("event types = %#v, want %#v", gotTypes, wantTypes)
	}
	for i := range wantTypes {
		if gotTypes[i] != wantTypes[i] {
			t.Fatalf("event[%d] = %q, want %q", i, gotTypes[i], wantTypes[i])
		}
	}
	stepError := events[len(events)-1].Data.(AgentStepEvent)
	if stepError.Status != "error" || stepError.Error != "retrieval unavailable" {
		t.Fatalf("step_error = %#v", stepError)
	}
}

func TestVideoAgentStreamStopsPromptlyWhenRequestIsCanceled(t *testing.T) {
	repos, _, session := newVideoAgentTestSession(t)
	retriever := &cancelAwareAgentRetriever{started: make(chan struct{})}
	agent := NewVideoAgentService(NewChatService(repos, retriever, ChatConfig{TopK: 5}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var events []AgentStreamEvent
	resultCh := make(chan error, 1)
	go func() {
		_, err := agent.Stream(ctx, VideoAgentStreamRequest{
			UserID: session.UserID, SessionID: session.ID, Question: "取消测试", Mode: AgentStreamMode,
		}, &fakeEmbeddingClient{dim: 3}, &scriptedChatClient{responses: []string{"not-json"}}, ai.Profile{
			EmbeddingModel: "text-embedding-3-small", LLMModel: "chat-model",
		}, func(event AgentStreamEvent) error {
			events = append(events, event)
			return nil
		})
		resultCh <- err
	}()

	<-retriever.started
	cancel()
	select {
	case err := <-resultCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Stream() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled stream did not stop promptly")
	}

	for _, event := range events {
		if event.Type == AgentEventAnswer || event.Type == AgentEventCitations || event.Type == AgentEventDone {
			t.Fatalf("canceled stream emitted terminal success event: %#v", events)
		}
	}
}

func TestVideoAgentStreamKeepsResearchAndKnowledgeBaseScopesClosed(t *testing.T) {
	repos, _, session := newVideoAgentTestSession(t)
	agent := NewVideoAgentService(NewChatService(repos, &fakeRetriever{}, ChatConfig{TopK: 5}))
	profile := ai.Profile{EmbeddingModel: "text-embedding-3-small", LLMModel: "chat-model"}

	_, err := agent.Stream(context.Background(), VideoAgentStreamRequest{
		UserID: session.UserID, SessionID: session.ID, Question: "研究测试", Mode: "research",
	}, &fakeEmbeddingClient{dim: 3}, &scriptedChatClient{}, profile, func(AgentStreamEvent) error { return nil })
	if err == nil {
		t.Fatal("research mode unexpectedly succeeded on agent stream endpoint")
	}

	kbSession := &model.ChatSession{UserID: session.UserID, ScopeType: model.ChatScopeKnowledgeBase, KnowledgeBaseID: 1}
	if err := repos.Chat.CreateSession(kbSession); err != nil {
		t.Fatalf("create knowledge-base session: %v", err)
	}
	_, err = agent.Stream(context.Background(), VideoAgentStreamRequest{
		UserID: session.UserID, SessionID: kbSession.ID, Question: "知识库测试", Mode: AgentStreamMode,
	}, &fakeEmbeddingClient{dim: 3}, &scriptedChatClient{}, profile, func(AgentStreamEvent) error { return nil })
	if err == nil || err.Error() != "知识库会话暂不支持 Agent 问答" {
		t.Fatalf("knowledge-base stream error = %v", err)
	}
}

type cancelAwareAgentRetriever struct {
	started chan struct{}
}

func (r *cancelAwareAgentRetriever) Search(ctx context.Context, _ []float32, _ RetrievalRequest) ([]RetrievedChunk, error) {
	select {
	case <-r.started:
	default:
		close(r.started)
	}
	<-ctx.Done()
	return nil, ctx.Err()
}
