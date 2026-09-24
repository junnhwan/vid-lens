package service

import (
	"context"
	"strings"
	"testing"

	"vid-lens/internal/ai"
)

func TestChatExecutionRecordNeverCopiesProviderErrorOrReasoning(t *testing.T) {
	ctx := withChatExecutionRecord(context.Background(), ChatModeNatural, ai.Profile{ID: 17, LLMModel: "model"})
	if err := emitProgress(ctx, ConversationProgress{ID: "retrieve", Kind: "retrieve", Status: "running", Detail: "api_key=secret"}); err != nil {
		t.Fatal(err)
	}
	if err := emitProgress(ctx, ConversationProgress{ID: "retrieve", Kind: "retrieve", Status: "error", Detail: "api_key=secret and private prompt"}); err != nil {
		t.Fatal(err)
	}
	record := chatExecutionFromContext(ctx)
	steps := record.completedSteps()
	if len(steps) != 1 || steps[0].Status != "error" || strings.Contains(steps[0].Output, "secret") || strings.Contains(steps[0].Output, "prompt") {
		t.Fatalf("unsafe persisted step: %+v", steps)
	}
}
