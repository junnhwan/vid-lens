package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/observability"
)

type ArtifactEditRequest struct {
	Instruction         string           `json:"instruction"`
	ExpectedHeadVersion int64            `json:"expected_head_version"`
	SelectedBlockIDs    []string         `json:"selected_block_ids"`
	Mode                ArtifactEditMode `json:"mode"`
}

type artifactEditToolPolicy struct {
	SchemaVersion    int              `json:"schema_version"`
	Mode             ArtifactEditMode `json:"mode"`
	AllowedTools     []string         `json:"allowed_tools"`
	BaseDigest       string           `json:"base_digest"`
	ScopeDigest      string           `json:"scope_digest"`
	ToolSchemaDigest string           `json:"tool_schema_digest"`
	TermRules        VideoTermRuleSet `json:"term_rules,omitempty"`
	TermSnapshotHash string           `json:"term_snapshot_hash,omitempty"`
}

type ArtifactEditRunResult struct {
	Kind            string    `json:"kind"`
	Message         string    `json:"message,omitempty"`
	EvidenceIDs     *[]string `json:"evidence_ids,omitempty"`
	OperationID     string    `json:"operation_id,omitempty"`
	ResultVersionID string    `json:"result_version_id,omitempty"`
}

type ArtifactEditRunView struct {
	ID                  string                 `json:"id"`
	ArtifactID          string                 `json:"artifact_id"`
	Instruction         string                 `json:"instruction"`
	ExpectedHeadVersion int64                  `json:"expected_head_version"`
	BaseVersionID       string                 `json:"base_version_id"`
	SelectedBlockIDs    []string               `json:"selected_block_ids"`
	Mode                ArtifactEditMode       `json:"mode"`
	Status              string                 `json:"status"`
	Stage               string                 `json:"stage"`
	CancelRequested     bool                   `json:"cancel_requested"`
	CanCancel           bool                   `json:"can_cancel"`
	Result              *ArtifactEditRunResult `json:"result"`
	ErrorCode           *string                `json:"error_code"`
	CreatedAt           time.Time              `json:"created_at"`
	StartedAt           *time.Time             `json:"started_at"`
	FinishedAt          *time.Time             `json:"finished_at"`
	LastSeq             int64                  `json:"last_seq"`
	Usage               ArtifactRunUsage       `json:"usage"`
}

type ArtifactEditOperationView struct {
	ID              string                  `json:"id"`
	ArtifactID      string                  `json:"artifact_id"`
	Status          string                  `json:"status"`
	BaseVersion     int64                   `json:"base_version"`
	BaseVersionID   string                  `json:"base_version_id"`
	ResultVersionID *string                 `json:"result_version_id"`
	UndoVersionID   *string                 `json:"undo_version_id"`
	Basis           string                  `json:"basis"`
	EvidenceIDs     []string                `json:"evidence_ids"`
	Summary         string                  `json:"summary"`
	Counts          artifact.PatchCounts    `json:"counts"`
	Changes         []artifact.PatchChange  `json:"changes"`
	BlockMappings   []artifact.BlockMapping `json:"block_mappings"`
	CanApply        bool                    `json:"can_apply"`
	CanUndo         bool                    `json:"can_undo"`
	CreatedAt       time.Time               `json:"created_at"`
	UpdatedAt       time.Time               `json:"updated_at"`
	CommittedAt     *time.Time              `json:"committed_at"`
}

func (input *ArtifactEditRequest) normalizeAndValidate() error {
	if input == nil {
		return artifact.Err("invalid_request", 400)
	}
	input.Instruction = strings.TrimSpace(input.Instruction)
	if input.Instruction == "" || utf8.RuneCountInString(input.Instruction) > 2000 || input.ExpectedHeadVersion <= 0 || len(input.SelectedBlockIDs) > 20 {
		return artifact.Err("invalid_request", 400)
	}
	if input.Mode != ArtifactEditModeAnswer && input.Mode != ArtifactEditModePreview && input.Mode != ArtifactEditModeApply {
		return artifact.Err("invalid_request", 400)
	}
	seen := make(map[string]bool, len(input.SelectedBlockIDs))
	for i, id := range input.SelectedBlockIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return artifact.Err("invalid_request", 400)
		}
		seen[id] = true
		input.SelectedBlockIDs[i] = id
	}
	return nil
}

