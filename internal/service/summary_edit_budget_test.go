package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/summarydoc"
)

type summaryBudgetResponse struct {
	content string
	usage   *ai.ChatUsage
	finish  string
}

// Exercise the real provider adapter on a local HTTP server, without a model.
func summaryBudgetProvider(t *testing.T, responses ...summaryBudgetResponse) (ai.ChatClient, <-chan map[string]json.RawMessage, *atomic.Int32) {
	t.Helper()
	requests := make(chan map[string]json.RawMessage, len(responses)+1)
	calls := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "invalid request", 400)
			return
		}
		requests <- request
		index := int(calls.Add(1)) - 1
		if index >= len(responses) {
			t.Error("unexpected provider call")
			http.Error(w, "unexpected call", 500)
			return
		}
		response := responses[index]
		body := map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": response.content}, "finish_reason": response.finish}}}
		if response.usage != nil {
			body["usage"] = map[string]any{"prompt_tokens": response.usage.PromptTokens, "completion_tokens": response.usage.CompletionTokens}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(server.Close)
	return ai.NewOpenAIChatClient(server.URL, "", "fixture"), requests, calls
}

func summaryBudgetOperation(t *testing.T, svc *SummaryRevisionService, db *gorm.DB, base summarydoc.Document, key string, output int64) (*model.SummaryEditOperation, string) {
	t.Helper()
	op, err := svc.begin(context.Background(), 7, 142, key, SummaryEditInput{Instruction: "完善章节标题", Mode: "preview"})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Model(&model.AgentRun{}).Where("id = ?", op.RunID).Update("max_completion_tokens", output).Error; err != nil {
		t.Fatal(err)
	}
	hash, _ := summarydoc.Digest(base)
	title := "安装及适用条件"
	return op, artifact.JSON(summarydoc.Patch{BaseContentHashKind: summarydoc.HashKind, BaseContentHash: hash, Operations: []summarydoc.Operation{{Op: summarydoc.OpUpdateBlock, BlockID: "install", Title: &title}}})
}

func TestSummaryDocumentEditProviderBudgetAndActualUsageAcrossRepair(t *testing.T) {
	svc, db, base, _ := documentEditFixture(t)
	op, patch := summaryBudgetOperation(t, svc, db, base, "budget-repair", 90)
	client, requests, calls := summaryBudgetProvider(t,
		summaryBudgetResponse{content: `{}`, usage: &ai.ChatUsage{PromptTokens: 40, CompletionTokens: 30}},
		summaryBudgetResponse{content: patch, usage: &ai.ChatUsage{PromptTokens: 50, CompletionTokens: 20}},
	)
	svc.chat = client
	if err := svc.ExecuteSummaryEdit(context.Background(), op.RunID); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("bounded repair did not make exactly two calls")
	}
	for _, want := range []string{"90", "60"} {
		request := <-requests
		if string(request["max_tokens"]) != want {
			t.Fatalf("max_tokens=%s, want remaining budget %s", request["max_tokens"], want)
		}
	}
	records, err := svc.repos.AgentExecution.GetExecution(context.Background(), 7, op.RunID)
	if err != nil || records.Run.CompletionTokensUsed != 50 || records.Run.PromptTokensUsed != 90 || (records.Run.TokenUsageSource != model.AgentCallUsageActual && records.Run.TokenUsageSource != model.AgentCallUsageMixed) {
		t.Fatalf("actual run usage not persisted: %+v, %v", records, err)
	}
	for _, call := range records.ToolCalls {
		if call.UsageSource != model.AgentCallUsageActual || call.TokenEstimated || call.CompletionTokens <= 0 {
			t.Fatalf("actual provider usage lost: %+v", call)
		}
	}
	view, err := svc.Operation(context.Background(), 7, 142, op.ID)
	if err != nil || view.Status != "proposed" {
		t.Fatalf("valid repair did not publish preview: %+v, %v", view, err)
	}
}

func TestSummaryDocumentEditRepairCannotExceedTotalOutputBudget(t *testing.T) {
	svc, db, base, _ := documentEditFixture(t)
	op, _ := summaryBudgetOperation(t, svc, db, base, "budget-no-repair-spend", 90)
	client, _, calls := summaryBudgetProvider(t, summaryBudgetResponse{content: `{}`, usage: &ai.ChatUsage{PromptTokens: 40, CompletionTokens: 90}})
	svc.chat = client
	err := svc.ExecuteSummaryEdit(context.Background(), op.RunID)
	var domain *artifact.Error
	if !errors.As(err, &domain) || domain.Code != "budget_exhausted" || calls.Load() != 1 {
		t.Fatalf("repair exceeded total output budget: %v calls=%d", err, calls.Load())
	}
	view, err := svc.Operation(context.Background(), 7, 142, op.ID)
	if err != nil || view.Status != "failed" || view.ErrorCode != "budget_exhausted" {
		t.Fatalf("budget failure left operation running: %+v %v", view, err)
	}
}

