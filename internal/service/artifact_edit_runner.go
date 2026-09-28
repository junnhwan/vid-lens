package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

const artifactEditPlannerCall = "artifact_edit_planner"

type ArtifactEditProgressFunc func(context.Context, string, int, int) error

type ArtifactEditRunner struct {
	registry *VideoAgentToolRegistry
	planner  ArtifactEditPlanner
	journal  *AgentExecutionJournal
	userID   int64
	runID    string
	maxSteps int
	progress ArtifactEditProgressFunc
}

type artifactEditToolCheckpoint struct {
	Result VideoAgentToolResult `json:"result"`
}

func NewArtifactEditRunner(registry *VideoAgentToolRegistry, planner ArtifactEditPlanner, journal *AgentExecutionJournal, userID int64, runID string, maxSteps int, progress ArtifactEditProgressFunc) (*ArtifactEditRunner, error) {
	if registry == nil || planner == nil || journal == nil || userID <= 0 || strings.TrimSpace(runID) == "" || maxSteps < 2 {
		return nil, errors.New("artifact edit runner 参数无效")
	}
	return &ArtifactEditRunner{registry: registry, planner: planner, journal: journal, userID: userID, runID: runID, maxSteps: maxSteps, progress: progress}, nil
}

func (r *ArtifactEditRunner) Run(ctx context.Context, state ArtifactEditPlannerState, runtime *ArtifactEditToolRuntime) (*ArtifactEditToolOutcome, error) {
	if r == nil || runtime == nil {
		return nil, errors.New("artifact edit runner unavailable")
	}
	if state.ToolSchemaDigest != ArtifactEditToolSchemaDigest(r.registry.Definitions()) {
		return nil, errors.New("artifact edit frozen tool schema mismatch")
	}
	for stepNumber := 1; stepNumber <= r.maxSteps/2; stepNumber++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if r.progress != nil {
			if err := r.progress(ctx, artifactEditPlanningStage(state), stepNumber-1, r.maxSteps/2); err != nil {
				return nil, err
			}
		}
		decision, err := r.plan(ctx, stepNumber, state)
		if err != nil {
			return nil, err
		}
		checkpoint, err := r.execute(ctx, stepNumber, state, runtime, decision)
		if err != nil {
			return nil, err
		}
		if err = restoreArtifactEditToolRuntime(runtime, decision.Tool, checkpoint.Result.Output); err != nil {
			return nil, err
		}
		state.Observations = append(state.Observations, ArtifactEditPlannerObservation{Tool: decision.Tool, Output: append(json.RawMessage(nil), checkpoint.Result.Output...)})
		if runtime.Proposal != nil {
			state.ProposalOperationID = runtime.Proposal.OperationID
		}
		if runtime.Outcome != nil {
			if r.progress != nil {
				if err := r.progress(ctx, artifactEditTerminalStage(runtime.Outcome.Kind), stepNumber, r.maxSteps/2); err != nil {
					return nil, err
				}
			}
			outcome := *runtime.Outcome
			return &outcome, nil
		}
		if decision.Tool == ArtifactEditToolProposePatch && state.Mode == ArtifactEditModePreview {
			outcome := &ArtifactEditToolOutcome{Kind: "proposal", OperationID: runtime.Proposal.OperationID}
			runtime.Outcome = outcome
			if r.progress != nil {
				if err := r.progress(ctx, "proposal_ready", stepNumber, r.maxSteps/2); err != nil {
					return nil, err
				}
			}
			return outcome, nil
		}
	}
	return nil, artifact.Err("budget_exhausted", 422)
}