func (s *ArtifactService) SubmitEdit(ctx context.Context, owner int64, artifactID, key string, input ArtifactEditRequest) (*ArtifactEditRunView, error) {
	artifactID = strings.TrimSpace(artifactID)
	if owner <= 0 || artifactID == "" || input.normalizeAndValidate() != nil {
		return nil, artifact.Err("invalid_request", 400)
	}
	if err := artifact.ValidateKey(key); err != nil {
		return nil, err
	}
	requestHash := artifact.Hash(artifact.JSON(struct {
		ArtifactID string              `json:"artifact_id"`
		Input      ArtifactEditRequest `json:"input"`
	}{ArtifactID: artifactID, Input: input}))
	prior, err := s.repos.Artifact.EditByKey(ctx, owner, key, requestHash)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		return s.EditRun(ctx, owner, prior.RunID)
	}

	target, base, err := s.repos.Artifact.Get(ctx, owner, artifactID)
	if err != nil {
		return nil, err
	}
	if target.HeadVersion != input.ExpectedHeadVersion || base == nil || base.Version != input.ExpectedHeadVersion || target.CurrentVersionID == nil || *target.CurrentVersionID != base.ID {
		return nil, artifact.Err("version_conflict", 409)
	}
	manifest, evidence, err := s.repos.Artifact.Snapshot(ctx, owner, base.ManifestID)
	if err != nil {
		return nil, err
	}
	var body artifact.Body
	if err = artifact.Decode([]byte(base.BodyJSON), &body); err != nil {
		return nil, err
	}
	registry, schemaDigest, err := NewArtifactEditToolRegistry(input.Mode)
	if err != nil {
		return nil, err
	}
	digests, err := FreezeArtifactEditDigests(body, input.SelectedBlockIDs, registry.Definitions())
	if err != nil {
		return nil, err
	}
	if digests.ToolSchema != schemaDigest {
		return nil, errors.New("artifact edit tool schema digest mismatch")
	}
	resolved, err := s.profiles.GetDefaultConversationProfile(owner)
	if err != nil || resolved == nil || resolved.Profile == nil {
		return nil, artifact.Err("profile_required", 422)
	}
	profile := resolved.Profile
	if strings.TrimSpace(profile.LLMAPIKey) == "" || strings.TrimSpace(profile.LLMModel) == "" {
		return nil, artifact.Err("profile_required", 422)
	}
	definitions := registry.Definitions()
	allowedTools := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		allowedTools = append(allowedTools, definition.Name)
	}
	rules, err := EffectiveTermRules(ctx, s.repos, owner, manifest.SourceID)
	if err != nil {
		return nil, err
	}
	policy := artifactEditToolPolicy{SchemaVersion: 2, Mode: input.Mode, AllowedTools: allowedTools, BaseDigest: digests.Base, ScopeDigest: digests.Scope, ToolSchemaDigest: schemaDigest, TermRules: rules, TermSnapshotHash: artifact.Hash(artifact.JSON(rules))}
	budget := resolved.EffectiveAgentBudget.Values
	now := time.Now().UTC()
	runID := uuid.NewString()
	requestID := uuid.NewString()
	maxToolCalls := budget.MaxToolCalls
	if maxToolCalls < 1 {
		maxToolCalls = 1
	}
	run := &model.AgentRun{
		ID: runID, UserID: owner, SubjectKind: model.AgentRunSubjectArtifactEdit, SubjectID: requestID, ExecutionKind: "artifact", RecipeVersion: ArtifactEditRecipe,
		ScopeType: "video", TaskID: manifest.SourceID, Goal: input.Instruction, Mode: string(input.Mode), AgentProfile: "default",
		ProfileSnapshot: artifact.JSON(map[string]any{"profile_id": profile.ID, "model": profile.LLMModel, "fingerprint": profileFingerprint(profile)}),
		PolicySnapshot:  artifact.JSON(policy), BudgetSnapshot: artifact.JSON(resolved.EffectiveAgentBudget), Status: model.AgentRunStatusPending, Stage: "queued", Version: 1,
		MaxSteps: maxToolCalls * 2, MaxToolCalls: maxToolCalls, MaxLLMCalls: maxToolCalls, MaxAttemptsPerStep: 2,
		MaxPromptTokens: int64(budget.MaxInputTokens), MaxCompletionTokens: int64(budget.MaxOutputTokens), MaxDurationMs: int64(budget.MaxDurationSeconds) * 1000,
		MaxContextChars: int64(profile.LLMContextTokens), CreatedAt: now, UpdatedAt: now,
	}
	request := &model.ArtifactEditRequest{
		ID: requestID, RunID: runID, UserID: owner, IdempotencyKey: key, RequestHash: requestHash, RequestJSON: artifact.JSON(input),
		ArtifactID: artifactID, BaseVersionID: base.ID, BaseVersion: base.Version, ManifestID: manifest.ID, Instruction: input.Instruction,
		Mode: string(input.Mode), SelectedBlockIDsJSON: artifact.JSON(input.SelectedBlockIDs), Recipe: ArtifactEditRecipe,
		ProfileID: profile.ID, ProfileFingerprint: profileFingerprint(profile), BudgetJSON: artifact.JSON(resolved.EffectiveAgentBudget),
		ToolPolicyJSON: artifact.JSON(policy), QueueDeadline: now.Add(24 * time.Hour), CreatedAt: now,
	}
	_ = evidence // Snapshot read above is the owner/revocation gate; items are loaded again by the worker.
	saved, err := s.repos.Artifact.SubmitEdit(ctx, request, run)
	if err != nil {
		return nil, err
	}
	return s.EditRun(ctx, owner, saved.RunID)
}

