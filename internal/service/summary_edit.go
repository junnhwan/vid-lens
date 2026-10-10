package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
	"vid-lens/internal/summaryselection"

	"github.com/google/uuid"
	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
	"vid-lens/internal/summarydoc"
)

const (
	summaryEditRecipeV1       = "summary-edit-v1"
	summaryEditRecipe         = "summary-edit-v2"
	summaryDocumentEditRecipe = "summary-document-edit-v2"
)

type SummaryEditInput struct {
	ExpectedContentDigest string                   `json:"expected_content_digest,omitempty"`
	ExpectedVersionRef    *model.SummaryVersionRef `json:"expected_version_ref,omitempty"`
	SelectedBlockIDs      []string                 `json:"selected_block_ids,omitempty"`
	Instruction           string                   `json:"instruction"`
	ExpectedRevision      int64                    `json:"expected_revision"`
	Mode                  string                   `json:"mode"`
}

type SummaryEditView struct {
	Activities            []SummaryEditActivity  `json:"activities"`
	SelectedBlockIDs      []string               `json:"selected_block_ids,omitempty"`
	Operations            []summarydoc.Operation `json:"operations,omitempty"`
	Preview               *summarydoc.Preview    `json:"preview,omitempty"`
	Document              *summarydoc.Document   `json:"document,omitempty"`
	BaseContentHashKind   string                 `json:"base_content_hash_kind"`
	BaseGeneratedHashKind string                 `json:"base_generated_hash_kind"`
	ID                    string                 `json:"id"`
	RunID                 string                 `json:"run_id"`
	TaskID                int64                  `json:"task_id"`
	Instruction           string                 `json:"instruction"`
	Status                string                 `json:"status"`
	Mode                  string                 `json:"mode"`
	BaseVersion           int64                  `json:"base_version"`
	RuleVersion           int64                  `json:"rule_version"`
	RuleDigest            string                 `json:"rule_digest"`
	Edits                 []SummaryTextEdit      `json:"edits"`
	ResultRevisionID      *string                `json:"result_revision_id,omitempty"`
	UndoRevisionID        *string                `json:"undo_revision_id,omitempty"`
	ErrorCode             string                 `json:"error_code,omitempty"`
}

type SummaryRevisionService struct {
	repos    *repository.Repositories
	profiles *AIProfileService
	factory  *ai.Factory
	chat     ai.ChatClient // deterministic integration seam; production uses the frozen profile
}

func NewSummaryRevisionService(repos *repository.Repositories, profiles *AIProfileService, factory *ai.Factory) *SummaryRevisionService {
	return &SummaryRevisionService{repos: repos, profiles: profiles, factory: factory}
}

func (s *SummaryRevisionService) Effective(ctx context.Context, owner, taskID int64) (*repository.EffectiveSummary, error) {
	return s.repos.SummaryRevision.Effective(ctx, owner, taskID)
}

func (s *SummaryRevisionService) Operation(ctx context.Context, owner, taskID int64, id string) (*SummaryEditView, error) {
	op, err := s.repos.SummaryRevision.Operation(ctx, owner, taskID, id)
	if err != nil {
		return nil, err
	}
	return s.editView(ctx, op)
}

func (s *SummaryRevisionService) LatestOperation(ctx context.Context, owner, taskID int64) (*SummaryEditView, error) {
	op, err := s.repos.SummaryRevision.LatestOperation(ctx, owner, taskID)
	if err != nil || op == nil {
		return nil, err
	}
	return s.editView(ctx, op)
}