func (r *ArtifactEditRunner) plan(ctx context.Context, number int, state ArtifactEditPlannerState) (ArtifactEditPlannerDecision, error) {
	definitions := r.registry.Definitions()
	stateJSON, _ := json.Marshal(state)
	inputDigest := artifact.Hash("artifact-edit-planner:v1:" + string(stateJSON) + ":" + state.ToolSchemaDigest)
	inputSummary, _ := json.Marshal(map[string]any{
		"schema": 1, "planner_version": "artifact-edit-planner:v1", "input_digest": "sha256:" + inputDigest,
		"instruction_digest": "sha256:" + artifact.Hash(state.Instruction), "base_digest": state.BaseDigest,
		"scope_digest": state.ScopeDigest, "tool_schema_digest": state.ToolSchemaDigest, "observation_count": len(state.Observations),
	})
	execution, err := r.journal.Execute(ctx, AgentJournalStep{
		UserID: r.userID, RunID: r.runID, StepID: fmt.Sprintf("edit-plan-%d", number), Sequence: number*2 - 1,
		Kind: "plan", Action: "select_edit_action", DigestAction: artifactEditPlannerCall,
		SafeReason: "select the next allow-listed artifact edit action", InputSummary: string(inputSummary), ArgumentsDigest: inputDigest,
		ToolName: artifactEditPlannerCall, CallKind: model.AgentCallKindPlannerLLM, InternalCall: true,
		ReplaySafe: true, RetryReplaySafe: true, LLMCall: true, EstimatedPromptTokens: max(int64(1), plannerContextCharsForArtifactEdit(state, definitions)/4),
		ContextChars: plannerContextCharsForArtifactEdit(state, definitions), FailureCode: "planner_failure",
	}, func() (AgentJournalResult, error) {
		decision, usage, planErr := r.planner.NextDecisionWithUsage(ctx, state, definitions)
		if planErr == nil {
			planErr = ValidateArtifactEditDecision(state, r.registry, decision)
		}
		return AgentJournalResult{Checkpoint: decision, OutputRef: decision.Tool, Usage: usage, MetricsJSON: usageMetrics(usage)}, planErr
	})
	if err != nil {
		return ArtifactEditPlannerDecision{}, err
	}
	if execution.BudgetExhausted {
		return ArtifactEditPlannerDecision{}, artifact.Err("budget_exhausted", 422)
	}
	var decision ArtifactEditPlannerDecision
	if err := json.Unmarshal(execution.Checkpoint, &decision); err != nil {
		return ArtifactEditPlannerDecision{}, fmt.Errorf("decode persisted artifact edit decision: %w", err)
	}
	if err := ValidateArtifactEditDecision(state, r.registry, decision); err != nil {
		return ArtifactEditPlannerDecision{}, fmt.Errorf("validate persisted artifact edit decision: %w", err)
	}
	return decision, nil
}

func (r *ArtifactEditRunner) execute(ctx context.Context, number int, state ArtifactEditPlannerState, runtime *ArtifactEditToolRuntime, decision ArtifactEditPlannerDecision) (artifactEditToolCheckpoint, error) {
	argumentsDigest := artifact.Hash(string(decision.Arguments))
	inputSummary, _ := json.Marshal(map[string]any{
		"schema": 1, "arguments_digest": "sha256:" + argumentsDigest, "base_digest": state.BaseDigest,
		"scope_digest": state.ScopeDigest, "tool_schema_digest": state.ToolSchemaDigest,
	})
	if r.progress != nil {
		if err := r.progress(ctx, artifactEditToolStage(decision.Tool), number-1, r.maxSteps/2); err != nil {
			return artifactEditToolCheckpoint{}, err
		}
	}
	execution, err := r.journal.Execute(ctx, AgentJournalStep{
		UserID: r.userID, RunID: r.runID, StepID: fmt.Sprintf("edit-tool-%d", number), Sequence: number * 2,
		Kind: "tool", Action: decision.Tool, SafeReason: safeArtifactEditToolReason(decision.Tool),
		InputSummary: string(inputSummary), ArgumentsDigest: argumentsDigest, ToolName: decision.Tool, CallKind: model.AgentCallKindTool,
		ReplaySafe: true, RetryReplaySafe: true, FailureCode: "tool_failure",
	}, func() (AgentJournalResult, error) {
		result, toolErr := r.registry.Execute(ctx, decision.Tool, VideoAgentToolRequest{Runtime: VideoAgentToolRuntime{ArtifactEdit: runtime}, Arguments: decision.Arguments})
		evidenceRefs := artifactEditToolEvidenceRefs(decision.Tool, decision.Arguments)
		return AgentJournalResult{Checkpoint: artifactEditToolCheckpoint{Result: result}, OutputRef: artifactEditToolOutputRef(result), EvidenceRefs: evidenceRefs}, toolErr
	})
	if err != nil {
		return artifactEditToolCheckpoint{}, err
	}
	if execution.BudgetExhausted {
		return artifactEditToolCheckpoint{}, artifact.Err("budget_exhausted", 422)
	}
	var checkpoint artifactEditToolCheckpoint
	if err := json.Unmarshal(execution.Checkpoint, &checkpoint); err != nil {
		return artifactEditToolCheckpoint{}, fmt.Errorf("decode persisted artifact edit tool checkpoint: %w", err)
	}
	if checkpoint.Result.Step.Tool != decision.Tool {
		return artifactEditToolCheckpoint{}, errors.New("persisted artifact edit tool checkpoint does not match decision")
	}
	return checkpoint, nil
}

