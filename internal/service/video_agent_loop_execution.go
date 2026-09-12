package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/observability"
	"vid-lens/internal/repository"
)

const videoAgentLoopPlannerCall = "video_research_planner"

type invalidResearchDecisionError struct{ cause error }

func (e *invalidResearchDecisionError) Error() string { return e.cause.Error() }
func (e *invalidResearchDecisionError) Unwrap() error { return e.cause }

type durableResearchExecution struct {
	journal *AgentExecutionJournal
	userID  int64
	runID   string
}

type durableResearchDecision struct {
	BudgetNotice  *AgentBudgetNotice `json:"budget_notice,omitempty"`
	PublicSummary string             `json:"public_summary,omitempty"`
	Done          bool               `json:"done"`
	Tool          string             `json:"tool,omitempty"`
	Arguments     json.RawMessage    `json:"arguments,omitempty"`
	Replan        bool               `json:"replan,omitempty"`
	StopReason    string             `json:"stop_reason,omitempty"`
}

type durableResearchToolCheckpoint struct {
	Result      VideoAgentToolResult      `json:"result"`
	Observation VideoAgentLoopObservation `json:"observation"`
}

func (r *VideoAgentLoopRunner) SetDurableExecution(journal *AgentExecutionJournal, userID int64, runID string) error {
	if r == nil || journal == nil || userID <= 0 || strings.TrimSpace(runID) == "" {
		return errors.New("durable video research execution parameters are invalid")
	}
	r.execution = &durableResearchExecution{journal: journal, userID: userID, runID: runID}
	return nil
}