func summaryEditView(op *model.SummaryEditOperation) (*SummaryEditView, error) {
	view := &SummaryEditView{ID: op.ID, RunID: op.RunID, TaskID: op.TaskID, Instruction: op.Instruction, Status: op.Status, Mode: op.Mode, BaseVersion: op.BaseVersion, RuleVersion: op.RuleVersion, RuleDigest: op.RuleDigest, ResultRevisionID: op.ResultRevisionID, UndoRevisionID: op.UndoRevisionID, ErrorCode: op.ErrorCode, Edits: []SummaryTextEdit{}}
	view.BaseContentHashKind = op.BaseContentHashKind
	if op.SelectedBlockIDsJSON != "" {
		_ = json.Unmarshal([]byte(op.SelectedBlockIDsJSON), &view.SelectedBlockIDs)
	}
	if view.BaseContentHashKind == "" {
		view.BaseContentHashKind = model.SummaryHashMarkdown
	}
	view.BaseGeneratedHashKind = op.BaseGeneratedHashKind
	if view.BaseGeneratedHashKind == "" {
		view.BaseGeneratedHashKind = model.SummaryHashMarkdown
	}
	if op.PatchJSON != "" && op.PatchJSON != "{}" {
		if op.BaseDocumentJSON != "" {
			patch, err := summarydoc.ParsePatch([]byte(op.PatchJSON))
			if err != nil {
				return nil, err
			}
			view.Operations = patch.Operations
			return view, nil
		}
		var patch SummaryTextPatch
		if err := json.Unmarshal([]byte(op.PatchJSON), &patch); err != nil {
			return nil, err
		}
		view.Edits = patch.Edits
	}
	return view, nil
}

func (s *SummaryRevisionService) Edit(ctx context.Context, owner, taskID int64, key string, input SummaryEditInput) (*SummaryEditView, error) {
	op, err := s.begin(ctx, owner, taskID, key, input)
	if err != nil {
		return nil, err
	}
	if op.Status != "running" {
		return s.editView(ctx, op)
	}
	return s.execute(ctx, op)
}

// Submit durably accepts a request and returns immediately. The summary
// worker owns the provider call; an HTTP disconnect does not cancel the run.
func (s *SummaryRevisionService) Submit(ctx context.Context, owner, taskID int64, key string, input SummaryEditInput) (*SummaryEditView, error) {
	op, err := s.begin(ctx, owner, taskID, key, input)
	if err != nil {
		return nil, err
	}
	return s.editView(ctx, op)
}

