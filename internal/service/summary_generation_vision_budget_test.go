package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

type summaryBudgetVisualFactory struct {
	base   *summaryVisualFixture
	vision ai.VisionClient
}

func (f summaryBudgetVisualFactory) NewChatClient(profile ai.Profile) (ai.ChatClient, error) {
	return f.base.NewChatClient(profile)
}
func (f summaryBudgetVisualFactory) NewVisionClient(ai.Profile) (ai.VisionClient, error) {
	return f.vision, nil
}

type summaryBudgetInspector struct {
	base *summaryVisualFixture
	path string
}

func (i summaryBudgetInspector) Inspect(ctx context.Context, req InspectRequest) (Investigation, error) {
	if _, err := req.VisionClient.CaptionImage(ctx, i.path, buildQueryVisualPrompt(req.Goal, req.RequiredFacts)); err != nil {
		return Investigation{Budget: VisualBudgetUsage{VLMCalls: 1}}, err
	}
	return i.base.Inspect(ctx, req)
}

func TestSummaryGenerationVisionUsagePersistsActualEstimatedAndPartialFailure(t *testing.T) {
	for _, mode := range []string{"actual", "missing", "partial"} {
		t.Run(mode, func(t *testing.T) {
			f := newGenerationFixture(t, false)
			base := enableGenerationVisualFixture(t, f)
			requests := make(chan map[string]json.RawMessage, 2)
			calls := &atomic.Int32{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					http.Error(w, "bad request", 400)
					return
				}
				requests <- request
				calls.Add(1)
				finish := "stop"
				if mode == "partial" {
					finish = "length"
				}
				body := map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"facts":["实际参数界面"],"gaps":[]}`}, "finish_reason": finish}}}
				if mode != "missing" {
					body["usage"] = map[string]any{"prompt_tokens": 100, "completion_tokens": 50}
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(body)
			}))
			t.Cleanup(server.Close)
			path := filepath.Join(t.TempDir(), "fixture.jpg")
			if err := os.WriteFile(path, []byte("local fixture bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			factory := summaryBudgetVisualFactory{base: base, vision: ai.NewOpenAIVisionClient(server.URL, "", "qwen3.6-flash")}
			f.svc.WithVisualEnricher(NewSummaryVisualService(f.repos, factory, summaryBudgetInspector{base: base, path: path}))
			if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 1 {
				t.Fatal("one inspection made additional provider calls")
			}
			request := <-requests
			if string(request["max_tokens"]) != "2048" {
				t.Fatalf("VLM did not inherit frozen shared cap: %s", request["max_tokens"])
			}
			records, err := repository.NewSummaryGenerationExecutionStore(f.repos, f.task.UserID, f.task.ID, f.job.GenerationID).GetExecution(context.Background(), f.task.UserID, f.job.GenerationID)
			if err != nil {
				t.Fatal(err)
			}
			var inspection *model.AgentToolCall
			for index := range records.ToolCalls {
				if records.ToolCalls[index].ToolName == "inspect_summary_frame" {
					inspection = &records.ToolCalls[index]
				}
			}
			if inspection == nil {
				t.Fatal("missing inspection journal")
			}
			if mode == "missing" {
				if inspection.UsageSource != model.AgentCallUsageEstimated || !inspection.TokenEstimated || inspection.CompletionTokens != 2048 || inspection.PromptTokens <= 0 {
					t.Fatalf("unknown VLM usage became zero/actual: %+v", inspection)
				}
			} else if inspection.UsageSource != model.AgentCallUsageActual || inspection.TokenEstimated || inspection.CompletionTokens != 50 || inspection.PromptTokens != 100 {
				t.Fatalf("actual/partial VLM usage lost: %+v", inspection)
			}
			if mode == "partial" {
				row, _ := f.repos.Summary.FindByTaskID(f.task.ID)
				if row.GeneratedVersion != 1 {
					t.Fatal("partial observation published a figure")
				}
			}
		})
	}
}