func (s *ArtifactService) EditRun(ctx context.Context, owner int64, runID string) (*ArtifactEditRunView, error) {
	run, request, err := s.repos.Artifact.ReadableEditRun(ctx, owner, runID)
	if err != nil {
		return nil, err
	}
	selected := []string{}
	if err = json.Unmarshal([]byte(request.SelectedBlockIDsJSON), &selected); err != nil {
		return nil, err
	}
	active := run.Status == model.AgentRunStatusPending || run.Status == model.AgentRunStatusRunning
	view := &ArtifactEditRunView{
		ID: run.ID, ArtifactID: request.ArtifactID, Instruction: request.Instruction, ExpectedHeadVersion: request.BaseVersion,
		BaseVersionID: request.BaseVersionID, SelectedBlockIDs: selected, Mode: ArtifactEditMode(request.Mode), Status: run.Status, Stage: run.Stage,
		CancelRequested: run.CancelRequestedAt != nil, CanCancel: active && run.CancelRequestedAt == nil, CreatedAt: run.CreatedAt,
		StartedAt: run.ExecutionStartedAt, FinishedAt: run.FinishedAt, LastSeq: run.EventSeq,
		Usage: ArtifactRunUsage{LLMCalls: run.LLMCallsUsed, PromptTokens: run.PromptTokensUsed, CompletionTokens: run.CompletionTokensUsed, TokenSource: run.TokenUsageSource},
	}
	if run.ErrorCode != "" {
		view.ErrorCode = &run.ErrorCode
	}
	view.Result, err = s.artifactEditRunResult(ctx, run, request)
	return view, err
}

