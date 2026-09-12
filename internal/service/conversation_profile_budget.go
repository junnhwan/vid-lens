package service

import (
	"context"
	"errors"
	"vid-lens/internal/ai"
)

type resolvedBudgetContextKey struct{}

func (e *ConversationExecution) prepareRequest(ctx context.Context, req ConversationRequest) (context.Context, ai.EmbeddingClient, ai.ChatClient, ai.Profile, error) {
	if req.Kind == ConversationKindAgent && e != nil && e.clients != nil {
		if provider, ok := e.profiles.(interface {
			GetDefaultConversationProfile(int64) (*ResolvedConversationProfile, error)
		}); ok {
			resolved, err := provider.GetDefaultConversationProfile(req.UserID)
			if err != nil {
				return ctx, nil, nil, ai.Profile{}, err
			}
			if resolved == nil || resolved.Profile == nil {
				return ctx, nil, nil, ai.Profile{}, errors.New("default conversation profile unavailable")
			}
			embedding, err := e.clients.NewEmbeddingClient(*resolved.Profile)
			if err != nil {
				return ctx, nil, nil, ai.Profile{}, err
			}
			chat, err := e.clients.NewChatClient(*resolved.Profile)
			return context.WithValue(ctx, resolvedBudgetContextKey{}, resolved), embedding, chat, *resolved.Profile, err
		}
	}
	embedding, chat, profile, err := e.prepareClients(req.UserID)
	return ctx, embedding, chat, profile, err
}

func applyResolvedAgentBudget(ctx context.Context, policy *frozenAgentPolicy, budget *frozenAgentBudget, visual bool) {
	resolved, ok := ctx.Value(resolvedBudgetContextKey{}).(*ResolvedConversationProfile)
	if !ok || resolved == nil {
		return
	}
	effective := resolved.EffectiveAgentBudget
	values := effective.Values
	policy.MaxSteps = values.MaxToolCalls
	policy.MaxReplans = min(policy.MaxReplans, values.MaxToolCalls-1)
	budget.SchemaVersion = 1
	budget.ProfileID = resolved.ProfileID
	budget.Source = effective.Source
	budget.Requested = resolved.AgentBudget
	budget.MaxSteps = values.MaxToolCalls*2 + 1
	budget.MaxToolCalls = values.MaxToolCalls
	budget.MaxLLMCalls = values.MaxToolCalls*2 + 1
	budget.MaxRetrievalCalls = values.MaxToolCalls
	budget.MaxDurationMs = int64(values.MaxDurationSeconds) * 1000
	budget.MaxPromptTokens = int64(values.MaxInputTokens)
	budget.MaxCompletionTokens = int64(values.MaxOutputTokens)
	budget.MaxContextChars = int64(values.MaxInputTokens) * 4
	budget.MaxCostMicros = 0 // No price table: money is unknown, not a fabricated budget.
	if visual && values.MaxVisualFrames != nil {
		budget.MaxFrames = *values.MaxVisualFrames
	}
	budget.ReserveInputTokens = int64(effective.FinalAnswerReserve.InputTokens)
	budget.ReserveOutputTokens = int64(effective.FinalAnswerReserve.OutputTokens)
	budget.ReserveDurationMs = int64(effective.FinalAnswerReserve.DurationSeconds) * 1000
}
