package service

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Progress is public execution metadata, separate from provider reasoning.
type ConversationProgress struct {
	ID            string   `json:"id"`
	RunID         string   `json:"run_id,omitempty"`
	PlanID        string   `json:"plan_id,omitempty"`
	Kind          string   `json:"kind"`
	Label         string   `json:"label"`
	Status        string   `json:"status"`
	Detail        string   `json:"detail,omitempty"`
	InputSummary  string   `json:"input_summary,omitempty"`
	OutputSummary string   `json:"output_summary,omitempty"`
	Tool          string   `json:"tool,omitempty"`
	EvidenceRefs  []string `json:"evidence_refs,omitempty"`
	Replan        bool     `json:"replan,omitempty"`
	DurationMs    int64    `json:"duration_ms,omitempty"`
	TS            string   `json:"ts"`
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
	if record := chatExecutionFromContext(ctx); record != nil {
		step := record.observe(p)
		p.Kind, p.Label, p.Tool = step.Kind, step.Label, step.Tool
		if summary, ok := step.Input["summary"].(string); ok {
			p.InputSummary = summary
		}
		p.OutputSummary = step.Output
		p.Detail = step.Output
		p.DurationMs = step.DurationMs
	}
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
		return trimRunes(text, 120)
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
		progress.Detail = "规划未完成；请查看运行状态或重试"
	} else if exhausted {
		progress.Status = "cancelled"
		progress.Detail = "已达到执行预算，停止规划"
	} else {
		progress.Status = "done"
		progress.Label = publicDecisionTitle(decision)
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

func safeConversationTitle(value string) string {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > 40 {
		return ""
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return ""
		}
	}
	lower := strings.ToLower(value)
	for _, unsafe := range []string{"http:", "https:", "token", "secret", "password", "authorization", "api_key", "object_key", "signed", "<", ">", "```"} {
		if strings.Contains(lower, unsafe) {
			return ""
		}
	}
	return value
}
func publicDecisionTitle(decision VideoAgentLoopDecision) string {
	if title := safeConversationTitle(decision.PublicTitle); title != "" && titleMatchesDecision(title, decision) {
		return title
	}
	if decision.Done {
		return "完成回答"
	}
	if decision.Tool != "" {
		return agentSnapshotStepLabel(VideoAgentStep{Tool: decision.Tool})
	}
	return "规划下一步"
}

// A public title describes the actual allowed action. Unsupported wording
// falls back to the tool label rather than inventing download/write activity.
func titleMatchesDecision(title string, decision VideoAgentLoopDecision) bool {
	if decision.Done {
		return false
	}
	var verbs []string
	switch decision.Tool {
	case VideoAgentToolSearchTranscript, VideoAgentToolSearchVisualEvidence:
		verbs = []string{"检索", "搜索", "查找", "定位", "search", "find", "locate"}
	case VideoAgentToolGetTranscriptWindow, VideoAgentToolInspectVisualWindow:
		verbs = []string{"阅读", "查看", "读取", "核对", "检查", "摘录", "read", "inspect", "check", "review"}
	case VideoAgentToolBuildCitedAnswer:
		verbs = []string{"整理", "回答", "撰写", "生成回答", "组织", "汇总", "核对回答", "compose", "answer", "write", "build"}
	default:
		return false
	}
	lower := strings.ToLower(title)
	for _, verb := range verbs {
		if strings.HasPrefix(lower, verb) {
			return true
		}
	}
	return false
}