func (s *ArtifactService) artifactEditRunResult(ctx context.Context, run *model.AgentRun, request *model.ArtifactEditRequest) (*ArtifactEditRunResult, error) {
	if run == nil || request == nil || run.Status != model.AgentRunStatusCompleted {
		return nil, nil
	}
	operationID := artifactEditOperationID(run.ID)
	operation, err := s.repos.Artifact.EditOperation(ctx, run.UserID, operationID)
	if err == nil && operation != nil {
		result := &ArtifactEditRunResult{Kind: "proposal", OperationID: operation.ID}
		if operation.Status == model.ArtifactEditOperationCommitted && operation.ResultVersionID != nil {
			result.Kind, result.ResultVersionID = "committed", *operation.ResultVersionID
		}
		return result, nil
	}
	if !isArtifactNotFound(err) {
		return nil, err
	}
	records, err := s.repos.AgentExecution.GetExecution(ctx, run.UserID, run.ID)
	if err != nil {
		return nil, err
	}
	if records == nil {
		return nil, nil
	}
	for i := len(records.ToolCalls) - 1; i >= 0; i-- {
		call := records.ToolCalls[i]
		if call.Status != model.AgentToolCallStatusCompleted || (call.ToolName != ArtifactEditToolAnswerQuestion && call.ToolName != ArtifactEditToolNothingToChange && call.ToolName != ArtifactEditToolCommitPatch) {
			continue
		}
		var checkpoint artifactEditToolCheckpoint
		if json.Unmarshal([]byte(call.ResultCheckpoint), &checkpoint) != nil {
			continue
		}
		var outcome ArtifactEditToolOutcome
		if json.Unmarshal(checkpoint.Result.Output, &outcome) == nil && outcome.Kind != "" {
			result := &ArtifactEditRunResult{Kind: outcome.Kind, Message: outcome.Message, OperationID: outcome.OperationID, ResultVersionID: outcome.ResultVersionID}
			if outcome.Kind == "answer" || outcome.Kind == "no_change" {
				evidenceIDs := append([]string{}, outcome.EvidenceIDs...)
				result.EvidenceIDs = &evidenceIDs
			}
			return result, nil
		}
	}
	return nil, nil
}

func (s *ArtifactService) CancelEdit(ctx context.Context, owner int64, runID string) (*ArtifactEditRunView, error) {
	if err := s.repos.Artifact.Cancel(ctx, owner, runID, model.AgentRunSubjectArtifactEdit); err != nil {
		return nil, err
	}
	return s.EditRun(ctx, owner, runID)
}

func (s *ArtifactService) EditEvents(ctx context.Context, owner int64, runID string, after int64) ([]model.RunEvent, error) {
	if _, _, err := s.repos.Artifact.ReadableEditRun(ctx, owner, runID); err != nil {
		return nil, err
	}
	return s.repos.Artifact.Events(ctx, owner, runID, model.AgentRunSubjectArtifactEdit, after)
}

func (s *ArtifactService) EditOperation(ctx context.Context, owner int64, operationID string) (*ArtifactEditOperationView, error) {
	operation, err := s.repos.Artifact.EditOperation(ctx, owner, operationID)
	if err != nil {
		return nil, err
	}
	return artifactEditOperationView(operation)
}

func (s *ArtifactService) ApplyEdit(ctx context.Context, owner int64, operationID, key string, expectedHead int64) (*ArtifactEditOperationView, error) {
	if err := artifact.ValidateKey(key); err != nil || expectedHead <= 0 {
		return nil, artifact.Err("invalid_request", 400)
	}
	hash := artifact.Hash(artifact.JSON(map[string]any{"action": "apply", "operation_id": operationID, "expected_head_version": expectedHead}))
	operation, _, err := s.repos.Artifact.ApplyEdit(ctx, owner, operationID, expectedHead, key, hash, artifactSource)
	if err != nil {
		return nil, err
	}
	return artifactEditOperationView(operation)
}

func (s *ArtifactService) UndoEdit(ctx context.Context, owner int64, operationID, key string, expectedHead int64) (*ArtifactEditOperationView, error) {
	if err := artifact.ValidateKey(key); err != nil || expectedHead <= 0 {
		return nil, artifact.Err("invalid_request", 400)
	}
	hash := artifact.Hash(artifact.JSON(map[string]any{"action": "undo", "operation_id": operationID, "expected_head_version": expectedHead}))
	operation, _, err := s.repos.Artifact.UndoEdit(ctx, owner, operationID, expectedHead, key, hash, artifactSource)
	if err != nil {
		return nil, err
	}
	return artifactEditOperationView(operation)
}

