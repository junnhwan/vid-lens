package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"vid-lens/internal/model"
)

type ChatRunHistory struct {
	RunID     string                 `json:"run_id"`
	Question  string                 `json:"question"`
	Status    string                 `json:"status"`
	Error     string                 `json:"error,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
	Steps     []ConversationProgress `json:"steps"`
}

// Failed runs do not have a committed assistant message. Return only public
// metadata, after checking both the session owner and the execution owner.
func (s *ChatService) ListUnfinishedRunHistory(ctx context.Context, userID, sessionID int64) ([]ChatRunHistory, error) {
	session, err := s.repos.Chat.FindSessionForUser(userID, sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, fmt.Errorf("会话不存在或无权限")
	}
	history := []ChatRunHistory{}
	if s.repos.AgentExecution == nil {
		return history, nil
	}
	runs, err := s.repos.AgentExecution.ListSessionTerminalRuns(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	for _, run := range runs {
		records, err := s.repos.AgentExecution.GetExecution(ctx, userID, run.ID)
		if err != nil {
			return nil, err
		}
		if records == nil || records.Run.SessionID != sessionID {
			continue
		}
		item := ChatRunHistory{RunID: run.ID, Question: run.Goal, Status: run.Status, Error: run.ErrorMessage, CreatedAt: run.CreatedAt, Steps: []ConversationProgress{}}
		latest := map[string]int{}
		for _, step := range records.Steps {
			status := "done"
			if step.Status != model.AgentStepStatusCompleted {
				status = "error"
				if run.Status == model.AgentRunStatusCancelled {
					status = "cancelled"
				}
			}
			p := ConversationProgress{ID: step.StepID, RunID: run.ID, Kind: step.Kind, Label: agentSnapshotStepLabel(VideoAgentStep{Tool: step.Action}), Status: status, Detail: step.OutputRef, Tool: step.Action, DurationMs: step.DurationMs, TS: step.StartedAt.UTC().Format(time.RFC3339Nano)}
			if step.Kind == "plan" {
				p.Label = "规划下一步"
				p.Tool = ""
				p.PlanID = step.StepID
				var decision durableResearchDecision
				if json.Unmarshal([]byte(step.ResultCheckpoint), &decision) == nil {
					p.Detail = decision.PublicSummary
				}
			} else if step.Sequence > 0 {
				p.ID = fmt.Sprintf("s%d", step.Sequence/2)
				p.PlanID = fmt.Sprintf("plan-%d", step.Sequence/2)
			}
			if step.ErrorMessage != "" {
				p.Detail = step.ErrorMessage
			}
			if index, ok := latest[p.ID]; ok {
				item.Steps[index] = p
			} else {
				latest[p.ID] = len(item.Steps)
				item.Steps = append(item.Steps, p)
			}
		}
		history = append(history, item)
	}
	return history, nil
}
