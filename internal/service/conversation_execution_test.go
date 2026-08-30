package service

import (
	"context"
	"errors"
	"testing"

	"vid-lens/internal/ai"
)

type stubConversationProfileProvider struct{ profile ai.Profile }

func (s stubConversationProfileProvider) GetDefaultAIProfile(int64) (*ai.Profile, error) {
	profile := s.profile
	return &profile, nil
}

type stubConversationClientFactory struct{}

func (stubConversationClientFactory) NewEmbeddingClient(ai.Profile) (ai.EmbeddingClient, error) {
	return nil, nil
}
func (stubConversationClientFactory) NewChatClient(ai.Profile) (ai.ChatClient, error) {
	return nil, nil
}

type recordingConversationAgent struct {
	template VideoAgentRequest
	research VideoResearchRequest
	funnel   EvidenceFunnelRequest
	stream   VideoAgentStreamRequest
	events   []AgentStreamEvent
	wait     bool
}

type recordingConversationChat struct {
	mode   ChatMode
	events []ChatStreamEvent
	wait   bool
}

func (c *recordingConversationChat) AskWithMode(_ context.Context, mode ChatMode, _, _ int64, _ string, _ int, _ ai.EmbeddingClient, _ ai.ChatClient, _ ai.Profile) (*AskResult, error) {
	c.mode = mode
	return &AskResult{Answer: "chat"}, nil
}

func (c *recordingConversationChat) AskStreamWithMode(ctx context.Context, mode ChatMode, _, _ int64, _ string, _ int, _ ai.EmbeddingClient, _ ai.ChatClient, _ ai.Profile, emit func(ChatStreamEvent) error) (*AskResult, error) {
	c.mode = mode
	if c.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	for _, event := range c.events {
		if err := emit(event); err != nil {
			return nil, err
		}
	}
	return &AskResult{Answer: "chat stream"}, nil
}

func (a *recordingConversationAgent) Ask(_ context.Context, req VideoAgentRequest, _ ai.EmbeddingClient, _ ai.ChatClient, _ ai.Profile) (*VideoAgentResult, error) {
	a.template = req
	return &VideoAgentResult{Answer: "template"}, nil
}
func (a *recordingConversationAgent) AskResearch(_ context.Context, req VideoResearchRequest, _ ai.EmbeddingClient, _ ai.ChatClient, _ ai.Profile) (*VideoAgentResult, error) {
	a.research = req
	return &VideoAgentResult{Answer: "research"}, nil
}
func (a *recordingConversationAgent) AskEvidenceFunnel(_ context.Context, req EvidenceFunnelRequest, _ ai.EmbeddingClient, _ ai.ChatClient, _ ai.Profile) (*VideoAgentResult, error) {
	a.funnel = req
	return &VideoAgentResult{Answer: "funnel"}, nil
}
func (a *recordingConversationAgent) Stream(ctx context.Context, req VideoAgentStreamRequest, _ ai.EmbeddingClient, _ ai.ChatClient, _ ai.Profile, emit func(AgentStreamEvent) error) (*VideoAgentResult, error) {
	a.stream = req
	if a.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	for _, event := range a.events {
		if err := emit(event); err != nil {
			return nil, err
		}
	}
	return &VideoAgentResult{Answer: "stream"}, nil
}

func TestConversationExecutionSelectsAgentModeBehindOneInterface(t *testing.T) {
	agent := &recordingConversationAgent{}
	execution := NewConversationExecution(nil, agent, stubConversationProfileProvider{profile: ai.Profile{LLMModel: "chat"}}, stubConversationClientFactory{})

	result, err := execution.Execute(context.Background(), ConversationRequest{
		Kind: ConversationKindAgent, UserID: 7, SessionID: 22, Question: "goal", TopK: 4,
		Mode: "research", RunID: "run-1",
	})
	if err != nil || result.Agent == nil || result.Agent.Answer != "research" {
		t.Fatalf("research Execute() = %+v, %v", result, err)
	}
	if agent.research.UserID != 7 || agent.research.SessionID != 22 || agent.research.Goal != "goal" || agent.research.RunID != "run-1" {
		t.Fatalf("research request = %+v", agent.research)
	}

	result, err = execution.Execute(context.Background(), ConversationRequest{
		Kind: ConversationKindAgent, UserID: 7, SessionID: 22, Question: "verify", TopK: 3,
		Mode: string(VideoAgentEvidenceFunnelTemplate), RunID: "run-2",
	})
	if err != nil || result.Agent == nil || result.Agent.Answer != "funnel" || agent.funnel.RunID != "run-2" {
		t.Fatalf("funnel Execute() = %+v, %v request=%+v", result, err, agent.funnel)
	}
}

func TestConversationExecutionPreservesAgentStreamEventsAndCancellation(t *testing.T) {
	agent := &recordingConversationAgent{events: []AgentStreamEvent{{Type: AgentEventAnswer, Data: "delta"}}}
	execution := NewConversationExecution(nil, agent, stubConversationProfileProvider{}, stubConversationClientFactory{})
	var events []ConversationStreamEvent
	_, err := execution.Stream(context.Background(), ConversationRequest{
		Kind: ConversationKindAgent, UserID: 7, SessionID: 22, Question: "q", Mode: AgentStreamMode,
	}, func(event ConversationStreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil || len(events) != 1 || events[0].Type != AgentEventAnswer || events[0].Data != "delta" {
		t.Fatalf("stream events = %+v, %v", events, err)
	}

	agent.wait = true
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = execution.Stream(ctx, ConversationRequest{
		Kind: ConversationKindAgent, UserID: 7, SessionID: 22, Question: "q", Mode: AgentStreamMode,
	}, func(ConversationStreamEvent) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Stream() error = %v", err)
	}
}

func TestConversationExecutionPreservesChatModeEventsAndCancellation(t *testing.T) {
	chat := &recordingConversationChat{events: []ChatStreamEvent{{Type: "answer", Data: "delta"}, {Type: "done", Data: map[string]any{"message_id": 3}}}}
	execution := NewConversationExecution(chat, nil, stubConversationProfileProvider{}, stubConversationClientFactory{})
	var events []ConversationStreamEvent
	_, err := execution.Stream(context.Background(), ConversationRequest{
		Kind: ConversationKindChat, UserID: 7, SessionID: 22, Question: "q", Mode: string(ChatModeVideoAssistant),
	}, func(event ConversationStreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil || chat.mode != ChatModeVideoAssistant || len(events) != 2 || events[0].Type != "answer" || events[1].Type != "done" {
		t.Fatalf("chat Stream() events=%+v mode=%q err=%v", events, chat.mode, err)
	}

	chat.wait = true
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = execution.Stream(ctx, ConversationRequest{Kind: ConversationKindChat, UserID: 7, SessionID: 22, Question: "q"}, func(ConversationStreamEvent) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled chat Stream() error = %v", err)
	}
}