func artifactEditOperationView(operation *model.ArtifactEditOperation) (*ArtifactEditOperationView, error) {
	if operation == nil {
		return nil, artifact.Err("not_found", 404)
	}
	view := &ArtifactEditOperationView{
		ID: operation.ID, ArtifactID: operation.ArtifactID, Status: operation.Status, BaseVersion: operation.BaseVersion,
		BaseVersionID: operation.BaseVersionID, ResultVersionID: operation.ResultVersionID, UndoVersionID: operation.UndoVersionID,
		Basis: operation.Basis, Summary: operation.Summary, EvidenceIDs: []string{}, Changes: []artifact.PatchChange{}, BlockMappings: []artifact.BlockMapping{},
		CanApply:  operation.Status == model.ArtifactEditOperationProposed,
		CanUndo:   operation.Status == model.ArtifactEditOperationCommitted && operation.ResultVersionID != nil && operation.UndoVersionID == nil,
		CreatedAt: operation.CreatedAt, UpdatedAt: operation.UpdatedAt, CommittedAt: operation.CommittedAt,
	}
	for _, field := range []struct {
		raw    string
		target any
	}{
		{operation.EvidenceIDsJSON, &view.EvidenceIDs},
		{operation.CountsJSON, &view.Counts},
		{operation.ChangesJSON, &view.Changes},
		{operation.BlockMappingsJSON, &view.BlockMappings},
	} {
		if strings.TrimSpace(field.raw) != "" {
			if err := json.Unmarshal([]byte(field.raw), field.target); err != nil {
				return nil, err
			}
		}
	}
	return view, nil
}

func artifactEditOperationID(runID string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(ArtifactEditRecipe+":"+runID+":proposal:1")).String()
}

func isArtifactNotFound(err error) bool {
	return isArtifactErrorCode(err, "not_found")
}

func isArtifactErrorCode(err error, code string) bool {
	var domain *artifact.Error
	return errors.As(err, &domain) && domain.Code == code
}

func (s *ArtifactService) ExecuteArtifactEdit(parent context.Context, runID string) error {
	if _, recovered, err := s.repos.Artifact.RecoverCommittedEdit(parent, runID); err != nil {
		return err
	} else if recovered {
		return nil
	}
	token := uuid.NewString()
	run, err := s.repos.Artifact.Claim(parent, runID, token, time.Now().UTC())
	if errors.Is(err, artifact.ErrLease) {
		return nil
	}
	if err != nil {
		return err
	}
	if run.Status != model.AgentRunStatusRunning {
		return nil
	}
	ctx, cancel := context.WithDeadlineCause(parent, run.ExecutionStartedAt.Add(time.Duration(run.MaxDurationMs)*time.Millisecond), errAgentRunDurationLimit)
	ctx = observability.WithCorrelation(ctx, observability.Correlation{UserID: run.UserID, TaskID: run.TaskID, TraceID: run.ID})
	ctx = ai.WithGovernanceContext(ctx, ai.GovernanceContext{Subject: fmt.Sprintf("user:%d", run.UserID), OperationKey: run.ID})
	defer cancel()
	done := make(chan struct{})
	var heartbeat sync.WaitGroup
	heartbeat.Add(1)
	go func() {
		defer heartbeat.Done()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if heartbeatErr := s.repos.Artifact.Heartbeat(ctx, runID, token, run.RunLeaseEpoch); heartbeatErr != nil {
					cancel()
					return
				}
				current, _, heartbeatErr := s.repos.Artifact.EditRun(ctx, run.UserID, runID)
				if heartbeatErr != nil || current.Status != model.AgentRunStatusRunning || current.CancelRequestedAt != nil {
					cancel()
					return
				}
			}
		}
	}()
	executionErr := s.executeClaimedArtifactEdit(ctx, run, token)
	close(done)
	heartbeat.Wait()
	if parent.Err() != nil {
		return parent.Err()
	}
	if executionErr == nil {
		return nil
	}
	checkCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if _, recovered, recoverErr := s.repos.Artifact.RecoverCommittedEdit(checkCtx, runID); recoverErr != nil {
		return recoverErr
	} else if recovered {
		return nil
	}
	current, _, readErr := s.repos.Artifact.EditRun(checkCtx, run.UserID, runID)
	if readErr != nil {
		return readErr
	}
	if current.Status != model.AgentRunStatusRunning {
		return nil
	}
	if current.CancelRequestedAt != nil {
		return s.repos.Artifact.Finish(checkCtx, runID, token, run.RunLeaseEpoch, model.AgentRunStatusCancelled, "")
	}
	if errors.Is(executionErr, artifact.ErrLease) || errors.Is(executionErr, context.Canceled) {
		return nil
	}
	status, code := model.AgentRunStatusFailed, "provider_error"
	var domain *artifact.Error
	if errors.As(executionErr, &domain) {
		code = domain.Code
	}
	if errors.Is(executionErr, context.DeadlineExceeded) || errors.Is(executionErr, errAgentRunDurationLimit) || errors.Is(context.Cause(ctx), errAgentRunDurationLimit) || code == "budget_exhausted" {
		status, code = model.AgentRunStatusBudgetExhausted, "budget_exhausted"
	}
	var finish *ai.ChatFinishError
	if errors.As(executionErr, &finish) {
		code = "provider_truncated"
		if finish.Reason == "content_filter" {
			code = "provider_refused"
		}
	}
	return s.repos.Artifact.Finish(checkCtx, runID, token, run.RunLeaseEpoch, status, code)
}

