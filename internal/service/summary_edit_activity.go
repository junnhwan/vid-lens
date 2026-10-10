package service

import (
	"context"
	"time"
	"vid-lens/internal/model"
)

type SummaryEditActivity struct {
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	Status     string     `json:"status"`
	DurationMS int64      `json:"duration_ms"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

func (s *SummaryRevisionService) projectEditActivities(ctx context.Context, op *model.SummaryEditOperation, view *SummaryEditView) error {
	records, err := s.repos.AgentExecution.GetExecution(ctx, op.UserID, op.RunID)
	if err != nil {
		return err
	}
	if records == nil {
		return nil
	}
	view.Activities = []SummaryEditActivity{}
	for _, step := range records.Steps {
		title := "规划并校验摘要修订"
		switch step.Action {
		case "propose_literal_document_patch", "propose_literal_term_patch":
			title = "更正名称并校验摘要"
		case "propose_summary_patch", "propose_summary_document_patch":
		default:
			continue
		}
		status := map[string]string{model.AgentStepStatusRunning: "running", model.AgentStepStatusCompleted: "done", model.AgentStepStatusFailed: "error", model.AgentStepStatusAmbiguous: "error"}[step.Status]
		if status == "" {
			continue
		}
		if op.Status != "running" && status == "running" {
			status = "error"
		}
		view.Activities = append(view.Activities, SummaryEditActivity{ID: step.ID, Title: title, Status: status, DurationMS: step.DurationMs, StartedAt: step.StartedAt, FinishedAt: step.FinishedAt})
	}
	return nil
}