func TestSummaryDocumentEditMissingUsageRemainsEstimated(t *testing.T) {
	svc, db, base, _ := documentEditFixture(t)
	op, patch := summaryBudgetOperation(t, svc, db, base, "budget-estimated", 900)
	client, _, _ := summaryBudgetProvider(t, summaryBudgetResponse{content: patch})
	svc.chat = client
	if err := svc.ExecuteSummaryEdit(context.Background(), op.RunID); err != nil {
		t.Fatal(err)
	}
	records, err := svc.repos.AgentExecution.GetExecution(context.Background(), 7, op.RunID)
	if err != nil || len(records.ToolCalls) != 1 {
		t.Fatalf("missing call record: %+v, %v", records, err)
	}
	call := records.ToolCalls[0]
	if call.UsageSource != model.AgentCallUsageEstimated || !call.TokenEstimated || call.PromptTokens <= 0 || call.CompletionTokens <= 0 || records.Run.TokenUsageSource != model.AgentCallUsageEstimated {
		t.Fatalf("missing provider usage was treated as actual/zero: %+v", call)
	}
	if records.Run.CostUsageSource != model.AgentCallUsageUnknown {
		t.Fatal("missing price/usage fabricated actual cost")
	}
}

func TestSummaryDocumentEditProviderFailureRetainsReportedUsage(t *testing.T) {
	svc, db, base, _ := documentEditFixture(t)
	op, _ := summaryBudgetOperation(t, svc, db, base, "budget-truncated", 90)
	client, requests, calls := summaryBudgetProvider(t, summaryBudgetResponse{content: `{"partial":`, finish: "length", usage: &ai.ChatUsage{PromptTokens: 40, CompletionTokens: 90}})
	svc.chat = client
	if err := svc.ExecuteSummaryEdit(context.Background(), op.RunID); err == nil {
		t.Fatal("truncated output published a preview")
	}
	if calls.Load() != 1 || string((<-requests)["max_tokens"]) != "90" {
		t.Fatal("failed call ignored output budget or retried")
	}
	records, err := svc.repos.AgentExecution.GetExecution(context.Background(), 7, op.RunID)
	if err != nil || len(records.ToolCalls) != 1 || records.ToolCalls[0].UsageSource != model.AgentCallUsageActual || records.ToolCalls[0].CompletionTokens != 90 || records.Run.CompletionTokensUsed != 90 {
		t.Fatalf("failed call actual usage lost: %+v %v", records, err)
	}
}

type summaryDeadlineChat struct {
	deadline time.Time
	calls    int
}

func (c *summaryDeadlineChat) Chat(ctx context.Context, _ []ai.ChatMessage) (string, error) {
	c.calls++
	c.deadline, _ = ctx.Deadline()
	<-ctx.Done()
	return "", ctx.Err()
}

func TestSummaryDocumentEditFrozenDeadlineStopsWorkerAndSyncExecution(t *testing.T) {
	for _, worker := range []bool{false, true} {
		t.Run(map[bool]string{false: "sync", true: "worker"}[worker], func(t *testing.T) {
			svc, db, base, _ := documentEditFixture(t)
			op, _ := summaryBudgetOperation(t, svc, db, base, "budget-deadline", 900)
			created := time.Now().UTC().Add(-time.Second)
			deadline := created.Add(1400 * time.Millisecond)
			if err := db.Model(&model.AgentRun{}).Where("id=?", op.RunID).Updates(map[string]any{"created_at": created, "max_duration_ms": 1400}).Error; err != nil {
				t.Fatal(err)
			}
			chat := &summaryDeadlineChat{}
			svc.chat = chat
			var err error
			if worker {
				err = svc.ExecuteSummaryEdit(context.Background(), op.RunID)
			} else {
				_, err = svc.execute(context.Background(), op)
			}
			var domain *artifact.Error
			if !errors.As(err, &domain) || domain.Code != "budget_exhausted" || chat.calls != 1 || !chat.deadline.Equal(deadline) {
				t.Fatalf("frozen deadline not enforced: err=%v calls=%d deadline=%v want=%v", err, chat.calls, chat.deadline, deadline)
			}
			view, err := svc.Operation(context.Background(), 7, 142, op.ID)
			if err != nil || view.Status != "failed" || view.ErrorCode != "budget_exhausted" || view.Preview != nil {
				t.Fatalf("deadline did not durably fail without preview: %+v %v", view, err)
			}
			records, err := svc.repos.AgentExecution.GetExecution(context.Background(), 7, op.RunID)
			if err != nil || !records.Run.CreatedAt.Equal(created) || records.Run.MaxDurationMs != 1400 || records.Run.RunLeaseToken != "" || records.Run.RunLeaseUntil != nil {
				t.Fatalf("worker extended budget or retained lease: %+v %v", records, err)
			}
		})
	}
}

func TestSummaryDocumentEditExpiredFrozenRunNeverCallsProvider(t *testing.T) {
	svc, db, base, chat := documentEditFixture(t)
	op, _ := summaryBudgetOperation(t, svc, db, base, "budget-already-expired", 900)
	if err := db.Model(&model.AgentRun{}).Where("id=?", op.RunID).Updates(map[string]any{"created_at": time.Now().Add(-time.Minute), "max_duration_ms": 1}).Error; err != nil {
		t.Fatal(err)
	}
	err := svc.ExecuteSummaryEdit(context.Background(), op.RunID)
	var domain *artifact.Error
	if !errors.As(err, &domain) || domain.Code != "budget_exhausted" || chat.calls != 0 {
		t.Fatalf("expired restored run dispatched provider: %v calls=%d", err, chat.calls)
	}
}
