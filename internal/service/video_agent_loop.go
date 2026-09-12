package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"vid-lens/internal/model"
)

type VideoAgentLoopStatus string

const (
	VideoAgentLoopStatusRunning   VideoAgentLoopStatus = "running"
	VideoAgentLoopStatusCompleted VideoAgentLoopStatus = "completed"
	VideoAgentLoopStatusStopped   VideoAgentLoopStatus = "stopped"
	VideoAgentLoopStatusFailed    VideoAgentLoopStatus = "failed"
)

type VideoAgentLoopStepStatus string

const (
	VideoAgentLoopStepRunning   VideoAgentLoopStepStatus = "running"
	VideoAgentLoopStepCompleted VideoAgentLoopStepStatus = "completed"
	VideoAgentLoopStepFailed    VideoAgentLoopStepStatus = "failed"
)

type VideoAgentLoopPolicy struct {
	MaxSteps   int `json:"max_steps"`
	MaxReplans int `json:"max_replans"`
}

func DefaultVideoAgentLoopPolicy() VideoAgentLoopPolicy {
	return VideoAgentLoopPolicy{MaxSteps: 8, MaxReplans: 2}
}

func (p VideoAgentLoopPolicy) Validate() error {
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

type VideoAgentLoopDecision struct {
	Done          bool            `json:"done"`
	Tool          string          `json:"tool,omitempty"`
	Reason        string          `json:"reason,omitempty"`
	PublicSummary string          `json:"public_summary,omitempty"`
	Arguments     json.RawMessage `json:"arguments,omitempty"`
	Replan        bool            `json:"replan,omitempty"`
	StopReason    string          `json:"stop_reason,omitempty"`
}

type VideoAgentLoopObservation struct {
	Tool                string           `json:"tool"`
	Output              json.RawMessage  `json:"output,omitempty"`
	Step                VideoAgentStep   `json:"step"`
	NewEvidence         []RetrievedChunk `json:"new_evidence,omitempty"`
	UnresolvedQuestions []string         `json:"unresolved_questions,omitempty"`
	Answer              string           `json:"answer,omitempty"`
	Citations           []Citation       `json:"citations,omitempty"`
}

type VideoAgentLoopStep struct {
	Number      int                        `json:"number"`
	Action      VideoAgentLoopDecision     `json:"action"`
	Status      VideoAgentLoopStepStatus   `json:"status"`
	Trace       VideoAgentStep             `json:"trace"`
	Observation *VideoAgentLoopObservation `json:"observation,omitempty"`
	Error       string                     `json:"error,omitempty"`
}

type VideoAgentLoopState struct {
	ScopeTaskIDs     []int64                     `json:"scope_task_ids,omitempty"`
	Goal             string                      `json:"goal"`
	Status           VideoAgentLoopStatus        `json:"status"`
	CurrentStep      int                         `json:"current_step"`
	ReplanCount      int                         `json:"replan_count"`
	MaxSteps         int                         `json:"max_steps"`
	MaxReplans       int                         `json:"max_replans"`
	StopReason       string                      `json:"stop_reason,omitempty"`
	PendingQuestions []string                    `json:"pending_questions,omitempty"`
	Evidence         []RetrievedChunk            `json:"evidence,omitempty"`
	Observations     []VideoAgentLoopObservation `json:"observations,omitempty"`
	Steps            []VideoAgentLoopStep        `json:"steps,omitempty"`
	Answer           string                      `json:"answer,omitempty"`
	Citations        []Citation                  `json:"citations,omitempty"`
	Memory           *MemorySnapshot             `json:"memory,omitempty"`
}

type VideoAgentLoopResult struct {
	State VideoAgentLoopState `json:"state"`
}

type VideoAgentLoopPlanner interface {
	NextDecision(ctx context.Context, state VideoAgentLoopState, tools []VideoAgentToolDefinition) (VideoAgentLoopDecision, error)
}

type VideoAgentLoopObserver interface {
	Observe(state VideoAgentLoopState, result VideoAgentToolResult) (VideoAgentLoopObservation, error)
}

type VideoAgentLoopRunner struct {
	registry  *VideoAgentToolRegistry
	planner   VideoAgentLoopPlanner
	observer  VideoAgentLoopObserver
	policy    VideoAgentLoopPolicy
	execution *durableResearchExecution
}

func NewVideoAgentLoopRunner(registry *VideoAgentToolRegistry, planner VideoAgentLoopPlanner, observer VideoAgentLoopObserver, policy VideoAgentLoopPolicy) (*VideoAgentLoopRunner, error) {
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
	return &VideoAgentLoopRunner{registry: registry, planner: planner, observer: observer, policy: policy}, nil
}

func NewVideoAgentLoopState(goal string, policy VideoAgentLoopPolicy) (VideoAgentLoopState, error) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return VideoAgentLoopState{}, errors.New("video research goal 不能为空")
	}
	if err := policy.Validate(); err != nil {
		return VideoAgentLoopState{}, err
	}
	return VideoAgentLoopState{
		Goal:         goal,
		Status:       VideoAgentLoopStatusRunning,
		MaxSteps:     policy.MaxSteps,
		MaxReplans:   policy.MaxReplans,
		Evidence:     make([]RetrievedChunk, 0),
		Observations: make([]VideoAgentLoopObservation, 0),
		Steps:        make([]VideoAgentLoopStep, 0, policy.MaxSteps),
	}, nil
}

