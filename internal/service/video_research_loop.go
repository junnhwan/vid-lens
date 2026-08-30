package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type VideoResearchStatus string

const (
	VideoResearchStatusRunning   VideoResearchStatus = "running"
	VideoResearchStatusCompleted VideoResearchStatus = "completed"
	VideoResearchStatusStopped   VideoResearchStatus = "stopped"
	VideoResearchStatusFailed    VideoResearchStatus = "failed"
)

type VideoResearchStepStatus string

const (
	VideoResearchStepRunning   VideoResearchStepStatus = "running"
	VideoResearchStepCompleted VideoResearchStepStatus = "completed"
	VideoResearchStepFailed    VideoResearchStepStatus = "failed"
)

type VideoResearchPolicy struct {
	MaxSteps   int `json:"max_steps"`
	MaxReplans int `json:"max_replans"`
}

func DefaultVideoResearchPolicy() VideoResearchPolicy {
	return VideoResearchPolicy{MaxSteps: 8, MaxReplans: 2}
}

func (p VideoResearchPolicy) Validate() error {
	if p.MaxSteps <= 0 {
		return errors.New("video research max_steps 必须大于 0")
	}
	if p.MaxReplans < 0 {
		return errors.New("video research max_replans 不能小于 0")
	}
	if p.MaxReplans >= p.MaxSteps {
		return errors.New("video research max_replans 必须小于 max_steps")
	}
	return nil
}

