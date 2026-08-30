package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const AgentSnapshotVersion = 1

const (
	AgentStepStatusDone      = "done"
	AgentStepStatusError     = "error"
	AgentStepStatusCancelled = "cancelled"
)

// AgentSnapshot is the replayable terminal representation persisted in the
// existing chat_messages.retrieval_snapshot column. The legacy trace field is
// retained as a write-side compatibility alias for clients that still decode
// the previous Agent envelope.
type AgentSnapshot struct {
	Version   int                 `json:"version"`
	RunID     string              `json:"run_id"`
	Mode      string              `json:"mode"`
	Template  string              `json:"template,omitempty"`
	Steps     []AgentSnapshotStep `json:"steps"`
	Citations []Citation          `json:"citations"`
	Trace     []VideoAgentStep    `json:"trace,omitempty"`
	Memory    *MemorySnapshot     `json:"memory,omitempty"`
}

// AgentSnapshotStep is deliberately limited to safe execution metadata. It
// never contains model reasoning or provider token deltas.
type AgentSnapshotStep struct {
	StepID string         `json:"step_id"`
	Kind   string         `json:"kind"`
	Label  string         `json:"label"`
	Status string         `json:"status"`
	Tool   string         `json:"tool,omitempty"`
	Input  map[string]any `json:"input"`
	Output string         `json:"output,omitempty"`
	Error  string         `json:"error,omitempty"`
	TS     string         `json:"ts"`
}

// NewAgentSnapshot normalizes the internal legacy trace into the stable
// replay format. The array order is the execution order, and s1/s2/... remain
// stable for the lifetime of the persisted run.
func NewAgentSnapshot(runID, mode, template string, trace []VideoAgentStep, citations []Citation) AgentSnapshot {
	if strings.TrimSpace(runID) == "" {
		runID = uuid.NewString()
	}
	if strings.TrimSpace(mode) == "" {
		mode = AgentStreamMode
	}

	steps := make([]AgentSnapshotStep, 0, len(trace))
	for index, step := range trace {
		steps = append(steps, agentSnapshotStep(index+1, step))
	}
	if citations == nil {
		citations = []Citation{}
	}
	citationCopy := make([]Citation, len(citations))
	copy(citationCopy, citations)

	legacyTrace := append([]VideoAgentStep(nil), trace...)
	return AgentSnapshot{
		Version:   AgentSnapshotVersion,
		RunID:     runID,
		Mode:      mode,
		Template:  template,
		Steps:     steps,
		Citations: citationCopy,
		Trace:     legacyTrace,
	}
}

