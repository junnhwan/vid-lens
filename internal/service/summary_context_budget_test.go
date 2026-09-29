package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

func TestSummaryEditContextWindowUsesTokensPerCall(t *testing.T) {
	base := strings.Repeat("这是摘要中的中文内容。", 140)
	chat := &summaryRepairChat{responses: []string{`{"edits":[{"anchor_id":"s1","new_text":"更新后的摘要内容。"}]}`}}
	svc, db := summaryRepairService(t, base, chat)
	if err := db.Model(&model.UserAIProfile{}).Where("user_id = ?", 7).Update("llm_context_tokens", 8192).Error; err != nil {
		t.Fatal(err)
	}
	accepted, err := svc.Submit(context.Background(), 7, 42, "summary-context-tokens", SummaryEditInput{Instruction: "将第一段精简成步骤", Mode: "preview"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ExecuteSummaryEdit(context.Background(), accepted.RunID); err != nil {
		t.Fatal(err)
	}
	if len(chat.messages) != 1 {
		t.Fatalf("provider calls=%d, want 1", len(chat.messages))
	}
	operation, err := svc.Operation(context.Background(), 7, 42, accepted.ID)
	if err != nil || operation.Status != "proposed" {
		t.Fatalf("operation=%+v err=%v", operation, err)
	}
}

func TestSummaryEditStillRejectsPromptBeyondSingleCallWindow(t *testing.T) {
	base := strings.Repeat("这是摘要中的中文内容。", 140)
	chat := &summaryRepairChat{responses: []string{`{"edits":[]}`}}
	svc, db := summaryRepairService(t, base, chat)
	if err := db.Model(&model.UserAIProfile{}).Where("user_id = ?", 7).Update("llm_context_tokens", 1024).Error; err != nil {
		t.Fatal(err)
	}
	accepted, err := svc.Submit(context.Background(), 7, 42, "summary-context-small", SummaryEditInput{Instruction: "将第一段精简成步骤", Mode: "preview"})
	if err != nil {
		t.Fatal(err)
	}
	err = svc.ExecuteSummaryEdit(context.Background(), accepted.RunID)
	var domain *artifact.Error
	if !errors.As(err, &domain) || domain.Code != "budget_exhausted" {
		t.Fatalf("error=%v, want budget_exhausted", err)
	}
	if len(chat.messages) != 0 {
		t.Fatalf("provider calls=%d, want 0", len(chat.messages))
	}
}
