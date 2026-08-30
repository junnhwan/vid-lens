package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

var errAgentExecutionBusy = errors.New("agent execution step is owned by another worker")

const videoResearchPlannerCall = "video_research_planner"

type invalidResearchDecisionError struct{ cause error }

func (e *invalidResearchDecisionError) Error() string { return e.cause.Error() }
func (e *invalidResearchDecisionError) Unwrap() error { return e.cause }

type durableResearchExecution struct {
	repo   *repository.AgentExecutionRepository
	userID int64
	runID  string
	now    func() time.Time
}

type durableResearchDecision struct {
	Done       bool            `json:"done"`
	Tool       string          `json:"tool,omitempty"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
	Replan     bool            `json:"replan,omitempty"`
	StopReason string          `json:"stop_reason,omitempty"`
}

type durableResearchToolCheckpoint struct {
	Result      VideoAgentToolResult     `json:"result"`
	Observation VideoResearchObservation `json:"observation"`
}

func (r *VideoResearchRunner) SetDurableExecution(repo *repository.AgentExecutionRepository, userID int64, runID string) error {
	if r == nil || repo == nil || userID <= 0 || strings.TrimSpace(runID) == "" {
		return errors.New("durable video research execution parameters are invalid")
	}
	r.execution = &durableResearchExecution{repo: repo, userID: userID, runID: runID, now: func() time.Time { return time.Now().UTC() }}
	return nil
}

func (r *VideoResearchRunner) recoverResearchState(ctx context.Context, state *VideoResearchState, runtime VideoAgentToolRuntime) (bool, error) {
	if r == nil || r.execution == nil || state == nil {
		return false, nil
	}
	records, err := r.execution.repo.GetExecution(ctx, r.execution.userID, r.execution.runID)
	if err != nil {
		return false, err
	}
	if records == nil {
		return false, errors.New("agent research execution not found")
	}
	if records.Run.TaskID != runtime.TaskID || records.Run.Goal != state.Goal {
		return false, errors.New("persisted research execution scope does not match runtime")
	}

	for number := 1; number <= state.MaxSteps+1; number++ {
		planID := fmt.Sprintf("plan-%d", number)
		planStep, planCall, err := completedResearchRecord(records, planID)
		if err != nil {
			return false, err
		}
		if planStep == nil {
			if hasCompletedResearchSequenceAfter(records.Steps, number*2-2) {
				return false, fmt.Errorf("persisted research execution has a gap before %s", planID)
			}
			return false, nil
		}
		if planStep.Sequence != number*2-1 || planStep.Kind != "plan" || planStep.Action != "select_next_action" || planCall == nil || planCall.CallKind != model.AgentCallKindPlannerLLM || planCall.ToolName != videoResearchPlannerCall {
			return false, fmt.Errorf("persisted planner record %s is invalid", planID)
		}
		_, expectedInputDigest := safePlannerInputSummary(*state, r.registry.Definitions())
		if planCall.ArgumentsDigest != expectedInputDigest || planStep.ResultCheckpoint != planCall.ResultCheckpoint {
			return false, fmt.Errorf("persisted planner record %s does not match recovered state", planID)
		}
		var storedDecision durableResearchDecision
		if err := json.Unmarshal([]byte(planStep.ResultCheckpoint), &storedDecision); err != nil {
			return false, fmt.Errorf("decode persisted planner decision %s: %w", planID, err)
		}
		decision, err := r.validatedResearchDecision(*state, runtime.TaskID, storedDecision.toDecision())
		if err != nil {
			return false, fmt.Errorf("validate persisted planner decision %s: %w", planID, err)
		}
		if decision.Done {
			if toolStep, _, toolErr := completedResearchRecord(records, fmt.Sprintf("tool-%d", number)); toolErr != nil {
				return false, toolErr
			} else if toolStep != nil {
				return false, fmt.Errorf("completed planner decision %s has an unexpected tool result", planID)
			}
			state.Status = VideoResearchStatusCompleted
			state.StopReason = firstNonEmpty(decision.StopReason, "goal_satisfied")
			return true, nil
		}

		toolID := fmt.Sprintf("tool-%d", number)
		toolStep, toolCall, err := completedResearchRecord(records, toolID)
		if err != nil {
			return false, err
		}
		if toolStep == nil {
			if hasCompletedResearchSequenceAfter(records.Steps, number*2-1) {
				return false, fmt.Errorf("persisted research execution has a gap before %s", toolID)
			}
			return false, nil
		}
		expectedArgumentsDigest := digestAgentValue(string(decision.Arguments))
		if toolStep.Sequence != number*2 || toolStep.Action != decision.Tool || toolCall == nil || toolCall.CallKind != model.AgentCallKindTool || toolCall.ToolName != decision.Tool || toolCall.ArgumentsDigest != expectedArgumentsDigest || toolStep.ResultCheckpoint != toolCall.ResultCheckpoint {
			return false, fmt.Errorf("persisted tool record %s does not match planner decision", toolID)
		}
		var checkpoint durableResearchToolCheckpoint
		if err := json.Unmarshal([]byte(toolStep.ResultCheckpoint), &checkpoint); err != nil {
			return false, fmt.Errorf("decode persisted tool checkpoint %s: %w", toolID, err)
		}
		if checkpoint.Result.Step.Tool != decision.Tool || checkpoint.Observation.Tool != decision.Tool || checkpoint.Observation.Step.Tool != decision.Tool {
			return false, fmt.Errorf("persisted tool checkpoint %s has inconsistent action provenance", toolID)
		}
		if err := validateObservedResearchEvidence(runtime.TaskID, checkpoint.Observation.NewEvidence); err != nil {
			return false, err
		}
		applyRecoveredResearchStep(state, number, decision, checkpoint)
		if decision.Replan {
			state.ReplanCount++
			if state.ReplanCount > state.MaxReplans {
				return false, errors.New("persisted research execution exceeds replan limit")
			}
		}
	}
	return false, errors.New("persisted research execution exceeds step limit")
}

func completedResearchRecord(records *repository.AgentExecutionRecords, stepID string) (*model.AgentStep, *model.AgentToolCall, error) {
	var completed *model.AgentStep
	for index := range records.Steps {
		step := &records.Steps[index]
		if step.StepID != stepID || step.Status != model.AgentStepStatusCompleted {
			continue
		}
		if completed != nil {
			return nil, nil, fmt.Errorf("persisted research step %s has multiple completed attempts", stepID)
		}
		completed = step
	}
	if completed == nil {
		return nil, nil, nil
	}
	var completedCall *model.AgentToolCall
	for index := range records.ToolCalls {
		call := &records.ToolCalls[index]
		if call.AgentStepID != completed.ID {
			continue
		}
		if call.Status != model.AgentToolCallStatusCompleted {
			return nil, nil, fmt.Errorf("persisted research step %s has a non-completed tool call", stepID)
		}
		if completedCall != nil {
			return nil, nil, fmt.Errorf("persisted research step %s has multiple tool calls", stepID)
		}
		completedCall = call
	}
	if completedCall == nil {
		return nil, nil, fmt.Errorf("persisted research step %s is missing its tool call", stepID)
	}
	return completed, completedCall, nil
}

func hasCompletedResearchSequenceAfter(steps []model.AgentStep, sequence int) bool {
	for _, step := range steps {
		if step.Status == model.AgentStepStatusCompleted && step.Sequence > sequence {
			return true
		}
	}
	return false
}

func applyRecoveredResearchStep(state *VideoResearchState, number int, decision VideoResearchDecision, checkpoint durableResearchToolCheckpoint) {
	observation := checkpoint.Observation
	state.CurrentStep++
	state.Steps = append(state.Steps, VideoResearchStep{
		Number: number, Action: decision, Status: VideoResearchStepCompleted,
		Trace: checkpoint.Result.Step, Observation: &observation,
	})
	state.Observations = append(state.Observations, observation)
	state.Evidence = mergeVideoResearchEvidence(state.Evidence, observation.NewEvidence)
	state.PendingQuestions = append([]string(nil), observation.UnresolvedQuestions...)
	if observation.Answer != "" {
		state.Answer = observation.Answer
		state.Citations = append([]Citation(nil), observation.Citations...)
	}
}

func (r *VideoResearchRunner) nextResearchDecision(ctx context.Context, state VideoResearchState, runtime VideoAgentToolRuntime) (VideoResearchDecision, bool, error) {
	if r.execution == nil {
		decision, err := r.planner.NextDecision(ctx, state, r.registry.Definitions())
		if err != nil {
			return VideoResearchDecision{}, false, err
		}
		decision, err = r.validatedResearchDecision(state, runtime.TaskID, decision)
		if err != nil {
			err = &invalidResearchDecisionError{cause: err}
		}
		return decision, false, err
	}
	execution := r.execution
	sequence := state.CurrentStep*2 + 1
	stepID := fmt.Sprintf("plan-%d", state.CurrentStep+1)
	now := execution.now()
	definitions := r.registry.Definitions()
	inputSummary, inputDigest := safePlannerInputSummary(state, definitions)
	plannerContextChars := plannerContextChars(state, definitions)
	callDigest := digestAgentValue(execution.runID + ":" + stepID + ":1:" + videoResearchPlannerCall + ":" + inputDigest)
	claim, err := execution.repo.ClaimStep(ctx, repository.AgentStepClaimRequest{
		UserID: execution.userID, RunID: execution.runID, StepID: stepID, Attempt: 1, Sequence: sequence,
		Kind: "plan", Action: "select_next_action", SafeReason: "select the next allow-listed action",
		InputSummary: inputSummary, ArgumentsDigest: inputDigest, CallDigest: callDigest,
		ToolName: videoResearchPlannerCall, CallKind: model.AgentCallKindPlannerLLM, InternalCall: true,
		ReplaySafe: false, LLMCall: true, ContextChars: plannerContextChars, EstimatedPromptTokens: plannerContextChars / 4,
		LeaseToken: uuid.NewString(), Now: now, LeaseUntil: now.Add(agentStepLeaseDuration),
	})
	if err != nil {
		return VideoResearchDecision{}, false, err
	}
	switch claim.Outcome {
	case repository.AgentStepClaimCompleted:
		if claim.ToolCall == nil || claim.ToolCall.CallKind != model.AgentCallKindPlannerLLM || claim.ToolCall.ToolName != videoResearchPlannerCall || claim.ToolCall.ArgumentsDigest != inputDigest || claim.ToolCall.CallDigest != callDigest {
			return VideoResearchDecision{}, false, errors.New("persisted planner call does not match the current safe input")
		}
		var stored durableResearchDecision
		if err := json.Unmarshal([]byte(claim.Step.ResultCheckpoint), &stored); err != nil {
			return VideoResearchDecision{}, false, fmt.Errorf("decode persisted planner decision: %w", err)
		}
		decision, err := r.validatedResearchDecision(state, runtime.TaskID, stored.toDecision())
		if err != nil {
			return VideoResearchDecision{}, false, fmt.Errorf("validate persisted planner decision: %w", err)
		}
		return decision, false, nil
	case repository.AgentStepClaimExhausted:
		return VideoResearchDecision{}, true, nil
	case repository.AgentStepClaimBusy:
		return VideoResearchDecision{}, false, errAgentExecutionBusy
	case repository.AgentStepClaimAmbiguous, repository.AgentStepClaimTerminal:
		return VideoResearchDecision{}, false, fmt.Errorf("planner step %s cannot be replayed safely: %s", stepID, claim.Outcome)
	case repository.AgentStepClaimAcquired:
	default:
		return VideoResearchDecision{}, false, fmt.Errorf("planner step %s claim failed", stepID)
	}

	decision, usage, planErr := callVideoResearchPlanner(ctx, r.planner, state, definitions)
	if planErr == nil {
		decision, planErr = r.validatedResearchDecision(state, runtime.TaskID, decision)
		if planErr != nil {
			planErr = &invalidResearchDecisionError{cause: planErr}
		}
	}
	if planErr != nil {
		cancelled := errors.Is(planErr, context.Canceled) || errors.Is(planErr, context.DeadlineExceeded)
		_, _ = execution.repo.FailStep(context.WithoutCancel(ctx), repository.AgentStepFailure{
			UserID: execution.userID, RunID: execution.runID, StepID: stepID, Attempt: 1, LeaseToken: claim.Step.LeaseToken,
			ErrorCode: "planner_failure", ErrorMessage: safeAgentError(planErr),
			PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens, CostMicros: usage.CostMicros,
			UsageSource: usage.UsageSource, TokenEstimated: usage.TokenEstimated, Currency: usage.Currency, PriceVersion: usage.PriceVersion,
			ContextChars: usage.ContextChars, ContextUsageSource: usageContextSource(usage), MetricsJSON: usageMetrics(usage), Cancelled: cancelled,
			Now: execution.now(),
		})
		return VideoResearchDecision{}, false, planErr
	}
	checkpoint := durableResearchDecisionFrom(decision)
	encoded, err := json.Marshal(checkpoint)
	if err != nil {
		return VideoResearchDecision{}, false, err
	}
	completed, err := execution.repo.CompleteStep(context.WithoutCancel(ctx), repository.AgentStepCompletion{
		UserID: execution.userID, RunID: execution.runID, StepID: stepID, Attempt: 1, LeaseToken: claim.Step.LeaseToken,
		OutputRef: firstNonEmpty(decision.Tool, decision.StopReason, "done"), ResultCheckpoint: string(encoded),
		PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens, CostMicros: usage.CostMicros,
		UsageSource: usage.UsageSource, TokenEstimated: usage.TokenEstimated, Currency: usage.Currency, PriceVersion: usage.PriceVersion,
		ContextChars: usage.ContextChars, ContextUsageSource: usageContextSource(usage), MetricsJSON: usageMetrics(usage),
		Now: execution.now(),
	})
	if err != nil {
		return VideoResearchDecision{}, false, err
	}
	if !completed {
		return VideoResearchDecision{}, false, errors.New("planner completion CAS failed")
	}
	return checkpoint.toDecision(), false, nil
}

func callVideoResearchPlanner(ctx context.Context, planner VideoResearchPlanner, state VideoResearchState, tools []VideoAgentToolDefinition) (VideoResearchDecision, VideoResearchPlannerCallUsage, error) {
	if observed, ok := planner.(VideoResearchPlannerWithUsage); ok {
		return observed.NextDecisionWithUsage(ctx, state, tools)
	}
	decision, err := planner.NextDecision(ctx, state, tools)
	return decision, VideoResearchPlannerCallUsage{UsageSource: model.AgentCallUsageUnknown}, err
}

func safePlannerInputSummary(state VideoResearchState, tools []VideoAgentToolDefinition) (string, string) {
	toolNames := make([]string, 0, len(tools))
	for _, definition := range tools {
		toolNames = append(toolNames, definition.Name)
	}
	stateJSON, _ := json.Marshal(state)
	inputDigest := digestAgentValue("video-research-planner:v1:" + string(stateJSON) + ":" + strings.Join(toolNames, ","))
	summary, _ := json.Marshal(map[string]any{
		"schema": 1, "planner_version": "video-research-planner:v1", "input_digest": "sha256:" + inputDigest,
		"goal_digest": "sha256:" + digestAgentValue(state.Goal), "candidate_tools": toolNames,
		"completed_steps": state.CurrentStep, "evidence_count": len(state.Evidence), "pending_question_count": len(state.PendingQuestions),
	})
	return string(summary), inputDigest
}

func plannerContextChars(state VideoResearchState, tools []VideoAgentToolDefinition) int64 {
	stateJSON, _ := json.Marshal(state)
	toolsJSON, _ := json.Marshal(tools)
	content := fmt.Sprintf("你是 VidLens 的视频研究计划器。%s%s", string(toolsJSON), string(stateJSON))
	return int64(len([]rune(content)))
}

func (r *VideoResearchRunner) executeResearchTool(ctx context.Context, state VideoResearchState, runtime VideoAgentToolRuntime, decision VideoResearchDecision) (VideoAgentToolResult, VideoResearchObservation, bool, error) {
	if r.execution == nil {
		result, err := r.registry.Execute(ctx, decision.Tool, VideoAgentToolRequest{Runtime: runtime, Arguments: decision.Arguments})
		if err != nil {
			return result, VideoResearchObservation{}, false, err
		}
		observation, err := r.observeResearchTool(state, runtime.TaskID, result)
		return result, observation, false, err
	}
	execution := r.execution
	sequence := state.CurrentStep*2 + 2
	stepID := fmt.Sprintf("tool-%d", state.CurrentStep+1)
	now := execution.now()
	inputSummary := safeResearchArgumentsSummary(decision.Tool, decision.Arguments)
	argsDigest := digestAgentValue(string(decision.Arguments))
	contextChars := researchToolContextChars(decision.Tool, decision.Arguments)
	claim, err := execution.repo.ClaimStep(ctx, repository.AgentStepClaimRequest{
		UserID: execution.userID, RunID: execution.runID, StepID: stepID, Attempt: 1, Sequence: sequence,
		Kind: videoAgentStepKind(decision.Tool), Action: decision.Tool, SafeReason: safeToolReason(decision.Tool),
		InputSummary: inputSummary, ArgumentsDigest: argsDigest,
		CallDigest: digestAgentValue(execution.runID + ":" + stepID + ":1:" + decision.Tool + ":" + argsDigest), ToolName: decision.Tool,
		ReplaySafe: replaySafeAgentAction(decision.Tool), LLMCall: llmAgentAction(decision.Tool), VisionCall: visionAgentAction(decision.Tool), RetrievalCall: retrievalAgentAction(decision.Tool), ContextChars: contextChars, EstimatedPromptTokens: contextChars / 4,
		LeaseToken: uuid.NewString(), Now: now, LeaseUntil: now.Add(agentStepLeaseDuration),
	})
	if err != nil {
		return VideoAgentToolResult{}, VideoResearchObservation{}, false, err
	}
	switch claim.Outcome {
	case repository.AgentStepClaimCompleted:
		if claim.Step.Action != decision.Tool || claim.ToolCall == nil || claim.ToolCall.ToolName != decision.Tool || claim.ToolCall.ArgumentsDigest != argsDigest {
			return VideoAgentToolResult{}, VideoResearchObservation{}, false, errors.New("persisted tool checkpoint does not match the validated action")
		}
		var stored durableResearchToolCheckpoint
		if err := json.Unmarshal([]byte(claim.Step.ResultCheckpoint), &stored); err != nil {
			return VideoAgentToolResult{}, VideoResearchObservation{}, false, fmt.Errorf("decode persisted tool checkpoint: %w", err)
		}
		return stored.Result, stored.Observation, false, nil
	case repository.AgentStepClaimExhausted:
		return VideoAgentToolResult{}, VideoResearchObservation{}, true, nil
	case repository.AgentStepClaimBusy:
		return VideoAgentToolResult{}, VideoResearchObservation{}, false, errAgentExecutionBusy
	case repository.AgentStepClaimAmbiguous, repository.AgentStepClaimTerminal:
		return VideoAgentToolResult{}, VideoResearchObservation{}, false, fmt.Errorf("tool step %s cannot be replayed safely: %s", stepID, claim.Outcome)
	case repository.AgentStepClaimAcquired:
	default:
		return VideoAgentToolResult{}, VideoResearchObservation{}, false, fmt.Errorf("tool step %s claim failed", stepID)
	}

	result, toolErr := r.registry.Execute(ctx, decision.Tool, VideoAgentToolRequest{Runtime: runtime, Arguments: decision.Arguments})
	var observation VideoResearchObservation
	if toolErr == nil {
		observation, toolErr = r.observeResearchTool(state, runtime.TaskID, result)
	}
	if toolErr != nil {
		cancelled := errors.Is(toolErr, context.Canceled) || errors.Is(toolErr, context.DeadlineExceeded)
		_, _ = execution.repo.FailStep(context.WithoutCancel(ctx), repository.AgentStepFailure{
			UserID: execution.userID, RunID: execution.runID, StepID: stepID, Attempt: 1, LeaseToken: claim.Step.LeaseToken,
			ErrorCode: "tool_failure", ErrorMessage: safeAgentError(toolErr), ContextChars: contextChars, ContextUsageSource: usageSourceForContext(contextChars),
			MetricsJSON: usageMetrics(VideoResearchPlannerCallUsage{ContextChars: contextChars, UsageSource: model.AgentCallUsageUnknown}), Cancelled: cancelled, Now: execution.now(),
		})
		return result, VideoResearchObservation{}, false, toolErr
	}
	checkpoint, err := json.Marshal(durableResearchToolCheckpoint{Result: result, Observation: observation})
	if err != nil {
		return result, observation, false, err
	}
	completed, err := execution.repo.CompleteStep(context.WithoutCancel(ctx), repository.AgentStepCompletion{
		UserID: execution.userID, RunID: execution.runID, StepID: stepID, Attempt: 1, LeaseToken: claim.Step.LeaseToken,
		OutputRef: agentToolOutputRef(result.Step), ResultCheckpoint: string(checkpoint), EvidenceRefs: researchObservationEvidenceRefs(observation), Now: execution.now(),
		ContextChars: contextChars, ContextUsageSource: usageSourceForContext(contextChars), MetricsJSON: usageMetrics(VideoResearchPlannerCallUsage{ContextChars: contextChars, UsageSource: model.AgentCallUsageUnknown}),
	})
	if err != nil {
		return result, observation, false, err
	}
	if !completed {
		return result, observation, false, errors.New("tool completion CAS failed")
	}
	return result, observation, false, nil
}

func (r *VideoResearchRunner) validatedResearchDecision(state VideoResearchState, taskID int64, decision VideoResearchDecision) (VideoResearchDecision, error) {
	if err := r.validateDecision(state, decision); err != nil {
		return VideoResearchDecision{}, err
	}
	if decision.Tool == VideoAgentToolBuildCitedAnswer {
		canonical, err := canonicalizeResearchAnswerArguments(state.Evidence, taskID, decision.Arguments)
		if err != nil {
			return VideoResearchDecision{}, err
		}
		decision.Arguments = canonical
	}
	return decision, nil
}

func (r *VideoResearchRunner) observeResearchTool(state VideoResearchState, taskID int64, result VideoAgentToolResult) (VideoResearchObservation, error) {
	observation, err := r.observer.Observe(state, result)
	if err != nil {
		return VideoResearchObservation{}, err
	}
	if err := validateObservedResearchEvidence(taskID, observation.NewEvidence); err != nil {
		return VideoResearchObservation{}, err
	}
	return observation, nil
}

func durableResearchDecisionFrom(decision VideoResearchDecision) durableResearchDecision {
	return durableResearchDecision{Done: decision.Done, Tool: decision.Tool, Arguments: append(json.RawMessage(nil), decision.Arguments...), Replan: decision.Replan, StopReason: decision.StopReason}
}

func (d durableResearchDecision) toDecision() VideoResearchDecision {
	reason := "select the persisted allow-listed action"
	if d.Done {
		reason = ""
	}
	return VideoResearchDecision{Done: d.Done, Tool: d.Tool, Reason: reason, Arguments: append(json.RawMessage(nil), d.Arguments...), Replan: d.Replan, StopReason: d.StopReason}
}

func safeResearchArgumentsSummary(tool string, arguments json.RawMessage) string {
	summary := map[string]any{"schema": 1, "arguments_digest": "sha256:" + digestAgentValue(string(arguments))}
	switch tool {
	case VideoAgentToolSearchTranscript:
		var input searchTranscriptToolArguments
		if json.Unmarshal(arguments, &input) == nil {
			summary["question_digest"], summary["top_k"] = "sha256:"+digestAgentValue(input.Question), input.TopK
		}
	case VideoAgentToolGetTranscriptWindow:
		var input transcriptWindowToolArguments
		if json.Unmarshal(arguments, &input) == nil {
			summary["chunk_index"], summary["radius"] = input.ChunkIndex, input.Radius
		}
	case VideoAgentToolSummarizeSegments:
		var input summarizeSegmentsToolArguments
		if json.Unmarshal(arguments, &input) == nil {
			summary["question_digest"], summary["segment_count"] = "sha256:"+digestAgentValue(input.Question), len(input.Segments)
		}
	case VideoAgentToolCompareSegments:
		var input compareSegmentsToolArguments
		if json.Unmarshal(arguments, &input) == nil {
			summary["question_digest"], summary["group_count"] = "sha256:"+digestAgentValue(input.Question), len(input.Groups)
		}
	case VideoAgentToolBuildCitedAnswer:
		var input buildCitedAnswerToolArguments
		if json.Unmarshal(arguments, &input) == nil {
			summary["question_digest"], summary["intermediate_digest"], summary["citation_count"] = "sha256:"+digestAgentValue(input.Question), "sha256:"+digestAgentValue(input.Intermediate), len(input.Citations)
		}
	}
	encoded, _ := json.Marshal(summary)
	return string(encoded)
}

func researchObservationEvidenceRefs(observation VideoResearchObservation) string {
	refs := make([]string, 0, len(observation.NewEvidence))
	for _, item := range observation.NewEvidence {
		if item.EvidenceID != "" {
			refs = append(refs, item.EvidenceID)
		}
	}
	encoded, _ := json.Marshal(refs)
	return string(encoded)
}

func researchToolContextChars(tool string, arguments json.RawMessage) int64 {
	if !llmAgentAction(tool) {
		return 0
	}
	return int64(len([]rune(string(arguments))))
}

func usageSourceForContext(contextChars int64) string {
	if contextChars <= 0 {
		return model.AgentCallUsageUnknown
	}
	return model.AgentCallUsageEstimated
}

func firstPositive(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func usageContextSource(usage VideoResearchPlannerCallUsage) string {
	return usageSourceForContext(usage.ContextChars)
}

func usageMetrics(usage VideoResearchPlannerCallUsage) string {
	metrics := map[string]any{"cost_usage_source": model.AgentCallUsageUnknown}
	if usage.ContextChars > 0 {
		metrics["context_chars"] = usage.ContextChars
		metrics["context_usage_source"] = usageContextSource(usage)
	}
	if usage.PromptTokens > 0 || usage.CompletionTokens > 0 {
		metrics["token_usage_source"] = usage.UsageSource
	}
	if usage.CostMicros > 0 {
		metrics["cost_usage_source"] = usage.UsageSource
	}
	encoded, _ := json.Marshal(metrics)
	return string(encoded)
}

func mergeAgentUsageMetrics(raw string, usage VideoResearchPlannerCallUsage, contextChars int64) string {
	metrics := map[string]any{}
	if strings.TrimSpace(raw) == "" || json.Unmarshal([]byte(raw), &metrics) != nil {
		metrics = map[string]any{}
	}
	if contextChars > 0 {
		metrics["context_chars"] = contextChars
		metrics["context_usage_source"] = usageSourceForContext(contextChars)
	}
	if usage.PromptTokens > 0 || usage.CompletionTokens > 0 {
		metrics["token_usage_source"] = usage.UsageSource
	}
	if usage.CostMicros > 0 {
		metrics["cost_usage_source"] = usage.UsageSource
	} else {
		metrics["cost_usage_source"] = model.AgentCallUsageUnknown
	}
	encoded, _ := json.Marshal(metrics)
	return string(encoded)
}