func agentSnapshotStep(number int, step VideoAgentStep) AgentSnapshotStep {
	input := cloneAgentStepInput(step.Input)
	status := AgentStepStatusDone
	if strings.TrimSpace(step.Error) != "" {
		status = AgentStepStatusError
	}
	return AgentSnapshotStep{
		StepID: fmt.Sprintf("s%d", number),
		Kind:   videoAgentStepKind(step.Tool),
		Label:  agentSnapshotStepLabel(step),
		Status: status,
		Tool:   strings.TrimSpace(step.Tool),
		Input:  input,
		Output: strings.TrimSpace(step.OutputRef),
		Error:  strings.TrimSpace(step.Error),
		TS:     time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func cloneAgentStepInput(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	clone := make(map[string]any, len(input))
	for key, value := range input {
		clone[key] = value
	}
	return clone
}

func agentSnapshotStepLabel(step VideoAgentStep) string {
	switch step.Tool {
	case VideoAgentToolSearchTranscript:
		return "检索转写片段"
	case VideoAgentToolGetTranscriptWindow:
		return "加载转写窗口"
	case VideoAgentToolSummarizeSegments:
		return "总结转写片段"
	case VideoAgentToolCompareSegments:
		return "对比转写片段"
	case VideoAgentToolBuildCitedAnswer:
		return "生成引用回答"
	}
	if label := strings.TrimSpace(step.Name); label != "" {
		return label
	}
	if tool := strings.TrimSpace(step.Tool); tool != "" {
		return tool
	}
	return "执行步骤"
}

// MarshalAgentSnapshot serializes the same terminal envelope for sync and
// streaming Agent results.
func MarshalAgentSnapshot(result *VideoAgentResult) ([]byte, error) {
	if result == nil {
		return nil, fmt.Errorf("agent result 不能为空")
	}
	snapshot := NewAgentSnapshot(result.RunID, result.Mode, result.Template, result.Trace, result.Citations)
	snapshot.Memory = result.Memory
	result.RunID = snapshot.RunID
	result.Mode = snapshot.Mode
	return json.Marshal(snapshot)
}

// DecodeAgentSnapshot accepts both the versioned envelope and historical
// retrieval_snapshot values: a bare Citation[] or the old object containing
// template/citations/trace.
func DecodeAgentSnapshot(raw string) (AgentSnapshot, error) {
	if strings.TrimSpace(raw) == "" {
		return AgentSnapshot{}, fmt.Errorf("agent snapshot 不能为空")
	}

	var arrayCitations []Citation
	if err := json.Unmarshal([]byte(raw), &arrayCitations); err == nil {
		if arrayCitations == nil {
			arrayCitations = []Citation{}
		}
		return AgentSnapshot{Citations: arrayCitations, Steps: []AgentSnapshotStep{}}, nil
	}

	var envelope struct {
		Version   int                 `json:"version"`
		RunID     string              `json:"run_id"`
		Mode      string              `json:"mode"`
		Template  string              `json:"template"`
		Steps     []AgentSnapshotStep `json:"steps"`
		Citations []Citation          `json:"citations"`
		Trace     []VideoAgentStep    `json:"trace"`
		Memory    *MemorySnapshot     `json:"memory"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return AgentSnapshot{}, fmt.Errorf("agent snapshot 无效: %w", err)
	}

	if envelope.Citations == nil {
		envelope.Citations = []Citation{}
	}
	if envelope.Steps != nil {
		envelope.Steps = normalizeAgentSnapshotSteps(envelope.Steps)
		return AgentSnapshot{
			Version: envelope.Version, RunID: envelope.RunID, Mode: envelope.Mode,
			Template: envelope.Template, Steps: envelope.Steps, Citations: envelope.Citations,
			Trace: append([]VideoAgentStep(nil), envelope.Trace...), Memory: envelope.Memory,
		}, nil
	}

	// Old Agent envelopes had no version and stored the internal trace field.
	if envelope.Trace != nil {
		snapshot := NewAgentSnapshot(envelope.RunID, envelope.Mode, envelope.Template, envelope.Trace, envelope.Citations)
		snapshot.Version = envelope.Version
		snapshot.Memory = envelope.Memory
		return snapshot, nil
	}

	return AgentSnapshot{
		Version: envelope.Version, RunID: envelope.RunID, Mode: envelope.Mode,
		Template: envelope.Template, Steps: []AgentSnapshotStep{}, Citations: envelope.Citations,
		Memory: envelope.Memory,
	}, nil
}

func normalizeAgentSnapshotSteps(steps []AgentSnapshotStep) []AgentSnapshotStep {
	for index := range steps {
		step := &steps[index]
		if strings.TrimSpace(step.StepID) == "" {
			step.StepID = fmt.Sprintf("s%d", index+1)
		}
		if strings.TrimSpace(step.Kind) == "" {
			step.Kind = videoAgentStepKind(step.Tool)
		}
		if strings.TrimSpace(step.Label) == "" {
			step.Label = step.Tool
			if step.Label == "" {
				step.Label = "执行步骤"
			}
		}
		if strings.TrimSpace(step.Status) == "" {
			step.Status = AgentStepStatusDone
		}
		if step.Input == nil {
			step.Input = map[string]any{}
		}
		if strings.TrimSpace(step.TS) == "" {
			step.TS = time.Now().UTC().Format(time.RFC3339Nano)
		}
	}
	return steps
}