type VideoResearchDecision struct {
	Done       bool            `json:"done"`
	Tool       string          `json:"tool,omitempty"`
	Reason     string          `json:"reason,omitempty"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
	Replan     bool            `json:"replan,omitempty"`
	StopReason string          `json:"stop_reason,omitempty"`
}

type VideoResearchObservation struct {
	Tool                string           `json:"tool"`
	Output              json.RawMessage  `json:"output,omitempty"`
	Step                VideoAgentStep   `json:"step"`
	NewEvidence         []RetrievedChunk `json:"new_evidence,omitempty"`
	UnresolvedQuestions []string         `json:"unresolved_questions,omitempty"`
	Answer              string           `json:"answer,omitempty"`
	Citations           []Citation       `json:"citations,omitempty"`
}

type VideoResearchStep struct {
	Number      int                       `json:"number"`
	Action      VideoResearchDecision     `json:"action"`
	Status      VideoResearchStepStatus   `json:"status"`
	Trace       VideoAgentStep            `json:"trace"`
	Observation *VideoResearchObservation `json:"observation,omitempty"`
	Error       string                    `json:"error,omitempty"`
}

type VideoResearchState struct {
	Goal             string                     `json:"goal"`
	Status           VideoResearchStatus        `json:"status"`
	CurrentStep      int                        `json:"current_step"`
	ReplanCount      int                        `json:"replan_count"`
	MaxSteps         int                        `json:"max_steps"`
	MaxReplans       int                        `json:"max_replans"`
	StopReason       string                     `json:"stop_reason,omitempty"`
	PendingQuestions []string                   `json:"pending_questions,omitempty"`
	Evidence         []RetrievedChunk           `json:"evidence,omitempty"`
	Observations     []VideoResearchObservation `json:"observations,omitempty"`
	Steps            []VideoResearchStep        `json:"steps,omitempty"`
	Answer           string                     `json:"answer,omitempty"`
	Citations        []Citation                 `json:"citations,omitempty"`
	Memory           *MemorySnapshot            `json:"memory,omitempty"`
}

type VideoResearchResult struct {
	State VideoResearchState `json:"state"`
}

type VideoResearchPlanner interface {
	NextDecision(ctx context.Context, state VideoResearchState, tools []VideoAgentToolDefinition) (VideoResearchDecision, error)
}

type VideoResearchObserver interface {
	Observe(state VideoResearchState, result VideoAgentToolResult) (VideoResearchObservation, error)
}

type VideoResearchRunner struct {
	registry *VideoAgentToolRegistry
	planner  VideoResearchPlanner
	observer VideoResearchObserver
	policy   VideoResearchPolicy
}

func NewVideoResearchRunner(registry *VideoAgentToolRegistry, planner VideoResearchPlanner, observer VideoResearchObserver, policy VideoResearchPolicy) (*VideoResearchRunner, error) {
	if registry == nil {
		return nil, errors.New("video research tool registry 不能为空")
	}
	if planner == nil {
		return nil, errors.New("video research planner 不能为空")
	}
	if observer == nil {
		return nil, errors.New("video research observer 不能为空")
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	return &VideoResearchRunner{registry: registry, planner: planner, observer: observer, policy: policy}, nil
}

func NewVideoResearchState(goal string, policy VideoResearchPolicy) (VideoResearchState, error) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return VideoResearchState{}, errors.New("video research goal 不能为空")
	}
	if err := policy.Validate(); err != nil {
		return VideoResearchState{}, err
	}
	return VideoResearchState{
		Goal:         goal,
		Status:       VideoResearchStatusRunning,
		MaxSteps:     policy.MaxSteps,
		MaxReplans:   policy.MaxReplans,
		Evidence:     make([]RetrievedChunk, 0),
		Observations: make([]VideoResearchObservation, 0),
		Steps:        make([]VideoResearchStep, 0, policy.MaxSteps),
	}, nil
}

func (r *VideoResearchRunner) Run(ctx context.Context, goal string, runtime VideoAgentToolRuntime) (*VideoResearchResult, error) {
	if r == nil {
		return nil, errors.New("video research runner 不能为空")
	}
	state, err := NewVideoResearchState(goal, r.policy)
	if err != nil {
		return nil, err
	}
	state.Memory = runtime.MemorySnapshot
	result := &VideoResearchResult{State: state}

	for {
		if result.State.CurrentStep >= result.State.MaxSteps {
			result.State.Status = VideoResearchStatusStopped
			result.State.StopReason = "budget_exhausted"
			return result, nil
		}

		decision, err := r.planner.NextDecision(ctx, result.State, r.registry.Definitions())
		if err != nil {
			return r.fail(result, "planner_failure", err)
		}
		if err := r.validateDecision(result.State, decision); err != nil {
			return r.fail(result, "invalid_planner_decision", err)
		}
		if decision.Tool == VideoAgentToolBuildCitedAnswer {
			canonicalArguments, err := canonicalizeResearchAnswerArguments(result.State.Evidence, runtime.TaskID, decision.Arguments)
			if err != nil {
				return r.fail(result, "invalid_planner_decision", err)
			}
			decision.Arguments = canonicalArguments
		}
		if decision.Done {
			result.State.Status = VideoResearchStatusCompleted
			result.State.StopReason = firstNonEmpty(decision.StopReason, "goal_satisfied")
			return result, nil
		}
		if decision.Replan {
			result.State.ReplanCount++
			if result.State.ReplanCount > result.State.MaxReplans {
				result.State.ReplanCount--
				result.State.Status = VideoResearchStatusStopped
				result.State.StopReason = "replan_limit_reached"
				return result, nil
			}
		}

		step := VideoResearchStep{
			Number: result.State.CurrentStep + 1,
			Action: decision,
			Status: VideoResearchStepRunning,
		}
		toolResult, err := r.registry.Execute(ctx, decision.Tool, VideoAgentToolRequest{
			Runtime:   runtime,
			Arguments: decision.Arguments,
		})
		result.State.CurrentStep++
		step.Trace = toolResult.Step
		if err != nil {
			step.Status = VideoResearchStepFailed
			step.Error = err.Error()
			result.State.Steps = append(result.State.Steps, step)
			return r.fail(result, "tool_failure", err)
		}

		observation, err := r.observer.Observe(result.State, toolResult)
		if err != nil {
			step.Status = VideoResearchStepFailed
			step.Error = err.Error()
			result.State.Steps = append(result.State.Steps, step)
			return r.fail(result, "observer_failure", err)
		}
		if err := validateObservedResearchEvidence(runtime.TaskID, observation.NewEvidence); err != nil {
			step.Status = VideoResearchStepFailed
			step.Error = err.Error()
			result.State.Steps = append(result.State.Steps, step)
			return r.fail(result, "observer_failure", err)
		}
		step.Status = VideoResearchStepCompleted
		step.Observation = &observation
		result.State.Steps = append(result.State.Steps, step)
		result.State.Observations = append(result.State.Observations, observation)
		result.State.Evidence = mergeVideoResearchEvidence(result.State.Evidence, observation.NewEvidence)
		result.State.PendingQuestions = append([]string(nil), observation.UnresolvedQuestions...)
		if observation.Answer != "" {
			result.State.Answer = observation.Answer
			result.State.Citations = append([]Citation(nil), observation.Citations...)
		}
	}
}

func (r *VideoResearchRunner) validateDecision(state VideoResearchState, decision VideoResearchDecision) error {
	if decision.Done {
		if strings.TrimSpace(decision.Tool) != "" || decision.Replan {
			return errors.New("完成决策不能同时指定工具或 replan")
		}
		return nil
	}
	if strings.TrimSpace(decision.Tool) == "" {
		return errors.New("未完成决策必须指定工具")
	}
	if strings.TrimSpace(decision.Reason) == "" {
		return errors.New("工具决策必须说明 reason")
	}
	if decision.Replan && state.CurrentStep == 0 {
		return errors.New("第一步不能标记为 replan")
	}
	if len(decision.Arguments) == 0 || !json.Valid(decision.Arguments) {
		return errors.New("工具 arguments 必须是有效 JSON")
	}
	if _, err := r.registry.Lookup(decision.Tool); err != nil {
		return err
	}
	return nil
}

func (r *VideoResearchRunner) fail(result *VideoResearchResult, reason string, err error) (*VideoResearchResult, error) {
	result.State.Status = VideoResearchStatusFailed
	result.State.StopReason = reason
	return result, err
}

type DefaultVideoResearchObserver struct{}

func (DefaultVideoResearchObserver) Observe(state VideoResearchState, result VideoAgentToolResult) (VideoResearchObservation, error) {
	observation := VideoResearchObservation{
		Tool:   result.Step.Tool,
		Output: append(json.RawMessage(nil), result.Output...),
		Step:   result.Step,
	}
	if result.Step.Tool == VideoAgentToolSearchTranscript {
		var search SearchTranscriptResult
		if err := json.Unmarshal(result.Output, &search); err != nil {
			return VideoResearchObservation{}, fmt.Errorf("解析 search_transcript observation 失败: %w", err)
		}
		observation.NewEvidence = append([]RetrievedChunk(nil), search.Citations...)
	}
	if result.Step.Tool == VideoAgentToolBuildCitedAnswer {
		var answer BuildCitedAnswerResult
		if err := json.Unmarshal(result.Output, &answer); err != nil {
			return VideoResearchObservation{}, fmt.Errorf("解析 build_cited_answer observation 失败: %w", err)
		}
		canonical, err := canonicalizeResearchCitations(state.Evidence, 0, answer.Citations)
		if err != nil {
			return VideoResearchObservation{}, fmt.Errorf("canonicalize build_cited_answer observation 失败: %w", err)
		}
		answer.Citations = canonical
		canonicalOutput, err := json.Marshal(answer)
		if err != nil {
			return VideoResearchObservation{}, fmt.Errorf("序列化 canonical build_cited_answer observation 失败: %w", err)
		}
		observation.Output = canonicalOutput
		finalized := finalizeAnswerCitations(answer.Answer, buildCitations(state.Goal, canonical))
		observation.Answer = finalized.Answer
		observation.Citations = finalized.Citations
	}
	return observation, nil
}

func mergeVideoResearchEvidence(existing, added []RetrievedChunk) []RetrievedChunk {
	merged := append([]RetrievedChunk(nil), existing...)
	seen := make(map[string]struct{}, len(merged)+len(added))
	for _, chunk := range merged {
		seen[videoResearchEvidenceKey(chunk)] = struct{}{}
	}
	for _, chunk := range added {
		key := videoResearchEvidenceKey(chunk)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, chunk)
	}
	return merged
}

func videoResearchEvidenceKey(chunk RetrievedChunk) string {
	if strings.TrimSpace(chunk.EvidenceID) != "" {
		return "evidence:" + chunk.EvidenceID
	}
	return fmt.Sprintf("chunk:%d:%d", chunk.TaskID, chunk.ChunkID)
}

func canonicalizeResearchAnswerArguments(evidence []RetrievedChunk, taskID int64, arguments json.RawMessage) (json.RawMessage, error) {
	var input buildCitedAnswerToolArguments
	if err := decodeVideoAgentToolArguments(VideoAgentToolRequest{Arguments: arguments}, &input); err != nil {
		return nil, fmt.Errorf("解析 build_cited_answer arguments 失败: %w", err)
	}
	if len(input.Citations) == 0 {
		return nil, errors.New("build_cited_answer 必须提供已观察到的证据")
	}
	canonical, err := canonicalizeResearchCitations(evidence, taskID, input.Citations)
	if err != nil {
		return nil, err
	}
	input.Citations = canonical
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("序列化 canonical build_cited_answer arguments 失败: %w", err)
	}
	return encoded, nil
}

func canonicalizeResearchCitations(evidence []RetrievedChunk, taskID int64, requested []RetrievedChunk) ([]RetrievedChunk, error) {
	if len(requested) == 0 {
		return nil, errors.New("build_cited_answer 必须提供已观察到的证据")
	}
	byEvidenceID := make(map[string]RetrievedChunk, len(evidence))
	byChunk := make(map[string]RetrievedChunk, len(evidence))
	for _, observed := range evidence {
		if taskID > 0 && observed.TaskID != taskID {
			return nil, fmt.Errorf("已观察证据越过当前视频边界: task:%d", observed.TaskID)
		}
		if evidenceID := strings.TrimSpace(observed.EvidenceID); evidenceID != "" {
			key := "evidence:" + evidenceID
			if _, exists := byEvidenceID[key]; exists {
				return nil, fmt.Errorf("已观察证据标识不唯一: %s", key)
			}
			byEvidenceID[key] = observed
		}
		chunkKey := fmt.Sprintf("chunk:%d:%d", observed.TaskID, observed.ChunkID)
		if _, exists := byChunk[chunkKey]; !exists {
			byChunk[chunkKey] = observed
		}
	}

	canonical := make([]RetrievedChunk, 0, len(requested))
	seen := make(map[string]struct{}, len(requested))
	for _, citation := range requested {
		var observed RetrievedChunk
		var ok bool
		if evidenceID := strings.TrimSpace(citation.EvidenceID); evidenceID != "" {
			observed, ok = byEvidenceID["evidence:"+evidenceID]
		} else {
			observed, ok = byChunk[fmt.Sprintf("chunk:%d:%d", citation.TaskID, citation.ChunkID)]
		}
		if !ok {
			return nil, fmt.Errorf("build_cited_answer 引用了未观察到的证据: %s", videoResearchEvidenceKey(citation))
		}
		key := videoResearchEvidenceKey(observed)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		canonical = append(canonical, observed)
	}
	return canonical, nil
}

func validateObservedResearchEvidence(taskID int64, evidence []RetrievedChunk) error {
	if taskID <= 0 {
		return nil
	}
	for _, observed := range evidence {
		if observed.TaskID != taskID {
			return fmt.Errorf("research observation 包含跨视频证据: task:%d", observed.TaskID)
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
