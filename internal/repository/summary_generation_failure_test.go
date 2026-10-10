package repository

import (
	"testing"
	"vid-lens/internal/model"
)

func TestSummaryGenerationQueuePreservesBudgetStopReason(t *testing.T) {
	for _, cause := range []string{"budget_exhausted", "budget_exhausted: input_tokens", "context_budget_exhausted", "provider-private-text"} {
		t.Run(cause, func(t *testing.T) {
			db := summaryRevisionDB(t)
			repos := NewRepositories(db)
			run := model.AgentRun{ID: "failure-generation", UserID: 91, TaskID: 9, SubjectKind: model.AgentRunSubjectSummaryGeneration, SubjectID: "failure-generation", Status: model.AgentRunStatusRunning, Stage: "text_summary", ProfileSnapshot: "{}", PolicySnapshot: "{}", BudgetSnapshot: "{}"}
			if err := db.Create(&run).Error; err != nil {
				t.Fatal(err)
			}
			job := model.TaskJob{GenerationID: run.ID, UserID: run.UserID, TaskID: run.TaskID}
			if err := repos.recordSummaryGenerationQueueFailure(&job, TaskProcessingFailureRequest{Status: model.TaskStatusFailed, ErrorCode: "non_retryable_error", ErrorMessage: cause}); err != nil {
				t.Fatal(err)
			}
			var stored model.AgentRun
			_ = db.First(&stored, "id=?", run.ID).Error
			expected := cause
			if cause == "budget_exhausted: input_tokens" {
				expected = "budget_exhausted"
			}
			if cause == "provider-private-text" {
				expected = "summary_generation_failed"
			}
			if stored.StopReason != expected || stored.ErrorCode != expected || stored.ErrorMessage != "" {
				t.Fatalf("cause lost or private text leaked: %+v", stored)
			}
		})
	}
}
