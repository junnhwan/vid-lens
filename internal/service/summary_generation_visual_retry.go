package service

import (
	"context"
	"time"
	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
	"vid-lens/internal/repository"
	"vid-lens/internal/summarydoc"
)

func (s *SummaryGenerationService) generateVisualRetry(ctx context.Context, task *model.VideoTask, job *model.TaskJob, frozen processing.GenerationSnapshot, token string) error {
	retry := frozen.VisualRetry
	intent, err := processing.Decode(artifact.JSON(frozen.Intent))
	if err != nil || retry == nil || !retry.NewBudgetAuthorized || retry.ParentGenerationID == "" || retry.ParentGenerationID == intent.GenerationID || !intent.Options.SummaryVisualEnabled || job.GenerationID != intent.GenerationID || job.TaskID != task.ID || job.UserID != task.UserID || job.InputSourceID != frozen.SourceID {
		return artifact.Err("invalid_generation_snapshot", 409)
	}
	lease := repository.SummaryGenerationLease{UserID: task.UserID, TaskID: task.ID, GenerationID: job.GenerationID, SourceID: frozen.SourceID, SourceDigest: frozen.SourceDigest, LeaseToken: token}
	if err = s.repos.WithSummaryGenerationLease(ctx, lease, func(*repository.Repositories) error { return nil }); err != nil {
		return err
	}
	store := repository.NewSummaryGenerationExecutionStore(s.repos, task.UserID, task.ID, job.GenerationID)
	priorRun, err := store.GetRun(ctx, task.UserID, job.GenerationID)
	if err != nil {
		return err
	}
	if priorRun != nil && priorRun.Status == model.AgentRunStatusCompleted {
		return nil
	}
	source, err := s.repos.TextSource.Read(ctx, task.UserID, task.ID, frozen.SourceID)
	if err != nil {
		return err
	}
	if source.SourceDigest != frozen.SourceDigest || source.Identity.MediaFingerprint != task.FileMD5 || source.CanonicalText != job.InputText {
		return artifact.Err("generation_source_changed", 409)
	}
	doc, err := summarydoc.Parse([]byte(retry.BaseDocumentJSON))
	if err != nil {
		return artifact.Err("invalid_generation_checkpoint", 409)
	}
	digest, err := summarydoc.Digest(doc)
	if err != nil || digest != frozen.ExpectedGeneratedHash || doc.PresentationMode != "text" || doc.SourceID != frozen.SourceID || doc.SourceDigest != frozen.SourceDigest || doc.MediaRevision != task.FileMD5 {
		return artifact.Err("invalid_generation_checkpoint", 409)
	}
	validation, err := s.repos.SummaryValidationContext(ctx, task.UserID, task.ID, frozen.SourceID, frozen.SourceDigest, retry.ParentGenerationID)
	if err != nil {
		return err
	}
	if err = summarydoc.Validate(doc, validation); err != nil {
		return artifact.Err("invalid_summary_document", 422)
	}
	base, err := s.repos.Summary.FindByTaskID(task.ID)
	if err != nil {
		return err
	}
	if base == nil || base.SourceID != frozen.SourceID || base.SourceDigest != frozen.SourceDigest || (base.GenerationID != job.GenerationID && (base.GenerationID != retry.ParentGenerationID || base.GeneratedVersion != frozen.ExpectedGeneratedVersion || base.ContentDigest != frozen.ExpectedGeneratedHash)) {
		return artifact.Err("generation_stale", 409)
	}
	profile, err := s.profiles.GetAIProfileByID(task.UserID, intent.ProfileID)
	if err != nil || profile == nil {
		return artifact.Err("frozen_profile_unavailable", 422)
	}
	if profile.ID != intent.ProfileID || processing.FingerprintProfile(*profile) != intent.ProfileFingerprint {
		return artifact.Err("frozen_profile_changed", 409)
	}
	if err = ai.RequireAction(*profile, "summary"); err != nil {
		return artifact.Err("summary_profile_required", 422)
	}
	var budget config.ResolvedAgentBudget
	if artifact.Decode([]byte(intent.BudgetJSON), &budget) != nil || budget.PolicyVersion != 1 || budget.Values.MaxToolCalls <= 0 || budget.Values.MaxInputTokens <= 0 || budget.Values.MaxOutputTokens <= 0 || budget.Values.MaxDurationSeconds <= 0 {
		return artifact.Err("invalid_generation_budget", 409)
	}
	var policy struct {
		Recipe  string             `json:"recipe"`
		Options processing.Options `json:"options"`
	}
	if artifact.Decode([]byte(intent.PolicyJSON), &policy) != nil || policy.Recipe != processing.Recipe || policy.Options != intent.Options {
		return artifact.Err("invalid_generation_policy", 409)
	}
	values := budget.Values
	visualLimit := 8
	if values.MaxVisualFrames != nil {
		visualLimit = max(0, min(8, *values.MaxVisualFrames))
	}
	run := &model.AgentRun{ID: job.GenerationID, UserID: task.UserID, TaskID: task.ID, SubjectKind: model.AgentRunSubjectSummaryGeneration, SubjectID: job.GenerationID, ExecutionKind: "artifact", RecipeVersion: processing.Recipe, ScopeType: model.ChatScopeVideo, Goal: "仅补充已保存摘要的画面", Mode: intent.Options.OutputMode, AgentProfile: "summary", ProfileSnapshot: artifact.JSON(map[string]any{"profile_id": profile.ID, "fingerprint": intent.ProfileFingerprint}), PolicySnapshot: artifact.JSON(map[string]any{"recipe": processing.Recipe, "options": intent.Options, "source_id": source.ID, "source_digest": source.SourceDigest, "operation": processing.OperationVisualRetry, "parent_generation_id": retry.ParentGenerationID, "new_budget_authorized": true, "previous_spend": retry.PreviousSpendJSON}), BudgetSnapshot: intent.BudgetJSON, Status: model.AgentRunStatusRunning, Stage: "visual_retry", MaxSteps: values.MaxToolCalls*2 + 2, MaxToolCalls: values.MaxToolCalls, MaxLLMCalls: values.MaxToolCalls * 2, MaxVisionCalls: visualLimit, MaxFrames: visualLimit, MaxVisualCalls: 3, MaxAttemptsPerStep: 2, MaxPromptTokens: int64(values.MaxInputTokens), MaxCompletionTokens: int64(values.MaxOutputTokens), MaxDurationMs: int64(values.MaxDurationSeconds) * 1000, MaxContextChars: int64(values.MaxInputTokens) * 8}
	run, err = s.repos.StartSummaryGeneration(ctx, lease, run)
	if err != nil {
		return err
	}
	remaining := time.Duration(run.MaxDurationMs)*time.Millisecond - time.Since(run.CreatedAt)
	if remaining <= 0 {
		return artifact.Err("duration_limit", 422)
	}
	ctx, cancel := context.WithTimeout(ctx, remaining)
	defer cancel()
	if err = s.repos.MarkSummaryVisualTextReused(ctx, lease, frozen); err != nil {
		return err
	}
	state, reason := "skipped", "visual_enricher_unavailable"
	if s.visual != nil {
		if err = s.repos.BeginSummaryVisualEnrichment(ctx, lease); err != nil {
			return err
		}
		// Enrichment receives the immutable accepted base. Its publication CAS
		// still targets the parent version; no text seed is written to AISummary.
		frozenBase := *base
		frozenBase.GenerationID = job.GenerationID
		frozenBase.DocumentJSON = retry.BaseDocumentJSON
		frozenBase.GeneratedVersion = frozen.ExpectedGeneratedVersion
		frozenBase.ContentDigest = frozen.ExpectedGeneratedHash
		frozenBase.ContentHashKind = frozen.ExpectedGeneratedHashKind
		visualErr := s.visual.Enrich(ctx, task, job, frozen, *profile, source, &frozenBase, token)
		if err = s.repos.WithSummaryGenerationLease(ctx, lease, func(*repository.Repositories) error { return nil }); err != nil {
			return err
		}
		if visualErr != nil {
			visualErr = summaryGenerationVisualReferenceFailure(visualErr, source, &frozenBase)
			state, reason = summaryVisualFailure(visualErr)
		} else {
			latest, findErr := s.repos.Summary.FindByTaskID(task.ID)
			if findErr != nil {
				return findErr
			}
			if latest != nil && latest.GenerationID == job.GenerationID {
				published, parseErr := summarydoc.Parse([]byte(latest.DocumentJSON))
				if parseErr != nil {
					return parseErr
				}
				if len(published.Blocks) > 0 && published.PresentationMode != "text" {
					state, reason = "complete", ""
				} else {
					state, reason = "skipped", "no_useful_visual"
				}
			} else {
				state, reason = "skipped", "no_useful_visual"
			}
		}
	}
	return s.repos.CompleteSummaryVisualRetry(ctx, lease, frozen, state, reason)
}