func (s *SummaryRevisionService) begin(ctx context.Context, owner, taskID int64, key string, input SummaryEditInput) (*model.SummaryEditOperation, error) {
	input.Instruction = strings.TrimSpace(input.Instruction)
	if len(input.SelectedBlockIDs) > 50 {
		return nil, artifact.Err("invalid_edit_scope", 400)
	}
	sort.Strings(input.SelectedBlockIDs)
	for i, id := range input.SelectedBlockIDs {
		if id == "" || (i > 0 && id == input.SelectedBlockIDs[i-1]) {
			return nil, artifact.Err("invalid_edit_scope", 400)
		}
	}
	if owner <= 0 || taskID <= 0 || input.ExpectedRevision < 0 || input.Instruction == "" || utf8.RuneCountInString(input.Instruction) > 2000 || (input.Mode != "preview" && input.Mode != "apply") {
		return nil, artifact.Err("invalid_request", 400)
	}
	if err := artifact.ValidateKey(key); err != nil {
		return nil, err
	}
	hash := artifact.Hash(artifact.JSON(struct {
		TaskID int64            `json:"task_id"`
		Input  SummaryEditInput `json:"input"`
	}{taskID, input}))
	prior, err := s.repos.SummaryRevision.OperationByKey(ctx, owner, key, hash)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		if prior.TaskID != taskID {
			return nil, artifact.Err("idempotency_conflict", 409)
		}
		return prior, nil
	}
	effective, err := s.Effective(ctx, owner, taskID)
	if err != nil {
		return nil, err
	}
	if len(input.SelectedBlockIDs) > 0 {
		if effective.Document != nil {
			known := map[string]bool{"summary-title": true, "summary-overview": true}
			for _, b := range effective.Document.Blocks {
				known[b.ID] = true
			}
			for _, id := range input.SelectedBlockIDs {
				if !known[id] {
					return nil, artifact.Err("invalid_edit_scope", 400)
				}
			}
		} else {
			if _, err := summaryselection.LegacyRanges(effective.Content, input.SelectedBlockIDs); err != nil {
				return nil, artifact.Err("invalid_edit_scope", 400)
			}
		}
	}
	if err := repository.ValidateSummaryExpectation(effective, repository.SummaryEditExpectation{ContentDigest: input.ExpectedContentDigest, VersionRef: input.ExpectedVersionRef}); err != nil {
		return nil, err
	}
	if effective.Content == "" {
		return nil, artifact.Err("source_not_ready", 422)
	}
	if effective.Version != input.ExpectedRevision {
		return nil, artifact.Err("version_conflict", 409)
	}
	if effective.Document == nil && utf8.RuneCountInString(effective.Content) > 48000 {
		return nil, artifact.Err("source_limit_exceeded", 422)
	}
	rules, err := EffectiveTermRules(ctx, s.repos, owner, taskID)
	if err != nil {
		return nil, err
	}
	rulesJSON := artifact.JSON(rules)
	resolved, err := s.profiles.GetDefaultConversationProfile(owner)
	if err != nil || resolved == nil || resolved.Profile == nil || strings.TrimSpace(resolved.Profile.LLMAPIKey) == "" {
		return nil, artifact.Err("profile_required", 422)
	}
	profile := resolved.Profile
	now, runID, opID := time.Now().UTC(), uuid.NewString(), uuid.NewString()
	budget := resolved.EffectiveAgentBudget.Values
	recipe := summaryEditRecipe
	if effective.Document != nil {
		recipe = summaryDocumentEditRecipe
	}
	run := &model.AgentRun{ID: runID, UserID: owner, SubjectKind: model.AgentRunSubjectSummaryEdit, SubjectID: opID, ExecutionKind: "artifact", RecipeVersion: summaryEditRecipe, ScopeType: "video", TaskID: taskID, Goal: input.Instruction, Mode: input.Mode, AgentProfile: "default", ProfileSnapshot: artifact.JSON(map[string]any{"profile_id": profile.ID, "model": profile.LLMModel, "fingerprint": profileFingerprint(profile)}), PolicySnapshot: artifact.JSON(map[string]any{"recipe": summaryEditRecipe, "base_hash": artifact.Hash(effective.Content), "rule_version": rules.Version, "rule_digest": rules.Digest}), BudgetSnapshot: artifact.JSON(resolved.EffectiveAgentBudget), Status: model.AgentRunStatusPending, Stage: "queued", Version: 1, MaxSteps: 2, MaxLLMCalls: 2, MaxAttemptsPerStep: 2, MaxPromptTokens: int64(budget.MaxInputTokens), MaxCompletionTokens: int64(budget.MaxOutputTokens), MaxDurationMs: int64(budget.MaxDurationSeconds) * 1000, MaxContextChars: int64(profile.LLMContextTokens), CreatedAt: now, UpdatedAt: now}
	op := &model.SummaryEditOperation{ID: opID, UserID: owner, TaskID: taskID, Key: key, RequestHash: hash, RunID: runID, Mode: input.Mode, Status: "running", Instruction: input.Instruction, BaseVersion: effective.Version, BaseContentHash: artifact.Hash(effective.Content), BaseContent: effective.Content, BaseGeneratedHash: effective.BaseHash, RuleVersion: rules.Version, RuleDigest: rules.Digest, RuleSnapshotJSON: rulesJSON, ProfileID: profile.ID, ProfileFingerprint: profileFingerprint(profile), PatchJSON: "{}", CreatedAt: now, UpdatedAt: now}
	op.SelectedBlockIDsJSON = artifact.JSON(input.SelectedBlockIDs)
	op.BaseContentHash = effective.ContentDigest
	op.BaseContentHashKind = effective.ContentHashKind
	op.BaseGeneratedHashKind = effective.BaseHashKind
	op.BaseDocumentJSON = effective.DocumentJSON
	op.BaseGenerationID = effective.GenerationID
	if effective.Document != nil {
		op.BaseSourceID = effective.Document.SourceID
		op.BaseSourceDigest = effective.Document.SourceDigest
	}
	run.RecipeVersion = recipe
	run.PolicySnapshot = artifact.JSON(map[string]any{"recipe": recipe, "base_hash": op.BaseContentHash, "base_hash_kind": op.BaseContentHashKind, "selected_block_ids": input.SelectedBlockIDs, "rule_version": rules.Version, "rule_digest": rules.Digest})
	op, err = s.repos.SummaryRevision.Begin(ctx, op, run, repository.SummaryEditExpectation{ContentDigest: input.ExpectedContentDigest, VersionRef: input.ExpectedVersionRef})
	if err != nil {
		return nil, err
	}
	return op, nil
}

