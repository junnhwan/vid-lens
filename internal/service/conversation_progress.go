package service

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Progress is public execution metadata, separate from provider reasoning.
type ConversationProgress struct {
	ID           string   `json:"id"`
	RunID        string   `json:"run_id,omitempty"`
	PlanID       string   `json:"plan_id,omitempty"`
	Kind         string   `json:"kind"`
	Label        string   `json:"label"`
	Status       string   `json:"status"`
	Detail       string   `json:"detail,omitempty"`
	Tool         string   `json:"tool,omitempty"`
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
	Replan       bool     `json:"replan,omitempty"`
	DurationMs   int64    `json:"duration_ms,omitempty"`
	TS           string   `json:"ts"`
}
type ConversationReasoning struct {
	CallID string `json:"call_id"`
	RunID  string `json:"run_id,omitempty"`
	Delta  string `json:"delta"`
}
type conversationProgressContext struct {
	emit      func(ConversationProgress) error
	reasoning func(ConversationReasoning) error
	callID    string
}
type conversationProgressKey struct{}

func progressContext(ctx context.Context) conversationProgressContext {
	p, _ := ctx.Value(conversationProgressKey{}).(conversationProgressContext)
	return p
}
func emitProgress(ctx context.Context, p ConversationProgress) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.TS = time.Now().UTC().Format(time.RFC3339Nano)
	if sink := progressContext(ctx).emit; sink != nil {
		return sink(p)
	}
	return nil
}
func emitReasoning(ctx context.Context, callID, delta string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if sink := progressContext(ctx).reasoning; sink != nil {
		return sink(ConversationReasoning{CallID: callID, Delta: delta})
	}
	return nil
}

func publicDecisionSummary(d VideoAgentLoopDecision, evidence int) string {
	if text := strings.TrimSpace(d.PublicSummary); text != "" {
		return trimRunes(text, 240)
	}
	action := agentSnapshotStepLabel(VideoAgentStep{Tool: d.Tool})
	if d.Replan {
		return fmt.Sprintf("已有 %d 条候选证据，调整策略：%s。", evidence, action)
	}
	return fmt.Sprintf("基于当前 %d 条候选证据，下一步：%s。", evidence, action)
}

// A single lifecycle surrounds both fresh and checkpoint-backed decisions.
func (r *VideoAgentLoopRunner) nextResearchDecision(ctx context.Context, state VideoAgentLoopState, runtime VideoAgentToolRuntime) (decision VideoAgentLoopDecision, exhausted bool, err error) {
	id := fmt.Sprintf("plan-%d", state.CurrentStep+1)
	progress := ConversationProgress{ID: id, PlanID: id, Kind: "plan", Label: "规划下一步", Status: "running"}
	if err = emitProgress(ctx, progress); err != nil {
		return
	}
	started := time.Now()
	p := progressContext(ctx)
	p.callID = id
	ctx = context.WithValue(ctx, conversationProgressKey{}, p)
	decision, exhausted, err = r.nextResearchDecisionCheckpoint(ctx, state, runtime)
	progress.DurationMs = time.Since(started).Milliseconds()
	if err != nil {
		progress.Status = "error"
		progress.Detail = err.Error()
	} else if exhausted {
		progress.Status = "cancelled"
		progress.Detail = "已达到执行预算，停止规划"
	} else {
		progress.Status = "done"
		progress.Tool = decision.Tool
		progress.Replan = decision.Replan
		progress.Detail = publicDecisionSummary(decision, len(state.Evidence))
		for _, e := range state.Evidence {
			if e.EvidenceID != "" {
				progress.EvidenceRefs = append(progress.EvidenceRefs, e.EvidenceID)
			}
		}
	}
	if emitErr := emitProgress(ctx, progress); err == nil {
		err = emitErr
	}
	return
}
