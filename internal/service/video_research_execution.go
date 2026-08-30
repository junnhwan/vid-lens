package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"vid-lens/internal/repository"
)

var errAgentExecutionBusy = errors.New("agent execution step is owned by another worker")

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
	inputSummary, _ := json.Marshal(map[string]any{
		"schema": 1, "goal_digest": "sha256:" + digestAgentValue(state.Goal),
		"completed_steps": state.CurrentStep, "evidence_count": len(state.Evidence), "pending_question_count": len(state.PendingQuestions),
	})
	claim, err := execution.repo.ClaimStep(ctx, repository.AgentStepClaimRequest{
		UserID: execution.userID, RunID: execution.runID, StepID: stepID, Attempt: 1, Sequence: sequence,
		Kind: "plan", Action: "select_next_action", SafeReason: "select the next allow-listed action",
		InputSummary: string(inputSummary), ReplaySafe: false, LLMCall: true,
		LeaseToken: uuid.NewString(), Now: now, LeaseUntil: now.Add(agentStepLeaseDuration),
	})
	if err != nil {
		return VideoResearchDecision{}, false, err
	}
	switch claim.Outcome {
	case repository.AgentStepClaimCompleted:
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

	decision, planErr := r.planner.NextDecision(ctx, state, r.registry.Definitions())
	if planErr == nil {
		decision, planErr = r.validatedResearchDecision(state, runtime.TaskID, decision)
		if planErr != nil {
			planErr = &invalidResearchDecisionError{cause: planErr}
		}
	}
	if planErr != nil {
		_, _ = execution.repo.FailStep(context.WithoutCancel(ctx), repository.AgentStepFailure{
			UserID: execution.userID, RunID: execution.runID, StepID: stepID, Attempt: 1, LeaseToken: claim.Step.LeaseToken,
			ErrorCode: "planner_failure", ErrorMessage: safeAgentError(planErr), Now: execution.now(),
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
		OutputRef: firstNonEmpty(decision.Tool, decision.StopReason, "done"), ResultCheckpoint: string(encoded), Now: execution.now(),
	})
	if err != nil {
		return VideoResearchDecision{}, false, err
	}
	if !completed {
		return VideoResearchDecision{}, false, errors.New("planner completion CAS failed")
	}
	return checkpoint.toDecision(), false, nil
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
	claim, err := execution.repo.ClaimStep(ctx, repository.AgentStepClaimRequest{
		UserID: execution.userID, RunID: execution.runID, StepID: stepID, Attempt: 1, Sequence: sequence,
		Kind: videoAgentStepKind(decision.Tool), Action: decision.Tool, SafeReason: safeToolReason(decision.Tool),
		InputSummary: inputSummary, ArgumentsDigest: argsDigest,
		CallDigest: digestAgentValue(execution.runID + ":" + stepID + ":1:" + decision.Tool + ":" + argsDigest), ToolName: decision.Tool,
		ReplaySafe: replaySafeAgentAction(decision.Tool), LLMCall: llmAgentAction(decision.Tool), VisionCall: visionAgentAction(decision.Tool),
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
		_, _ = execution.repo.FailStep(context.WithoutCancel(ctx), repository.AgentStepFailure{
			UserID: execution.userID, RunID: execution.runID, StepID: stepID, Attempt: 1, LeaseToken: claim.Step.LeaseToken,
			ErrorCode: "tool_failure", ErrorMessage: safeAgentError(toolErr), Now: execution.now(),
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