func (s *SummaryRevisionService) execute(ctx context.Context, op *model.SummaryEditOperation, leaseToken ...string) (*SummaryEditView, error) {
	token := ""
	if len(leaseToken) > 0 {
		token = leaseToken[0]
	}
	current, err := s.Effective(ctx, op.UserID, op.TaskID)
	if err != nil {
		return nil, err
	}
	if current.Version != op.BaseVersion || current.ContentDigest != op.BaseContentHash || current.ContentHashKind != normalizedSummaryHashKind(op.BaseContentHashKind) {
		return nil, artifact.Err("version_conflict", 409)
	}
	var rules VideoTermRuleSet
	if err = json.Unmarshal([]byte(op.RuleSnapshotJSON), &rules); err != nil || rules.Version != op.RuleVersion || rules.Digest != op.RuleDigest {
		return nil, artifact.Err("unsupported_checkpoint", 409)
	}
	client := s.chat
	if client == nil {
		row, profileErr := s.profiles.repo.FindByIDForUser(op.UserID, op.ProfileID)
		if profileErr != nil || row == nil {
			return nil, artifact.Err("profile_changed", 422)
		}
		decrypted, profileErr := s.profiles.decryptProfile(row)
		if profileErr != nil {
			return nil, artifact.Err("profile_changed", 422)
		}
		profile := providerFromDecrypted(decrypted)
		if profileFingerprint(profile) != op.ProfileFingerprint {
			return nil, artifact.Err("profile_changed", 422)
		}
		client, err = s.factory.NewChatClient(*profile)
		if err != nil {
			return nil, err
		}
	}
	if op.BaseDocumentJSON != "" {
		return s.executeDocumentEdit(ctx, op, rules, client, token)
	}
	patch, err := s.planSummaryPatch(ctx, op, rules, client)
	if err != nil {
		code := "provider_error"
		var domain *artifact.Error
		if errors.As(err, &domain) {
			code = domain.Code
		}
		if ctx.Err() == nil {
			_ = s.repos.SummaryRevision.Fail(context.WithoutCancel(ctx), op.ID, code, token)
		}
		return nil, err
	}
	content, err := applySummaryTextPatch(op.BaseContent, patch)
	if err != nil {
		return nil, err
	}
	patchJSON := artifact.JSON(patch)
	if op.Mode == "preview" {
		if err = s.repos.SummaryRevision.Propose(ctx, op.ID, patchJSON, token); err != nil {
			return nil, err
		}
	} else {
		if _, err = s.repos.SummaryRevision.Commit(ctx, op.ID, content, patchJSON, token); err != nil {
			return nil, err
		}
	}
	return s.Operation(ctx, op.UserID, op.TaskID, op.ID)
}

// ExecuteSummaryEdit is the worker entry point. The operation is already
// durable; lease fencing prevents a stale worker from publishing after a
// replacement worker or source deletion has won the run.
func (s *SummaryRevisionService) ExecuteSummaryEdit(parent context.Context, runID string) error {
	token := uuid.NewString()
	op, claimed, err := s.repos.SummaryRevision.ClaimRun(parent, runID, token, 2*time.Minute)
	if err != nil || !claimed {
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				alive, renewErr := s.repos.SummaryRevision.RenewRun(ctx, runID, token, 2*time.Minute)
				if renewErr != nil || !alive {
					cancel()
					return
				}
			}
		}
	}()
	_, err = s.execute(ctx, op, token)
	interrupted := ctx.Err() != nil
	close(done)
	cancel()
	cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cleanupCancel()
	if err != nil && !interrupted {
		code := "execution_failed"
		var domain *artifact.Error
		if errors.As(err, &domain) {
			code = domain.Code
		}
		_ = s.repos.SummaryRevision.Fail(cleanupCtx, op.ID, code, token)
	}
	if releaseErr := s.repos.SummaryRevision.ReleaseRun(cleanupCtx, runID, token); releaseErr != nil && err == nil {
		err = releaseErr
	}
	return err
}

