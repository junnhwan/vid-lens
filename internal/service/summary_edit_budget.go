package service

import (
	"context"
	"time"

	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

// Resuming or renewing a worker does not start a new duration budget.
func (s *SummaryRevisionService) summaryEditBudgetContext(parent context.Context, op *model.SummaryEditOperation) (context.Context, context.CancelFunc, error) {
	if op.BaseDocumentJSON != "" {
		records, err := s.repos.AgentExecution.GetExecution(parent, op.UserID, op.RunID)
		if err != nil {
			ctx, cancel := context.WithCancel(parent)
			return ctx, cancel, err
		}
		if records == nil || records.Run.SubjectKind != model.AgentRunSubjectSummaryEdit || records.Run.SubjectID != op.ID || records.Run.RecipeVersion != summaryDocumentEditRecipe {
			ctx, cancel := context.WithCancel(parent)
			return ctx, cancel, artifact.Err("unsupported_checkpoint", 409)
		}
		run := records.Run
		if run.MaxDurationMs > 0 {
			ctx, cancel := context.WithDeadlineCause(parent, run.CreatedAt.Add(time.Duration(run.MaxDurationMs)*time.Millisecond), errAgentRunDurationLimit)
			return ctx, cancel, nil
		}
	}
	ctx, cancel := context.WithCancel(parent)
	return ctx, cancel, nil
}
