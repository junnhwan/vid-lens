package service

import (
	"context"
	"errors"

	"vid-lens/internal/ai"
)

type ConversationKind string

const (
	ConversationKindChat  ConversationKind = "chat"
	ConversationKindAgent ConversationKind = "agent"
)

type ConversationRequest struct {
	Kind         ConversationKind
	UserID       int64
	SessionID    int64
	Question     string
	TopK         int
	Mode         string
	RunID        string
	AgentProfile string
}

type ConversationResult struct {
	Chat  *AskResult
	Agent *VideoAgentResult
}

func (r ConversationResult) Payload() any {
	if r.Agent != nil {
		return r.Agent
	}
	return r.Chat
}

type ConversationStreamEvent struct {
	Type string
	Data any
}

type ConversationStreamSink func(ConversationStreamEvent) error

type ConversationPreparationError struct{ Cause error }

func (e *ConversationPreparationError) Error() string { return e.Cause.Error() }
func (e *ConversationPreparationError) Unwrap() error { return e.Cause }

type ConversationProfileProvider interface {
	GetDefaultAIProfile(userID int64) (*ai.Profile, error)
}

type ConversationClientFactory interface {
	NewEmbeddingClient(profile ai.Profile) (ai.EmbeddingClient, error)
	NewChatClient(profile ai.Profile) (ai.ChatClient, error)
}

type ConversationAgent interface {
	Ask(context.Context, VideoAgentRequest, ai.EmbeddingClient, ai.ChatClient, ai.Profile) (*VideoAgentResult, error)
	AskResearch(context.Context, VideoResearchRequest, ai.EmbeddingClient, ai.ChatClient, ai.Profile) (*VideoAgentResult, error)
	AskEvidenceFunnel(context.Context, EvidenceFunnelRequest, ai.EmbeddingClient, ai.ChatClient, ai.Profile) (*VideoAgentResult, error)
	Stream(context.Context, VideoAgentStreamRequest, ai.EmbeddingClient, ai.ChatClient, ai.Profile, func(AgentStreamEvent) error) (*VideoAgentResult, error)
}

type ConversationChat interface {
	AskWithMode(context.Context, ChatMode, int64, int64, string, int, ai.EmbeddingClient, ai.ChatClient, ai.Profile) (*AskResult, error)
	AskStreamWithMode(context.Context, ChatMode, int64, int64, string, int, ai.EmbeddingClient, ai.ChatClient, ai.Profile, func(ChatStreamEvent) error) (*AskResult, error)
}

// ConversationExecution owns profile/client preparation, mode selection and
// cancellation-preserving execution for every chat endpoint.
type ConversationExecution struct {
	chat     ConversationChat
	agent    ConversationAgent
	profiles ConversationProfileProvider
	clients  ConversationClientFactory
}

func NewConversationExecution(chat ConversationChat, agent ConversationAgent, profiles ConversationProfileProvider, clients ConversationClientFactory) *ConversationExecution {
	return &ConversationExecution{chat: chat, agent: agent, profiles: profiles, clients: clients}
}

func (e *ConversationExecution) Execute(ctx context.Context, req ConversationRequest) (ConversationResult, error) {
	embedding, chat, profile, err := e.prepareClients(req.UserID)
	if err != nil {
		return ConversationResult{}, &ConversationPreparationError{Cause: err}
	}
	if req.Kind == ConversationKindAgent {
		if e.agent == nil {
			return ConversationResult{}, errors.New("agent 实验功能不可用")
		}
		var result *VideoAgentResult
		switch req.Mode {
		case "research":
			result, err = e.agent.AskResearch(ctx, VideoResearchRequest{
				UserID: req.UserID, SessionID: req.SessionID, Goal: req.Question, TopK: req.TopK, RunID: req.RunID,
			}, embedding, chat, profile)
		case string(VideoAgentEvidenceFunnelTemplate):
			result, err = e.agent.AskEvidenceFunnel(ctx, EvidenceFunnelRequest{
				UserID: req.UserID, SessionID: req.SessionID, Goal: req.Question, TopK: req.TopK, RunID: req.RunID,
			}, embedding, chat, profile)
		default:
			result, err = e.agent.Ask(ctx, VideoAgentRequest{
				UserID: req.UserID, SessionID: req.SessionID, Question: req.Question, TopK: req.TopK,
			}, embedding, chat, profile)
		}
		return ConversationResult{Agent: result}, err
	}
	if e.chat == nil {
		return ConversationResult{}, errors.New("chat service unavailable")
	}
	result, err := e.chat.AskWithMode(ctx, ChatMode(req.Mode), req.UserID, req.SessionID, req.Question, req.TopK, embedding, chat, profile)
	return ConversationResult{Chat: result}, err
}

func (e *ConversationExecution) Stream(ctx context.Context, req ConversationRequest, sink ConversationStreamSink) (ConversationResult, error) {
	if sink == nil {
		return ConversationResult{}, errors.New("conversation stream sink 不能为空")
	}
	embedding, chat, profile, err := e.prepareClients(req.UserID)
	if err != nil {
		return ConversationResult{}, &ConversationPreparationError{Cause: err}
	}
	if req.Kind == ConversationKindAgent {
		if e.agent == nil {
			return ConversationResult{}, errors.New("agent 流式功能不可用")
		}
		result, streamErr := e.agent.Stream(ctx, VideoAgentStreamRequest{
			UserID: req.UserID, SessionID: req.SessionID, Question: req.Question, TopK: req.TopK,
			Mode: req.Mode, AgentProfile: req.AgentProfile,
		}, embedding, chat, profile, func(event AgentStreamEvent) error {
			return sink(ConversationStreamEvent{Type: event.Type, Data: event.Data})
		})
		return ConversationResult{Agent: result}, streamErr
	}
	if e.chat == nil {
		return ConversationResult{}, errors.New("chat service unavailable")
	}
	result, streamErr := e.chat.AskStreamWithMode(ctx, ChatMode(req.Mode), req.UserID, req.SessionID, req.Question, req.TopK, embedding, chat, profile, func(event ChatStreamEvent) error {
		return sink(ConversationStreamEvent{Type: event.Type, Data: event.Data})
	})
	return ConversationResult{Chat: result}, streamErr
}

func (e *ConversationExecution) prepareClients(userID int64) (ai.EmbeddingClient, ai.ChatClient, ai.Profile, error) {
	if e == nil || e.profiles == nil || e.clients == nil {
		return nil, nil, ai.Profile{}, errors.New("conversation execution dependencies unavailable")
	}
	profile, err := e.profiles.GetDefaultAIProfile(userID)
	if err != nil {
		return nil, nil, ai.Profile{}, err
	}
	if profile == nil {
		return nil, nil, ai.Profile{}, errors.New("default AI profile unavailable")
	}
	embedding, err := e.clients.NewEmbeddingClient(*profile)
	if err != nil {
		return nil, nil, ai.Profile{}, err
	}
	chat, err := e.clients.NewChatClient(*profile)
	if err != nil {
		return nil, nil, ai.Profile{}, err
	}
	return embedding, chat, *profile, nil
}