func decodeSummaryPatch(raw string, patch *SummaryTextPatch) error {
	if err := decodeSummaryJSON(raw, patch); err != nil {
		return err
	}
	for _, edit := range patch.Edits {
		if edit.Start != nil {
			return artifact.Err("invalid_patch", 422)
		}
	}
	return nil
}

func decodeSummaryJSON(raw string, target any) error {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") && strings.HasSuffix(raw, "```") {
		firstLine := strings.IndexByte(raw, '\n')
		if firstLine < 0 || (raw[:firstLine] != "```" && raw[:firstLine] != "```json") {
			return artifact.Err("invalid_patch", 422)
		}
		raw = strings.TrimSpace(raw[firstLine+1 : len(raw)-3])
	}
	return artifact.Decode([]byte(raw), target)
}

func (s *SummaryRevisionService) Apply(ctx context.Context, owner, taskID int64, id string, expected int64) (*SummaryEditView, error) {
	op, err := s.repos.SummaryRevision.Operation(ctx, owner, taskID, id)
	if err != nil {
		return nil, err
	}
	if op.Status == "committed" {
		return s.editView(ctx, op)
	}
	if op.Status != "proposed" || op.BaseVersion != expected {
		return nil, artifact.Err("version_conflict", 409)
	}
	if op.BaseDocumentJSON != "" {
		return s.applyDocumentEdit(ctx, op)
	}
	var patch SummaryTextPatch
	if err = json.Unmarshal([]byte(op.PatchJSON), &patch); err != nil {
		return nil, err
	}
	content, err := applySummaryTextPatch(op.BaseContent, patch)
	if err != nil {
		return nil, err
	}
	if _, err = s.repos.SummaryRevision.Commit(ctx, op.ID, content, op.PatchJSON); err != nil {
		return nil, err
	}
	return s.Operation(ctx, owner, taskID, id)
}

func (s *SummaryRevisionService) Undo(ctx context.Context, owner, taskID int64, id string, expected int64) (*SummaryEditView, error) {
	op, err := s.repos.SummaryRevision.Operation(ctx, owner, taskID, id)
	if err != nil {
		return nil, err
	}
	if op.UndoRevisionID != nil {
		return s.editView(ctx, op)
	}
	if op.ResultRevisionID == nil {
		return nil, artifact.Err("undo_conflict", 409)
	}
	if op.BaseDocumentJSON != "" {
		current, err := s.Effective(ctx, owner, taskID)
		if err != nil {
			return nil, err
		}
		if current.Version != expected {
			return nil, artifact.Err("version_conflict", 409)
		}
		if current.Revision == nil || current.Revision.ID != *op.ResultRevisionID {
			return nil, artifact.Err("undo_conflict", 409)
		}
		if _, err = s.repos.SummaryRevision.Undo(ctx, owner, taskID, id, expected, current.ContentDigest, op.BaseContent); err != nil {
			return nil, err
		}
		return s.Operation(ctx, owner, taskID, id)
	}
	var patch SummaryTextPatch
	if err = json.Unmarshal([]byte(op.PatchJSON), &patch); err != nil {
		return nil, err
	}
	result, err := applySummaryTextPatch(op.BaseContent, patch)
	if err != nil {
		return nil, err
	}
	current, err := s.Effective(ctx, owner, taskID)
	if err != nil {
		return nil, err
	}
	if current.Version != expected {
		return nil, artifact.Err("version_conflict", 409)
	}
	reversed, err := reverseSummaryTextPatch(current.Content, result, op.BaseContent, patch)
	if err != nil {
		return nil, err
	}
	if _, err = s.repos.SummaryRevision.Undo(ctx, owner, taskID, id, expected, artifact.Hash(current.Content), reversed); err != nil {
		return nil, err
	}
	return s.Operation(ctx, owner, taskID, id)
}

func (s *SummaryRevisionService) ResolveBase(ctx context.Context, owner, taskID, expected int64, choice string) (*repository.EffectiveSummary, error) {
	if _, err := s.repos.SummaryRevision.ResolveBase(ctx, owner, taskID, expected, choice); err != nil {
		return nil, err
	}
	return s.Effective(ctx, owner, taskID)
}