func (s *ArtifactService) executeClaimedArtifactEdit(ctx context.Context, run *model.AgentRun, token string) error {
	execution, err := s.repos.Artifact.EditContext(ctx, run.UserID, run.ID)
	if err != nil {
		return err
	}
	request := execution.Request
	if request.Recipe != ArtifactEditRecipe || request.Recipe != run.RecipeVersion || request.Mode != run.Mode || request.Instruction != run.Goal {
		return artifact.Err("unsupported_checkpoint", 409)
	}
	mode := ArtifactEditMode(request.Mode)
	registry, schemaDigest, err := NewArtifactEditToolRegistry(mode)
	if err != nil {
		return err
	}
	selected := []string{}
	if err = json.Unmarshal([]byte(request.SelectedBlockIDsJSON), &selected); err != nil {
		return artifact.Err("unsupported_checkpoint", 409)
	}
	digests, err := FreezeArtifactEditDigests(execution.Body, selected, registry.Definitions())
	if err != nil {
		return err
	}
	var policy artifactEditToolPolicy
	if err = artifact.Decode([]byte(request.ToolPolicyJSON), &policy); err != nil {
		return artifact.Err("unsupported_checkpoint", 409)
	}
	definitions := registry.Definitions()
	wantedTools := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		wantedTools = append(wantedTools, definition.Name)
	}
	if (policy.SchemaVersion != 1 && policy.SchemaVersion != 2) || policy.Mode != mode || !equalStrings(policy.AllowedTools, wantedTools) || policy.ToolSchemaDigest != schemaDigest || policy.BaseDigest != digests.Base || policy.ScopeDigest != digests.Scope {
		return artifact.Err("unsupported_checkpoint", 409)
	}
	if policy.SchemaVersion == 2 && policy.TermSnapshotHash != artifact.Hash(artifact.JSON(policy.TermRules)) {
		return artifact.Err("unsupported_checkpoint", 409)
	}
	client, err := s.artifactEditClient(run.UserID, request)
	if err != nil {
		return err
	}
	operationID := artifactEditOperationID(run.ID)
	runtime := &ArtifactEditToolRuntime{
		ArtifactID: request.ArtifactID, BaseVersionID: request.BaseVersionID, BaseVersion: request.BaseVersion, ManifestID: request.ManifestID,
		OperationID: operationID, SelectedBlockIDs: selected, Body: execution.Body, Evidence: execution.Evidence,
		ValidateScope: func(validateCtx context.Context) error {
			fresh, validateErr := s.repos.Artifact.EditContext(validateCtx, run.UserID, run.ID)
			if validateErr != nil {
				return validateErr
			}
			if fresh.Request.ArtifactID != request.ArtifactID || fresh.Request.BaseVersionID != request.BaseVersionID || fresh.Request.ManifestID != request.ManifestID || fresh.Request.ToolPolicyJSON != request.ToolPolicyJSON {
				return artifact.Err("target_scope_mismatch", 409)
			}
			return nil
		},
	}
	runID, requestID := run.ID, request.ID
	operation := &model.ArtifactEditOperation{
		ID: operationID, UserID: run.UserID, ArtifactID: request.ArtifactID, RequestID: &requestID, RunID: &runID,
		Kind: model.ArtifactEditOperationKindEdit, BaseVersionID: request.BaseVersionID, BaseVersion: request.BaseVersion,
		ManifestID: request.ManifestID, ScopeJSON: request.SelectedBlockIDsJSON, ToolSchemaDigest: schemaDigest,
	}
	runtime.PersistProposal = func(toolCtx context.Context, proposal ArtifactEditProposal) (ArtifactEditProposal, error) {
		operation.Summary = proposal.Summary
		saved, persistErr := s.repos.Artifact.PersistEditProposal(toolCtx, run.UserID, operation, proposal.Patch, runtime.patchAuthorization(), token, run.RunLeaseEpoch)
		if persistErr != nil {
			return ArtifactEditProposal{}, persistErr
		}
		proposal.OperationID, proposal.Summary, proposal.PatchHash = saved.ID, saved.Summary, saved.PatchHash
		return proposal, nil
	}
	runtime.CommitProposal = func(toolCtx context.Context, proposal ArtifactEditProposal) (ArtifactEditCommitResult, error) {
		operation.Summary = proposal.Summary
		saved, version, commitErr := s.repos.Artifact.CommitEdit(toolCtx, run.UserID, operation, proposal.Patch, runtime.patchAuthorization(), token, run.RunLeaseEpoch, artifactSource)
		if commitErr != nil {
			return ArtifactEditCommitResult{}, commitErr
		}
		return ArtifactEditCommitResult{OperationID: saved.ID, ResultVersionID: version.ID}, nil
	}
	state := ArtifactEditPlannerState{
		ArtifactID: request.ArtifactID, BaseVersionID: request.BaseVersionID, BaseVersion: request.BaseVersion,
		SelectedBlockIDs: selected, Instruction: request.Instruction, Mode: mode, Intent: ClassifyArtifactEditIntent(request.Instruction, mode),
		BaseDigest: digests.Base, ScopeDigest: digests.Scope, ToolSchemaDigest: schemaDigest,
		TermRules: policy.TermRules, TermSnapshotHash: policy.TermSnapshotHash,
	}
	progress := func(progressCtx context.Context, stage string, covered, total int) error {
		return s.repos.Artifact.Progress(progressCtx, run.ID, token, run.RunLeaseEpoch, stage, covered, total)
	}
	runner, err := NewArtifactEditRunner(registry, NewLLMArtifactEditPlanner(client), NewAgentExecutionJournal(s.repos.AgentExecution), run.UserID, run.ID, run.MaxSteps, progress)
	if err != nil {
		return err
	}
	if _, err = runner.Run(ctx, state, runtime); err != nil {
		return err
	}
	return s.repos.Artifact.Finish(ctx, run.ID, token, run.RunLeaseEpoch, model.AgentRunStatusCompleted, "")
}

func (s *ArtifactService) artifactEditClient(owner int64, request *model.ArtifactEditRequest) (ai.ChatClient, error) {
	row, err := s.profiles.repo.FindByIDForUser(owner, request.ProfileID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, artifact.Err("profile_changed", 422)
	}
	decrypted, err := s.profiles.decryptProfile(row)
	if err != nil {
		return nil, artifact.Err("profile_changed", 422)
	}
	profile := providerFromDecrypted(decrypted)
	if profileFingerprint(profile) != request.ProfileFingerprint || strings.TrimSpace(profile.LLMAPIKey) == "" {
		return nil, artifact.Err("profile_changed", 422)
	}
	return s.factory.NewChatClient(*profile)
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func (s *ArtifactService) dispatchArtifactRun(parent context.Context, runID string) (bool, error) {
	_, _, err := s.repos.Artifact.EditRun(parent, 0, runID)
	if err == nil {
		return true, s.ExecuteArtifactEdit(parent, runID)
	}
	if isArtifactNotFound(err) {
		return false, nil
	}
	return true, fmt.Errorf("resolve artifact run kind: %w", err)
}