func (r *VideoAgentLoopRunner) Run(ctx context.Context, goal string, runtime VideoAgentToolRuntime) (*VideoAgentLoopResult, error) {
	if r == nil {
		return nil, errors.New("video research runner 不能为空")
	}
	state, err := NewVideoAgentLoopState(goal, r.policy)
	if err != nil {
		return nil, err
	}
	state.Memory = runtime.MemorySnapshot
	state.ScopeTaskIDs = append([]int64(nil), runtime.TaskIDs...)
	if err := runtime.checkScope(ctx, nil); err != nil {
		return nil, err
	}
	result := &VideoAgentLoopResult{State: state}
	if r.execution != nil {
		recoveredTerminal, recoverErr := r.recoverResearchState(ctx, &result.State, runtime)
		if recoverErr != nil {
			return r.fail(result, "recovery_failure", recoverErr)
		}
		if recoveredTerminal {
			return result, nil
		}
	}

	for {
		if err := runtime.checkScope(ctx, result.State.Evidence); err != nil {
			return r.fail(result, "scope_changed", err)
		}
		if err := ctx.Err(); err != nil {
			return r.fail(result, "request_cancelled", err)
		}
		if result.State.Answer != "" {
			result.State.Status = VideoAgentLoopStatusCompleted
			result.State.StopReason = "answer_generated"
			return result, nil
		}
		if result.State.CurrentStep >= result.State.MaxSteps {
			result.State.Status = VideoAgentLoopStatusStopped
			result.State.StopReason = "budget_exhausted"
			return result, nil
		}

		decision, budgetExhausted, err := r.nextResearchDecision(ctx, result.State, runtime)
		if err != nil {
			var invalidDecision *invalidResearchDecisionError
			if errors.As(err, &invalidDecision) {
				return r.fail(result, "invalid_planner_decision", err)
			}
			return r.fail(result, "planner_failure", err)
		}
		if budgetExhausted {
			result.State.Status = VideoAgentLoopStatusStopped
			result.State.StopReason = "budget_exhausted"
			return result, nil
		}
		if decision.Done {
			result.State.Status = VideoAgentLoopStatusCompleted
			result.State.StopReason = firstNonEmpty(decision.StopReason, "goal_satisfied")
			return result, nil
		}
		if decision.Replan {
			result.State.ReplanCount++
			if result.State.ReplanCount > result.State.MaxReplans {
				result.State.ReplanCount--
				result.State.Status = VideoAgentLoopStatusStopped
				result.State.StopReason = "replan_limit_reached"
				return result, nil
			}
		}

		step := VideoAgentLoopStep{
			Number: result.State.CurrentStep + 1,
			Action: decision,
			Status: VideoAgentLoopStepRunning,
		}
		toolResult, observation, budgetExhausted, err := r.executeResearchTool(ctx, result.State, runtime, decision)
		if budgetExhausted {
			result.State.Status = VideoAgentLoopStatusStopped
			result.State.StopReason = "budget_exhausted"
			return result, nil
		}
		result.State.CurrentStep++
		step.Trace = toolResult.Step
		if err != nil {
			step.Status = VideoAgentLoopStepFailed
			step.Error = err.Error()
			result.State.Steps = append(result.State.Steps, step)
			return r.fail(result, "tool_failure", err)
		}

		if err := runtime.checkScope(ctx, observation.NewEvidence); err != nil {
			return r.fail(result, "scope_changed", err)
		}
		step.Status = VideoAgentLoopStepCompleted
		step.Observation = &observation
		result.State.Steps = append(result.State.Steps, step)
		result.State.Observations = append(result.State.Observations, observation)
		result.State.Evidence = mergeVideoAgentLoopEvidence(result.State.Evidence, observation.NewEvidence)
		result.State.PendingQuestions = append([]string(nil), observation.UnresolvedQuestions...)
		if observation.Answer != "" {
			result.State.Answer = observation.Answer
			result.State.Citations = append([]Citation(nil), observation.Citations...)
		}
	}
}