func (r *VideoAgentLoopRunner) recoverResearchState(ctx context.Context, state *VideoAgentLoopState, runtime VideoAgentToolRuntime) (bool, error) {
	if r == nil || r.execution == nil || state == nil {
		return false, nil
	}
	records, err := r.execution.journal.Recover(ctx, r.execution.userID, r.execution.runID)
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
		if planStep.Sequence != number*2-1 || planStep.Kind != "plan" || planStep.Action != "select_next_action" || planCall == nil || planCall.CallKind != model.AgentCallKindPlannerLLM || planCall.ToolName != videoAgentLoopPlannerCall {
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
		state.BudgetNotice = storedDecision.BudgetNotice
		if decision.Done {
			if toolStep, _, toolErr := completedResearchRecord(records, fmt.Sprintf("tool-%d", number)); toolErr != nil {
				return false, toolErr
			} else if toolStep != nil {
				return false, fmt.Errorf("completed planner decision %s has an unexpected tool result", planID)
			}
			state.Status = VideoAgentLoopStatusCompleted
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
		if err := runtime.checkScope(ctx, checkpoint.Observation.NewEvidence); err != nil {
			return false, err
		}
		applyRecoveredResearchStep(state, number, decision, checkpoint)
		if state.ArgumentCorrections > 1 {
			return false, errors.New("工具参数纠正次数已用尽")
		}
		if state.Answer != "" {
			state.Status = VideoAgentLoopStatusCompleted
			state.StopReason = "answer_generated"
			if state.BudgetNotice != nil {
				state.StopReason = "budget_finalized"
			}
			return true, nil
		}
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

func applyRecoveredResearchStep(state *VideoAgentLoopState, number int, decision VideoAgentLoopDecision, checkpoint durableResearchToolCheckpoint) {
	observation := checkpoint.Observation
	state.CurrentStep++
	step := VideoAgentLoopStep{
		Number: number, Action: decision, Status: VideoAgentLoopStepCompleted,
		Trace: checkpoint.Result.Step, Observation: &observation,
	}
	if observation.ErrorClass == "invalid_arguments" {
		step.Status = VideoAgentLoopStepFailed
		step.Error = strings.Join(observation.UnresolvedQuestions, "；")
	}
	state.Steps = append(state.Steps, step)
	state.Observations = append(state.Observations, observation)
	state.Evidence = mergeVideoAgentLoopEvidence(state.Evidence, observation.NewEvidence)
	state.PendingQuestions = append([]string(nil), observation.UnresolvedQuestions...)
	if observation.ErrorClass == "invalid_arguments" {
		state.ArgumentCorrections++
	}
	if observation.Answer != "" {
		state.Answer = observation.Answer
		state.Citations = append([]Citation(nil), observation.Citations...)
	}
}

func (r *VideoAgentLoopRunner) nextResearchDecisionCheckpoint(ctx context.Context, state VideoAgentLoopState, runtime VideoAgentToolRuntime) (VideoAgentLoopDecision, bool, error) {
	if r.execution != nil {
		ctx = observability.WithCorrelation(ctx, observability.Correlation{TraceID: r.execution.runID, Stage: fmt.Sprintf("plan-%d", state.CurrentStep+1), Attempt: 1})
	}
	if r.execution == nil {
		decision, err := r.chooseDecision(ctx, state, r.registry.Definitions())
		if err != nil {
			return VideoAgentLoopDecision{}, false, err
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
	definitions := r.registry.Definitions()
	inputSummary, inputDigest := safePlannerInputSummary(state, definitions)
	plannerMessages, buildErr := buildPlannerMessages(state, definitions)
	if buildErr != nil {
		return VideoAgentLoopDecision{}, false, buildErr
	}
	plannerUsage := estimatedPlannerCallUsage(plannerMessages, "")
	forcedFinal := state.BudgetNotice != nil || state.CurrentStep >= state.MaxSteps-1
	if forcedFinal {
		plannerUsage = VideoAgentLoopPlannerCallUsage{UsageSource: model.AgentCallUsageUnknown}
	}
	journalResult, err := execution.journal.Execute(ctx, AgentJournalStep{
		UserID: execution.userID, RunID: execution.runID, StepID: stepID, Sequence: sequence,
		Kind: "plan", Action: "select_next_action", DigestAction: videoAgentLoopPlannerCall,
		SafeReason: "select the next allow-listed action", InputSummary: inputSummary, ArgumentsDigest: inputDigest,
		ToolName: videoAgentLoopPlannerCall, CallKind: model.AgentCallKindPlannerLLM, InternalCall: true,
		LLMCall: !forcedFinal, ContextChars: plannerUsage.ContextChars, EstimatedPromptTokens: plannerUsage.PromptTokens,
		FailureCode: "planner_failure",
	}, func() (AgentJournalResult, error) {
		decision, usage, planErr := r.chooseDecisionWithUsage(ctx, state, definitions)
		if planErr == nil {
			decision, planErr = r.validatedResearchDecision(state, runtime.TaskID, decision)
			if planErr != nil {
				planErr = &invalidResearchDecisionError{cause: planErr}
			}
		}
		checkpoint := durableResearchDecisionFrom(decision)
		if checkpoint.BudgetNotice == nil {
			checkpoint.BudgetNotice = state.BudgetNotice
		}
		return AgentJournalResult{
			Checkpoint: checkpoint, OutputRef: firstNonEmpty(decision.Tool, decision.StopReason, "done"),
			Usage: usage, MetricsJSON: usageMetrics(usage),
		}, planErr
	})
	if err != nil {
		return VideoAgentLoopDecision{}, false, err
	}
	if journalResult.BudgetExhausted {
		return VideoAgentLoopDecision{}, true, nil
	}
	var stored durableResearchDecision
	if err := json.Unmarshal(journalResult.Checkpoint, &stored); err != nil {
		return VideoAgentLoopDecision{}, false, fmt.Errorf("decode persisted planner decision: %w", err)
	}
	decision, err := r.validatedResearchDecision(state, runtime.TaskID, stored.toDecision())
	if err != nil {
		return VideoAgentLoopDecision{}, false, fmt.Errorf("validate persisted planner decision: %w", err)
	}
	return decision, false, nil
}

func callVideoAgentLoopPlanner(ctx context.Context, planner VideoAgentLoopPlanner, state VideoAgentLoopState, tools []VideoAgentToolDefinition) (VideoAgentLoopDecision, VideoAgentLoopPlannerCallUsage, error) {
	if observed, ok := planner.(VideoAgentLoopPlannerWithUsage); ok {
		return observed.NextDecisionWithUsage(ctx, state, tools)
	}
	decision, err := planner.NextDecision(ctx, state, tools)
	return decision, VideoAgentLoopPlannerCallUsage{UsageSource: model.AgentCallUsageUnknown}, err
}

func safePlannerInputSummary(state VideoAgentLoopState, tools []VideoAgentToolDefinition) (string, string) {
	state.BudgetNotice = nil // Reservation timing is recorded in the decision, not a replay input.
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

func plannerContextChars(state VideoAgentLoopState, tools []VideoAgentToolDefinition) int64 {
	messages, _ := buildPlannerMessages(state, tools)
	return estimatedPlannerCallUsage(messages, "").ContextChars
}

func (r *VideoAgentLoopRunner) executeResearchTool(ctx context.Context, state VideoAgentLoopState, runtime VideoAgentToolRuntime, decision VideoAgentLoopDecision) (VideoAgentToolResult, VideoAgentLoopObservation, bool, error) {
	if r.execution != nil {
		ctx = observability.WithCorrelation(ctx, observability.Correlation{TraceID: r.execution.runID, Stage: fmt.Sprintf("tool-%d", state.CurrentStep+1), Attempt: 1})
	}
	if err := runtime.checkScope(ctx, state.Evidence); err != nil {
		return VideoAgentToolResult{}, VideoAgentLoopObservation{}, false, err
	}
	if r.execution == nil {
		result, err := r.registry.Execute(ctx, decision.Tool, VideoAgentToolRequest{Runtime: runtime, Arguments: decision.Arguments})
		if err != nil {
			if observation, ok := recoverableToolObservation(result, err); ok {
				return result, observation, false, nil
			}
			return result, VideoAgentLoopObservation{}, false, err
		}
		observation, err := r.observeResearchTool(state, runtime.TaskID, result)
		return result, observation, false, err
	}
	execution := r.execution
	sequence := state.CurrentStep*2 + 2
	stepID := fmt.Sprintf("tool-%d", state.CurrentStep+1)
	inputSummary := safeResearchArgumentsSummary(decision.Tool, decision.Arguments)
	argsDigest := digestAgentValue(string(decision.Arguments))
	contextChars := researchToolContextChars(decision.Tool, decision.Arguments)
	usage := VideoAgentLoopPlannerCallUsage{ContextChars: contextChars, UsageSource: model.AgentCallUsageUnknown}
	estimatedPrompt := int64(0)
	if decision.Tool == VideoAgentToolBuildCitedAnswer {
		var args buildCitedAnswerToolArguments
		if err := json.Unmarshal(decision.Arguments, &args); err != nil {
			return VideoAgentToolResult{}, VideoAgentLoopObservation{}, false, err
		}
		messages := buildCitedAnswerMessages(BuildCitedAnswerInput{ScopeTaskIDs: runtime.TaskIDs, Question: args.Question, Intermediate: args.Intermediate, Citations: args.Citations, Recent: runtime.Recent}, runtime.MemorySnapshot)
		usage = estimatedPlannerCallUsage(messages, "")
		contextChars, estimatedPrompt = usage.ContextChars, usage.PromptTokens
		run, err := execution.journal.GetRun(ctx, execution.userID, execution.runID)
		if err != nil {
			return VideoAgentToolResult{}, VideoAgentLoopObservation{}, false, err
		}
		if run != nil {
			// Reserve is a minimum kept for finalization, not the final call's
			// ceiling. Spend only the frozen run's remaining output allowance.
			runtime.MaxOutputTokens = run.MaxCompletionTokens - run.CompletionTokensUsed
		}
		runtime.ReportUsage = func(reported VideoAgentLoopPlannerCallUsage) { usage = reported }
	}
	journalResult, err := execution.journal.Execute(ctx, AgentJournalStep{
		UserID: execution.userID, RunID: execution.runID, StepID: stepID, Sequence: sequence,
		Kind: videoAgentStepKind(decision.Tool), Action: decision.Tool, SafeReason: safeToolReason(decision.Tool),
		InputSummary: inputSummary, ArgumentsDigest: argsDigest, ToolName: decision.Tool,
		ReplaySafe: replaySafeAgentAction(decision.Tool), LLMCall: llmAgentAction(decision.Tool),
		VisionCall: visionAgentAction(decision.Tool), VisualCall: visualAgentAction(decision.Tool), FrameCount: visualFrameBudget(decision.Tool, decision.Arguments), RetrievalCall: retrievalAgentAction(decision.Tool),
		ContextChars: contextChars, EstimatedPromptTokens: estimatedPrompt, FailureCode: "tool_failure",
	}, func() (AgentJournalResult, error) {
		result, toolErr := r.registry.Execute(ctx, decision.Tool, VideoAgentToolRequest{Runtime: runtime, Arguments: decision.Arguments})
		var observation VideoAgentLoopObservation
		if toolErr == nil {
			observation, toolErr = r.observeResearchTool(state, runtime.TaskID, result)
		} else if corrected, ok := recoverableToolObservation(result, toolErr); ok {
			observation, toolErr = corrected, nil
		}
		return AgentJournalResult{
			Checkpoint: durableResearchToolCheckpoint{Result: result, Observation: observation},
			OutputRef:  agentToolOutputRef(result.Step), EvidenceRefs: researchObservationEvidenceRefs(observation),
			Usage: usage, MetricsJSON: usageMetrics(usage),
		}, toolErr
	})
	if err != nil {
		return VideoAgentToolResult{}, VideoAgentLoopObservation{}, false, err
	}
	if journalResult.BudgetExhausted {
		return VideoAgentToolResult{}, VideoAgentLoopObservation{}, true, nil
	}
	var stored durableResearchToolCheckpoint
	if err := json.Unmarshal(journalResult.Checkpoint, &stored); err != nil {
		return VideoAgentToolResult{}, VideoAgentLoopObservation{}, false, fmt.Errorf("decode persisted tool checkpoint: %w", err)
	}
	if stored.Result.Step.Tool != decision.Tool || stored.Observation.Tool != decision.Tool {
		return VideoAgentToolResult{}, VideoAgentLoopObservation{}, false, errors.New("persisted tool checkpoint does not match the validated action")
	}
	return stored.Result, stored.Observation, false, nil
}

func (r *VideoAgentLoopRunner) validatedResearchDecision(state VideoAgentLoopState, taskID int64, decision VideoAgentLoopDecision) (VideoAgentLoopDecision, error) {
	if err := r.validateDecision(state, decision); err != nil {
		return VideoAgentLoopDecision{}, err
	}
	if decision.Tool == VideoAgentToolBuildCitedAnswer {
		canonical, err := canonicalizeResearchAnswerArguments(state.Evidence, taskID, decision.Arguments)
		if err != nil {
			return VideoAgentLoopDecision{}, err
		}
		decision.Arguments = canonical
		var bound buildCitedAnswerToolArguments
		if err := json.Unmarshal(canonical, &bound); err != nil {
			return VideoAgentLoopDecision{}, err
		}
		bound.Question = state.Goal
		decision.Arguments, _ = json.Marshal(bound)
	}
	if decision.Tool == VideoAgentToolInvestigateVisual && state.MaxVisualFrames > 0 {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(decision.Arguments, &fields); err != nil {
			return decision, err
		}
		var budget VisualBudget
		if err := json.Unmarshal(fields["budget"], &budget); err != nil {
			return decision, err
		}
		if budget.MaxFrames <= 0 || budget.MaxFrames > state.MaxVisualFrames {
			budget.MaxFrames = state.MaxVisualFrames
		}
		fields["budget"], _ = json.Marshal(budget)
		decision.Arguments, _ = json.Marshal(fields)
	}
	decision.PublicSummary = trimRunes(strings.TrimSpace(decision.PublicSummary), 240)
	return decision, nil
}

func (r *VideoAgentLoopRunner) observeResearchTool(state VideoAgentLoopState, taskID int64, result VideoAgentToolResult) (VideoAgentLoopObservation, error) {
	observation, err := r.observer.Observe(state, result)
	if err != nil {
		return VideoAgentLoopObservation{}, err
	}
	if err := validateObservedResearchEvidence(taskID, observation.NewEvidence); err != nil {
		return VideoAgentLoopObservation{}, err
	}
	return observation, nil
}

func durableResearchDecisionFrom(decision VideoAgentLoopDecision) durableResearchDecision {
	return durableResearchDecision{BudgetNotice: decision.BudgetNotice, PublicSummary: decision.PublicSummary, Done: decision.Done, Tool: decision.Tool, Arguments: append(json.RawMessage(nil), decision.Arguments...), Replan: decision.Replan, StopReason: decision.StopReason}
}

func (d durableResearchDecision) toDecision() VideoAgentLoopDecision {
	reason := "select the persisted allow-listed action"
	if d.Done {
		reason = ""
	}
	return VideoAgentLoopDecision{BudgetNotice: d.BudgetNotice, PublicSummary: d.PublicSummary, Done: d.Done, Tool: d.Tool, Reason: reason, Arguments: append(json.RawMessage(nil), d.Arguments...), Replan: d.Replan, StopReason: d.StopReason}
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
	case VideoAgentToolBuildCitedAnswer:
		var input buildCitedAnswerToolArguments
		if json.Unmarshal(arguments, &input) == nil {
			summary["question_digest"], summary["intermediate_digest"], summary["citation_count"] = "sha256:"+digestAgentValue(input.Question), "sha256:"+digestAgentValue(input.Intermediate), len(input.Citations)
		}
	}
	encoded, _ := json.Marshal(summary)
	return string(encoded)
}

func researchObservationEvidenceRefs(observation VideoAgentLoopObservation) string {
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

func usageContextSource(usage VideoAgentLoopPlannerCallUsage) string {
	return usageSourceForContext(usage.ContextChars)
}

func usageMetrics(usage VideoAgentLoopPlannerCallUsage) string {
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

func mergeAgentUsageMetrics(raw string, usage VideoAgentLoopPlannerCallUsage, contextChars int64) string {
	metrics := map[string]any{}
	if strings.TrimSpace(raw) == "" || json.Unmarshal([]byte(raw), &metrics) != nil || metrics == nil {
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

// Reserve the last tool slot for one final answer, including explicit evidence gaps.
func (r *VideoAgentLoopRunner) chooseDecision(ctx context.Context, state VideoAgentLoopState, definitions []VideoAgentToolDefinition) (VideoAgentLoopDecision, error) {
	d, _, err := r.chooseDecisionWithUsage(ctx, state, definitions)
	return d, err
}
func (r *VideoAgentLoopRunner) chooseDecisionWithUsage(ctx context.Context, state VideoAgentLoopState, definitions []VideoAgentToolDefinition) (VideoAgentLoopDecision, VideoAgentLoopPlannerCallUsage, error) {
	if err := ctx.Err(); err != nil {
		return VideoAgentLoopDecision{}, VideoAgentLoopPlannerCallUsage{}, err
	}
	var d VideoAgentLoopDecision
	var u VideoAgentLoopPlannerCallUsage
	var err error
	_, finalToolErr := r.registry.Lookup(VideoAgentToolBuildCitedAnswer)
	if finalToolErr != nil || (state.BudgetNotice == nil && state.CurrentStep < state.MaxSteps-1) {
		planCtx := ctx
		cancel := func() {}
		var run *model.AgentRun
		var budget frozenAgentBudget
		if finalToolErr == nil && r.execution != nil {
			run, err = r.execution.journal.GetRun(ctx, r.execution.userID, r.execution.runID)
			if err != nil {
				return d, u, err
			}
			if run != nil {
				if err = json.Unmarshal([]byte(run.BudgetSnapshot), &budget); err != nil {
					return d, u, err
				}
				if budget.SchemaVersion >= 1 && budget.ReserveDurationMs > 0 && run.MaxDurationMs > budget.ReserveDurationMs {
					planCtx, cancel = context.WithDeadlineCause(ctx, run.CreatedAt.Add(time.Duration(run.MaxDurationMs-budget.ReserveDurationMs)*time.Millisecond), errPlannerFinalReserve)
				}
			}
		}
		defer cancel()
		d, u, err = callVideoAgentLoopPlanner(planCtx, r.planner, state, definitions)
		var finish *ai.ChatFinishError
		dimension := ""
		if errors.Is(context.Cause(planCtx), errPlannerFinalReserve) {
			dimension = "duration_ms"
		}
		if errors.As(err, &finish) && finish.Reason == "length" {
			dimension = "output_tokens"
		}
		if err != nil && ctx.Err() == nil && dimension != "" && run != nil && budget.SchemaVersion >= 1 {
			// Discard the incomplete model decision, keep its usage, and route
			// through the ordinary journaled writer with the remaining budget.
			notice := &AgentBudgetNotice{Dimension: dimension, UsageSource: u.UsageSource}
			if dimension == "duration_ms" {
				notice.Used, notice.Reserve, notice.Limit = time.Since(run.CreatedAt).Milliseconds(), budget.ReserveDurationMs, run.MaxDurationMs
			} else {
				notice.Used, notice.Reserve, notice.Limit = run.CompletionTokensUsed+u.CompletionTokens, budget.ReserveOutputTokens, run.MaxCompletionTokens
			}
			state.BudgetNotice, err = notice, nil
		}
	}
	if finalToolErr == nil && err == nil && (state.BudgetNotice != nil || state.CurrentStep >= state.MaxSteps-1 || d.Done || (d.Replan && state.ReplanCount >= state.MaxReplans)) {
		args, encodeErr := json.Marshal(buildCitedAnswerToolArguments{Question: state.Goal, Intermediate: "基于已有证据回答；明确说明未确认的信息与视觉限制。", Citations: state.Evidence})
		if encodeErr != nil {
			return d, u, encodeErr
		}
		d = VideoAgentLoopDecision{BudgetNotice: state.BudgetNotice, Tool: VideoAgentToolBuildCitedAnswer, Reason: "deliver available evidence and gaps", Arguments: args}
	}
	return d, u, err
}

var errPlannerFinalReserve = errors.New("planner reached reserved final answer time")