func restoreArtifactEditToolRuntime(runtime *ArtifactEditToolRuntime, tool string, output json.RawMessage) error {
	switch tool {
	case ArtifactEditToolProposePatch:
		var proposal ArtifactEditProposal
		if err := json.Unmarshal(output, &proposal); err != nil {
			return err
		}
		if proposal.OperationID != runtime.OperationID || proposal.PatchHash == "" {
			return errors.New("persisted artifact edit proposal identity mismatch")
		}
		runtime.Proposal = &proposal
	case ArtifactEditToolAnswerQuestion, ArtifactEditToolNothingToChange, ArtifactEditToolCommitPatch:
		var outcome ArtifactEditToolOutcome
		if err := json.Unmarshal(output, &outcome); err != nil {
			return err
		}
		runtime.Outcome = &outcome
	}
	return nil
}

func plannerContextCharsForArtifactEdit(state ArtifactEditPlannerState, tools []VideoAgentToolDefinition) int64 {
	messages, err := buildArtifactEditPlannerMessages(state, tools)
	if err != nil {
		return 0
	}
	return estimatedPlannerCallUsage(messages, "").ContextChars
}

func artifactEditToolEvidenceRefs(tool string, arguments json.RawMessage) string {
	refs := []string{}
	switch tool {
	case ArtifactEditToolInspectEvidence:
		var args artifactEditEvidenceArguments
		if json.Unmarshal(arguments, &args) == nil {
			refs = args.EvidenceIDs
		}
	case ArtifactEditToolAnswerQuestion, ArtifactEditToolNothingToChange:
		var args artifactEditMessageArguments
		if json.Unmarshal(arguments, &args) == nil {
			refs = args.EvidenceIDs
		}
	case ArtifactEditToolProposePatch:
		var args artifactEditProposeArguments
		if json.Unmarshal(arguments, &args) == nil {
			refs = args.Patch.EvidenceIDs
		}
	}
	encoded, _ := json.Marshal(refs)
	return string(encoded)
}

func artifactEditToolOutputRef(result VideoAgentToolResult) string {
	if result.Step.Error != "" {
		return "error:" + result.Step.Error
	}
	return result.Step.Tool
}

func safeArtifactEditToolReason(tool string) string {
	switch tool {
	case ArtifactEditToolRead:
		return "read the frozen base within authorized scope"
	case ArtifactEditToolFindBlocks:
		return "find text only within authorized blocks"
	case ArtifactEditToolInspectEvidence:
		return "inspect bounded evidence from the frozen manifest"
	case ArtifactEditToolAnswerQuestion:
		return "answer without creating a version"
	case ArtifactEditToolProposePatch:
		return "validate and persist one bounded proposal"
	case ArtifactEditToolNothingToChange:
		return "finish without creating an empty version"
	case ArtifactEditToolCommitPatch:
		return "commit the already validated deterministic proposal"
	default:
		return "execute allow-listed artifact edit action"
	}
}

func artifactEditPlanningStage(state ArtifactEditPlannerState) string {
	if state.ProposalOperationID != "" {
		return "saving"
	}
	if len(state.Observations) == 0 {
		return "reading"
	}
	return "checking"
}

func artifactEditToolStage(tool string) string {
	switch tool {
	case ArtifactEditToolRead, ArtifactEditToolFindBlocks:
		return "reading"
	case ArtifactEditToolInspectEvidence:
		return "checking"
	case ArtifactEditToolProposePatch:
		return "modifying"
	case ArtifactEditToolCommitPatch:
		return "saving"
	default:
		return "finalizing"
	}
}

func artifactEditTerminalStage(kind string) string {
	if kind == "committed" {
		return "saved"
	}
	return "completed"
}