func (r *VideoAgentLoopRunner) validateDecision(state VideoAgentLoopState, decision VideoAgentLoopDecision) error {
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

func (r *VideoAgentLoopRunner) fail(result *VideoAgentLoopResult, reason string, err error) (*VideoAgentLoopResult, error) {
	result.State.Status = VideoAgentLoopStatusFailed
	result.State.StopReason = reason
	return result, err
}

type DefaultVideoAgentLoopObserver struct{}

func (DefaultVideoAgentLoopObserver) Observe(state VideoAgentLoopState, result VideoAgentToolResult) (VideoAgentLoopObservation, error) {
	observation := VideoAgentLoopObservation{
		Tool:   result.Step.Tool,
		Output: append(json.RawMessage(nil), result.Output...),
		Step:   result.Step,
	}
	if result.Step.Tool == VideoAgentToolSearchTranscript || result.Step.Tool == VideoAgentToolSearchVisualEvidence {
		var search SearchTranscriptResult
		if err := json.Unmarshal(result.Output, &search); err != nil {
			return VideoAgentLoopObservation{}, fmt.Errorf("解析 %s observation 失败: %w", result.Step.Tool, err)
		}
		observation.NewEvidence = append([]RetrievedChunk(nil), search.Citations...)
	}
	if result.Step.Tool == VideoAgentToolInspectVisualWindow {
		var inspected InspectVisualWindowResult
		if err := json.Unmarshal(result.Output, &inspected); err != nil {
			return VideoAgentLoopObservation{}, fmt.Errorf("解析 inspect_visual_window observation 失败: %w", err)
		}
		observation.NewEvidence = append([]RetrievedChunk(nil), inspected.Evidence...)
	}
	if result.Step.Tool == VideoAgentToolInvestigateVisual {
		var investigated InvestigateVisualResult
		if err := json.Unmarshal(result.Output, &investigated); err != nil {
			return VideoAgentLoopObservation{}, fmt.Errorf("解析 investigate_visual observation 失败: %w", err)
		}
		observation.NewEvidence = visualInvestigationEvidence(investigated)
	}
	if result.Step.Tool == VideoAgentToolBuildCitedAnswer {
		var answer BuildCitedAnswerResult
		if err := json.Unmarshal(result.Output, &answer); err != nil {
			return VideoAgentLoopObservation{}, fmt.Errorf("解析 build_cited_answer observation 失败: %w", err)
		}
		canonical, err := canonicalizeResearchCitations(state.Evidence, 0, answer.Citations)
		if err != nil {
			return VideoAgentLoopObservation{}, fmt.Errorf("canonicalize build_cited_answer observation 失败: %w", err)
		}
		answer.Citations = canonical
		canonicalOutput, err := json.Marshal(answer)
		if err != nil {
			return VideoAgentLoopObservation{}, fmt.Errorf("序列化 canonical build_cited_answer observation 失败: %w", err)
		}
		observation.Output = canonicalOutput
		finalized := finalizeAnswerCitations(answer.Answer, buildCitations(state.Goal, canonical))
		observation.Answer = finalized.Answer
		observation.Citations = finalized.Citations
	}
	return observation, nil
}

func visualInvestigationEvidence(investigation InvestigateVisualResult) []RetrievedChunk {
	evidence := make([]RetrievedChunk, 0, len(investigation.Observations))
	for index, observed := range investigation.Observations {
		if observed.Status != model.VisualObservationStatusObserved || observed.ObjectKey == "" || strings.TrimSpace(observed.Observation) == "" {
			continue
		}
		content := observed.Observation
		if len(observed.StructuredFacts) > 0 {
			content = strings.Join(observed.StructuredFacts, "；")
		}
		evidence = append(evidence, RetrievedChunk{
			TaskID: observed.TaskID, EvidenceID: "visual-observation:" + observed.ID, ChunkIndex: index,
			Content: content, AnchorContent: content, Source: "visual_investigation",
			Modality: "image", StartMS: observed.StartMS, EndMS: observed.EndMS,
			TimeRangeStatus: model.ChunkTimeRangeExact, SourceMappingStatus: model.ChunkSourceMapped,
			SourceRefs: []ChunkSourceRef{{SourceType: "image", StableID: "visual-observation:" + observed.ID,
				StartMS: observed.StartMS, EndMS: observed.EndMS, TimeRangeStatus: model.ChunkTimeRangeExact,
				ObjectKey: observed.ObjectKey, ArtifactKind: firstNonEmpty(observed.ArtifactKind, model.VisualArtifactKindFrame), CaptionMethod: "query_vlm"}},
		})
	}
	return evidence
}

func mergeVideoAgentLoopEvidence(existing, added []RetrievedChunk) []RetrievedChunk {
	merged := append([]RetrievedChunk(nil), existing...)
	seen := make(map[string]struct{}, len(merged)+len(added))
	for _, chunk := range merged {
		seen[videoAgentLoopEvidenceKey(chunk)] = struct{}{}
	}
	for _, chunk := range added {
		key := videoAgentLoopEvidenceKey(chunk)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, chunk)
	}
	return merged
}

func videoAgentLoopEvidenceKey(chunk RetrievedChunk) string {
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
		return []RetrievedChunk{}, nil
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
			return nil, fmt.Errorf("build_cited_answer 引用了未观察到的证据: %s", videoAgentLoopEvidenceKey(citation))
		}
		key := videoAgentLoopEvidenceKey(observed)
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
