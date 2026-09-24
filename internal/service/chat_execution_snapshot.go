package service

import (
	"context"
	"strings"
	"time"

	"vid-lens/internal/ai"
)

// This record contains only server-authored, public execution metadata. Never
// copy provider reasoning, prompts, tool arguments, or raw provider errors.
type chatExecutionStep struct {
	ID         string         `json:"step_id"`
	Kind       string         `json:"kind"`
	Label      string         `json:"label"`
	Status     string         `json:"status"`
	Input      map[string]any `json:"input,omitempty"`
	Output     string         `json:"output,omitempty"`
	Tool       string         `json:"tool,omitempty"`
	DurationMs int64          `json:"duration_ms,omitempty"`
	TS         string         `json:"ts,omitempty"`
}

type chatExecutionRecord struct {
	Mode    string
	Profile ai.Profile
	Steps   []chatExecutionStep
	starts  map[string]time.Time
}

type chatExecutionRecordKey struct{}

func withChatExecutionRecord(ctx context.Context, mode ChatMode, profile ai.Profile) context.Context {
	return context.WithValue(ctx, chatExecutionRecordKey{}, &chatExecutionRecord{Mode: string(normalizeChatMode(mode)), Profile: profile, starts: map[string]time.Time{}})
}

func chatExecutionFromContext(ctx context.Context) *chatExecutionRecord {
	record, _ := ctx.Value(chatExecutionRecordKey{}).(*chatExecutionRecord)
	return record
}

func (r *chatExecutionRecord) observe(p ConversationProgress) chatExecutionStep {
	if r == nil {
		return chatExecutionStep{}
	}
	// IDs are fixed by this server; unknown Agent or provider events are ignored.
	var step chatExecutionStep
	switch p.ID {
	case "prepare":
		step = chatExecutionStep{ID: p.ID, Kind: "prepare", Label: "准备会话上下文", Input: map[string]any{"summary": "当前会话近期对话与授权视频范围"}, Output: "已加载当前轮可用上下文"}
	case "retrieve":
		step = chatExecutionStep{ID: p.ID, Kind: "retrieve", Label: "检索视频证据", Input: map[string]any{"summary": "当前问题、授权范围与本轮向量模型"}, Tool: "video_evidence_search", Output: p.Detail}
	case "fallback":
		step = chatExecutionStep{ID: p.ID, Kind: "retrieve", Label: "检索降级", Output: "检索不可用，改用当前视频摘要或转写；无检索引用"}
	case "answer":
		step = chatExecutionStep{ID: p.ID, Kind: "answer", Label: "生成回答", Input: map[string]any{"summary": "本轮上下文与已选证据"}, Output: "回答已生成"}
	default:
		return chatExecutionStep{Kind: p.Kind, Label: p.Label, Tool: p.Tool, Output: p.Detail}
	}
	if p.Status == "running" {
		r.starts[p.ID] = time.Now()
	}
	step.Status = p.Status
	if step.Status != "running" {
		if started, ok := r.starts[p.ID]; ok {
			step.DurationMs = time.Since(started).Milliseconds()
		}
	}
	if p.Status == "error" {
		step.Output = "步骤未完成"
	}
	if p.Status == "running" {
		step.Output = ""
	}
	step.TS = p.TS
	for i := range r.Steps {
		if r.Steps[i].ID == p.ID {
			r.Steps[i] = step
			return step
		}
	}
	r.Steps = append(r.Steps, step)
	return step
}

func (r *chatExecutionRecord) completedSteps() []chatExecutionStep {
	if r == nil || len(r.Steps) == 0 {
		return []chatExecutionStep{{ID: "prepare", Kind: "prepare", Label: "准备会话上下文", Status: "done", Output: "已加载当前轮可用上下文"}, {ID: "answer", Kind: "answer", Label: "生成回答", Status: "done", Output: "回答已生成"}}
	}
	steps := append([]chatExecutionStep(nil), r.Steps...)
	for i := range steps {
		if steps[i].Status == "running" {
			steps[i].Status = "done"
		}
		if steps[i].ID == "retrieve" && steps[i].Status == "done" && !strings.HasPrefix(steps[i].Output, "找到 ") {
			steps[i].Output = "检索已完成"
		}
	}
	return steps
}
