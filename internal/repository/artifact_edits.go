package repository

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

// ArtifactEditContext is the frozen, owner-checked input surface for an edit
// runner. Body and Evidence come only from the request's immutable manifest and
// base version; callers cannot use it to select a different target.
type ArtifactEditContext struct {
	Run         *model.AgentRun
	Request     *model.ArtifactEditRequest
	Artifact    *model.Artifact
	BaseVersion *model.ArtifactVersion
	Manifest    *model.SourceManifest
	Body        artifact.Body
	Evidence    []model.SourceSnapshotItem
}

func editRequestByKey(tx *gorm.DB, owner int64, key, hash string) (*model.ArtifactEditRequest, error) {
	var req model.ArtifactEditRequest
	err := tx.Where("user_id=? AND idempotency_key=?", owner, key).First(&req).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if req.RequestHash != hash {
		return nil, artifact.Err("idempotency_conflict", 409)
	}
	return &req, nil
}

func (r *ArtifactRepository) EditByKey(ctx context.Context, owner int64, key, hash string) (*model.ArtifactEditRequest, error) {
	return editRequestByKey(r.db.WithContext(ctx), owner, key, hash)
}

// SubmitEdit atomically freezes an edit request and queues its run. Request
// idempotency is resolved before mutable target checks, so an accepted request
// remains replayable after the head or source changes.
func (r *ArtifactRepository) SubmitEdit(ctx context.Context, req *model.ArtifactEditRequest, run *model.AgentRun) (*model.ArtifactEditRequest, error) {
	if req == nil || run == nil || req.UserID <= 0 || req.ID == "" || req.RunID == "" || req.IdempotencyKey == "" || req.RequestHash == "" || req.BaseVersion <= 0 || req.BaseVersionID == "" || req.ArtifactID == "" || req.ManifestID == "" || run.ID != req.RunID || run.UserID != req.UserID {
		return nil, artifact.Err("invalid_request", 400)
	}
	if prior, err := editRequestByKey(r.db.WithContext(ctx), req.UserID, req.IdempotencyKey, req.RequestHash); err != nil || prior != nil {
		return prior, err
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		prior, err := editRequestByKey(tx, req.UserID, req.IdempotencyKey, req.RequestHash)
		if err != nil {
			return err
		}
		if prior != nil {
			*req = *prior
			return nil
		}
		m, err := readableManifest(tx, req.UserID, req.ManifestID)
		if err != nil {
			return err
		}
		if _, err = sourceLock(tx, req.UserID, m.SourceID); err != nil {
			return err
		}
		if _, err = readableManifest(tx, req.UserID, req.ManifestID); err != nil {
			return err
		}
		a, err := ownedArtifact(tx, req.UserID, req.ArtifactID, true)
		if err != nil {
			return err
		}
		if a.HeadVersion != req.BaseVersion || a.CurrentVersionID == nil || *a.CurrentVersionID != req.BaseVersionID {
			return artifact.Err("version_conflict", 409)
		}
		var base model.ArtifactVersion
		if err = tx.Where("id=? AND artifact_id=? AND version=? AND manifest_id=?", req.BaseVersionID, req.ArtifactID, req.BaseVersion, req.ManifestID).First(&base).Error; err != nil {
			return hideMissing(err)
		}
		if err = tx.Create(req).Error; err != nil {
			return err
		}
		run.SubjectKind = model.AgentRunSubjectArtifactEdit
		run.SubjectID = req.ID
		run.ExecutionKind = "artifact"
		run.RecipeVersion = req.Recipe
		if err = tx.Create(run).Error; err != nil {
			return err
		}
		if err = appendEvent(tx, run, "run.created", map[string]any{"status": model.AgentRunStatusPending, "stage": "queued"}); err != nil {
			return err
		}
		return tx.Create(&model.ArtifactEditDispatch{ID: uuid.NewString(), RunID: run.ID, NextAttemptAt: req.CreatedAt}).Error
	})
	if err != nil {
		prior, replayErr := editRequestByKey(r.db.WithContext(ctx), req.UserID, req.IdempotencyKey, req.RequestHash)
		if replayErr != nil {
			return nil, replayErr
		}
		if prior != nil {
			return prior, nil
		}
		return nil, err
	}
	return req, nil
}

func loadEditRun(tx *gorm.DB, owner int64, id string) (*model.AgentRun, *model.ArtifactEditRequest, error) {
	var run model.AgentRun
	q := tx.Where("id=? AND subject_kind=?", id, model.AgentRunSubjectArtifactEdit)
	if owner > 0 {
		q = q.Where("user_id=?", owner)
	}
	if err := q.First(&run).Error; err != nil {
		return nil, nil, hideMissing(err)
	}
	var req model.ArtifactEditRequest
	if err := tx.Where("run_id=? AND user_id=?", id, run.UserID).First(&req).Error; err != nil {
		return nil, nil, hideMissing(err)
	}
	return &run, &req, nil
}

// EditRun intentionally exposes the immutable worker/recovery record even when
// its source has since been revoked. Public projections must use ReadableEditRun.
func (r *ArtifactRepository) EditRun(ctx context.Context, owner int64, id string) (*model.AgentRun, *model.ArtifactEditRequest, error) {
	return loadEditRun(r.db.WithContext(ctx), owner, id)
}

// ReadableEditRun applies the frozen request manifest's source gate before any
// source-bound instruction, result, or event metadata is returned publicly.
func (r *ArtifactRepository) ReadableEditRun(ctx context.Context, owner int64, id string) (*model.AgentRun, *model.ArtifactEditRequest, error) {
	var run *model.AgentRun
	var request *model.ArtifactEditRequest
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		run, request, err = loadEditRun(tx, owner, id)
		if err != nil {
			return err
		}
		_, err = readableManifest(tx, owner, request.ManifestID)
		return err
	})
	return run, request, err
}

func (r *ArtifactRepository) LatestEditRunIDs(ctx context.Context, owner int64, artifactIDs []string) (map[string]string, error) {
	out := make(map[string]string, len(artifactIDs))
	if len(artifactIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		ArtifactID string
		RunID      string
	}
	err := r.db.WithContext(ctx).Table("artifact_edit_requests AS er").
		Select("er.artifact_id, er.run_id").
		Joins("JOIN agent_runs AS ar ON ar.id=er.run_id AND ar.user_id=er.user_id AND ar.subject_kind=?", model.AgentRunSubjectArtifactEdit).
		Joins("JOIN artifacts AS a ON a.id=er.artifact_id AND a.user_id=er.user_id").
		Where("er.user_id=? AND er.artifact_id IN ?", owner, artifactIDs).
		Order("ar.created_at DESC, ar.id DESC").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if _, ok := out[row.ArtifactID]; !ok {
			out[row.ArtifactID] = row.RunID
		}
	}
	return out, nil
}

func (r *ArtifactRepository) EditContext(ctx context.Context, owner int64, runID string) (*ArtifactEditContext, error) {
	out := &ArtifactEditContext{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run model.AgentRun
		q := tx.Where("id=? AND subject_kind=?", runID, model.AgentRunSubjectArtifactEdit)
		if owner > 0 {
			q = q.Where("user_id=?", owner)
		}
		if err := q.First(&run).Error; err != nil {
			return hideMissing(err)
		}
		var req model.ArtifactEditRequest
		if err := tx.Where("run_id=? AND user_id=?", runID, run.UserID).First(&req).Error; err != nil {
			return hideMissing(err)
		}
		a, err := ownedArtifact(tx, run.UserID, req.ArtifactID, false)
		if err != nil {
			return err
		}
		m, err := readableManifest(tx, run.UserID, req.ManifestID)
		if err != nil {
			return err
		}
		var base model.ArtifactVersion
		if err = tx.Where("id=? AND artifact_id=? AND version=? AND manifest_id=?", req.BaseVersionID, req.ArtifactID, req.BaseVersion, req.ManifestID).First(&base).Error; err != nil {
			return hideMissing(err)
		}
		var body artifact.Body
		if err = json.Unmarshal([]byte(base.BodyJSON), &body); err != nil {
			return err
		}
		var evidence []model.SourceSnapshotItem
		if err = tx.Where("manifest_id=?", req.ManifestID).Order("position").Find(&evidence).Error; err != nil {
			return err
		}
		out = &ArtifactEditContext{Run: &run, Request: &req, Artifact: a, BaseVersion: &base, Manifest: m, Body: body, Evidence: evidence}
		return nil
	})
	return out, err
}

func editOperationByID(tx *gorm.DB, owner int64, id string, lock bool) (*model.ArtifactEditOperation, error) {
	var operation model.ArtifactEditOperation
	if lock {
		tx = tx.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := tx.Where("id=? AND user_id=?", id, owner).First(&operation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &operation, err
}

func canonicalEditAuthorization(operationID string, req *model.ArtifactEditRequest, allowed map[string]bool) (artifact.PatchAuthorization, error) {
	var selected []string
	if err := json.Unmarshal([]byte(req.SelectedBlockIDsJSON), &selected); err != nil {
		return artifact.PatchAuthorization{}, err
	}
	return artifact.PatchAuthorization{OperationID: operationID, ArtifactID: req.ArtifactID, BaseVersionID: req.BaseVersionID, BaseVersion: req.BaseVersion, SelectedBlockIDs: selected, AllowedEvidenceIDs: allowed}, nil
}

func validateEditOperationBinding(operation *model.ArtifactEditOperation, req *model.ArtifactEditRequest, patch artifact.Patch, supplied, server artifact.PatchAuthorization) error {
	if operation == nil || operation.ID == "" || operation.UserID != req.UserID || operation.ArtifactID != req.ArtifactID || operation.RunID == nil || *operation.RunID != req.RunID || operation.RequestID == nil || *operation.RequestID != req.ID || operation.BaseVersionID != req.BaseVersionID || operation.BaseVersion != req.BaseVersion || operation.ManifestID != req.ManifestID || supplied.OperationID != server.OperationID || supplied.ArtifactID != server.ArtifactID || supplied.BaseVersionID != server.BaseVersionID || supplied.BaseVersion != server.BaseVersion || !slices.Equal(supplied.SelectedBlockIDs, server.SelectedBlockIDs) || patch.ArtifactID != req.ArtifactID || patch.BaseVersionID != req.BaseVersionID || patch.BaseVersion != req.BaseVersion {
		return artifact.Err("target_scope_mismatch", 409)
	}
	return nil
}

func loadEditBase(tx *gorm.DB, req *model.ArtifactEditRequest) (*model.ArtifactVersion, artifact.Body, map[string]bool, error) {
	var base model.ArtifactVersion
	if err := tx.Where("id=? AND artifact_id=? AND version=? AND manifest_id=?", req.BaseVersionID, req.ArtifactID, req.BaseVersion, req.ManifestID).First(&base).Error; err != nil {
		return nil, artifact.Body{}, nil, hideMissing(err)
	}
	var body artifact.Body
	if err := json.Unmarshal([]byte(base.BodyJSON), &body); err != nil {
		return nil, artifact.Body{}, nil, err
	}
	allowed, err := allowedEvidence(tx, req.ManifestID)
	return &base, body, allowed, err
}

func populateEditOperation(operation *model.ArtifactEditOperation, req *model.ArtifactEditRequest, canonical, hash string, auth artifact.PatchAuthorization, patch artifact.Patch, result artifact.PatchResult, status string) {
	now := time.Now().UTC()
	operation.CanonicalPatchJSON = canonical
	operation.PatchHash = hash
	operation.AuthorizationJSON = artifact.JSON(map[string]any{"operation_id": auth.OperationID, "artifact_id": auth.ArtifactID, "base_version_id": auth.BaseVersionID, "base_version": auth.BaseVersion, "selected_block_ids": auth.SelectedBlockIDs})
	operation.ScopeJSON = artifact.JSON(auth.SelectedBlockIDs)
	operation.Basis = patch.Basis
	operation.EvidenceIDsJSON = artifact.JSON(patch.EvidenceIDs)
	operation.Status = status
	operation.CountsJSON = artifact.JSON(result.Diff.Counts)
	operation.ChangesJSON = artifact.JSON(result.Diff.Changes)
	operation.BlockMappingsJSON = artifact.JSON(result.Diff.BlockMappings)
	if operation.Kind == "" {
		operation.Kind = model.ArtifactEditOperationKindEdit
	}
	if operation.CreatedAt.IsZero() {
		operation.CreatedAt = now
	}
	operation.UpdatedAt = now
	if operation.Summary == "" {
		operation.Summary = "已生成受约束修改"
	}
	if operation.ToolSchemaDigest == "" {
		operation.ToolSchemaDigest = artifact.Hash(req.ToolPolicyJSON)
	}
}

// PersistEditProposal validates a proposal against the frozen base/scope and
// saves its deterministic operation identity without advancing the head.
func (r *ArtifactRepository) PersistEditProposal(ctx context.Context, owner int64, operation *model.ArtifactEditOperation, patch artifact.Patch, supplied artifact.PatchAuthorization, token string, epoch int64) (*model.ArtifactEditOperation, error) {
	canonical, hash, err := artifact.CanonicalPatch(patch)
	if err != nil {
		return nil, err
	}
	if operation == nil || operation.ID == "" {
		return nil, artifact.Err("invalid_request", 400)
	}
	if prior, err := editOperationByID(r.db.WithContext(ctx), owner, operation.ID, false); err != nil {
		return nil, err
	} else if prior != nil {
		if prior.PatchHash != hash {
			return nil, artifact.Err("idempotency_conflict", 409)
		}
		return prior, nil
	}
	var saved *model.ArtifactEditOperation
	cancelled := false
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var req model.ArtifactEditRequest
		if operation.RunID == nil {
			return artifact.Err("not_found", 404)
		}
		if err := tx.Where("run_id=? AND user_id=?", *operation.RunID, owner).First(&req).Error; err != nil {
			return hideMissing(err)
		}
		if req.Mode != "preview" && req.Mode != "apply" {
			return artifact.Err("target_scope_mismatch", 409)
		}
		m, err := readableManifest(tx, owner, req.ManifestID)
		if err != nil {
			return err
		}
		if _, err = sourceLock(tx, owner, m.SourceID); err != nil {
			return err
		}
		if _, err = readableManifest(tx, owner, req.ManifestID); err != nil {
			return err
		}
		if _, err = ownedArtifact(tx, owner, req.ArtifactID, true); err != nil {
			return err
		}
		run, err := lockedRun(tx, req.RunID)
		if err != nil {
			return err
		}
		prior, err := editOperationByID(tx, owner, operation.ID, true)
		if err != nil {
			return err
		}
		if prior != nil {
			if prior.PatchHash != hash {
				return artifact.Err("idempotency_conflict", 409)
			}
			saved = prior
			return nil
		}
		if err = fence(run, token, epoch, time.Now().UTC()); err != nil {
			return err
		}
		if run.CancelRequestedAt != nil {
			cancelled = true
			return terminal(tx, run, model.AgentRunStatusCancelled, "")
		}
		_, baseBody, allowed, err := loadEditBase(tx, &req)
		if err != nil {
			return err
		}
		serverAuth, err := canonicalEditAuthorization(operation.ID, &req, allowed)
		if err != nil {
			return err
		}
		if err = validateEditOperationBinding(operation, &req, patch, supplied, serverAuth); err != nil {
			return err
		}
		result, err := artifact.EditPatch(baseBody, patch, serverAuth)
		if err != nil {
			return err
		}
		candidate := *operation
		populateEditOperation(&candidate, &req, canonical, hash, serverAuth, patch, result, model.ArtifactEditOperationProposed)
		if err = tx.Create(&candidate).Error; err != nil {
			return err
		}
		saved = &candidate
		return nil
	})
	if err != nil {
		if prior, replayErr := editOperationByID(r.db.WithContext(ctx), owner, operation.ID, false); replayErr == nil && prior != nil {
			if prior.PatchHash == hash {
				return prior, nil
			}
			return nil, artifact.Err("idempotency_conflict", 409)
		}
		return nil, err
	}
	if cancelled {
		return nil, artifact.ErrLease
	}
	return saved, nil
}

func committedEditReplay(tx *gorm.DB, owner int64, operationID, patchHash string) (*model.ArtifactEditOperation, *model.ArtifactVersion, error) {
	op, err := editOperationByID(tx, owner, operationID, false)
	if err != nil || op == nil {
		return op, nil, err
	}
	if op.PatchHash != patchHash {
		return nil, nil, artifact.Err("idempotency_conflict", 409)
	}
	if op.Status != model.ArtifactEditOperationCommitted || op.ResultVersionID == nil {
		return op, nil, nil
	}
	var version model.ArtifactVersion
	if err = tx.Where("id=? AND artifact_id=?", *op.ResultVersionID, op.ArtifactID).First(&version).Error; err != nil {
		return nil, nil, err
	}
	return op, &version, nil
}

// CommitEdit publishes a validated patch as one immutable Agent version. A
// committed operation replay is returned before lease/head/source checks.
func (r *ArtifactRepository) CommitEdit(ctx context.Context, owner int64, operation *model.ArtifactEditOperation, patch artifact.Patch, supplied artifact.PatchAuthorization, token string, epoch int64, read SourceReader) (*model.ArtifactEditOperation, *model.ArtifactVersion, error) {
	_, hash, err := artifact.CanonicalPatch(patch)
	if err != nil {
		return nil, nil, err
	}
	if operation == nil || operation.ID == "" || read == nil {
		return nil, nil, artifact.Err("invalid_request", 400)
	}
	if prior, version, err := committedEditReplay(r.db.WithContext(ctx), owner, operation.ID, hash); err != nil || version != nil {
		return prior, version, err
	}
	var saved *model.ArtifactEditOperation
	var version *model.ArtifactVersion
	cancelled := false
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var req model.ArtifactEditRequest
		if operation.RunID == nil {
			return artifact.Err("not_found", 404)
		}
		if err := tx.Where("run_id=? AND user_id=?", *operation.RunID, owner).First(&req).Error; err != nil {
			return hideMissing(err)
		}
		if req.Mode != "apply" {
			return artifact.Err("target_scope_mismatch", 409)
		}
		m, err := readableManifest(tx, owner, req.ManifestID)
		if err != nil {
			return err
		}
		task, err := sourceLock(tx, owner, m.SourceID)
		if err != nil {
			return err
		}
		if _, err = readableManifest(tx, owner, req.ManifestID); err != nil {
			return err
		}
		if task.Status == model.TaskStatusQueued || task.Status == model.TaskStatusRunning {
			return artifact.Err("source_changed", 409)
		}
		contentHash, _, err := read(ctx, NewRepositories(tx), owner, m.SourceID)
		if err != nil {
			return err
		}
		if contentHash != m.ContentHash {
			return artifact.Err("source_changed", 409)
		}
		a, err := ownedArtifact(tx, owner, req.ArtifactID, true)
		if err != nil {
			return err
		}
		run, err := lockedRun(tx, req.RunID)
		if err != nil {
			return err
		}
		stored, err := editOperationByID(tx, owner, operation.ID, true)
		if err != nil {
			return err
		}
		if stored != nil {
			if stored.PatchHash != hash {
				return artifact.Err("idempotency_conflict", 409)
			}
			if stored.Status == model.ArtifactEditOperationCommitted && stored.ResultVersionID != nil {
				var replay model.ArtifactVersion
				if err = tx.Where("id=?", *stored.ResultVersionID).First(&replay).Error; err != nil {
					return err
				}
				saved, version = stored, &replay
				return nil
			}
		}
		if err = fence(run, token, epoch, time.Now().UTC()); err != nil {
			return err
		}
		if run.CancelRequestedAt != nil {
			cancelled = true
			return terminal(tx, run, model.AgentRunStatusCancelled, "")
		}
		if a.HeadVersion != req.BaseVersion || a.CurrentVersionID == nil || *a.CurrentVersionID != req.BaseVersionID {
			return artifact.Err("version_conflict", 409)
		}
		_, baseBody, allowed, err := loadEditBase(tx, &req)
		if err != nil {
			return err
		}
		serverAuth, err := canonicalEditAuthorization(operation.ID, &req, allowed)
		if err != nil {
			return err
		}
		if err = validateEditOperationBinding(operation, &req, patch, supplied, serverAuth); err != nil {
			return err
		}
		result, err := artifact.EditPatch(baseBody, patch, serverAuth)
		if err != nil {
			return err
		}
		canonical, _, err := artifact.CanonicalPatch(patch)
		if err != nil {
			return err
		}
		candidate := operation
		if stored != nil {
			candidate = stored
		}
		populateEditOperation(candidate, &req, canonical, hash, serverAuth, patch, result, model.ArtifactEditOperationCommitted)
		now := time.Now().UTC()
		candidate.CommittedAt = &now
		version, err = insertEditRevision(tx, a, result.Body, req.ManifestID, "agent", req.BaseVersion, &run.ID, candidate.ID)
		if err != nil {
			return err
		}
		candidate.ResultVersionID = &version.ID
		if stored == nil {
			if err = tx.Create(candidate).Error; err != nil {
				return err
			}
		} else if err = tx.Model(stored).Updates(map[string]any{"status": candidate.Status, "canonical_patch_json": candidate.CanonicalPatchJSON, "patch_hash": candidate.PatchHash, "authorization_json": candidate.AuthorizationJSON, "scope_json": candidate.ScopeJSON, "basis": candidate.Basis, "evidence_ids_json": candidate.EvidenceIDsJSON, "summary": candidate.Summary, "counts_json": candidate.CountsJSON, "changes_json": candidate.ChangesJSON, "block_mappings_json": candidate.BlockMappingsJSON, "result_version_id": version.ID, "committed_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
		run.ResultVersionID = &version.ID
		if err = tx.Model(run).Update("result_version_id", version.ID).Error; err != nil {
			return err
		}
		saved = candidate
		return nil
	})
	if err != nil {
		if prior, replayVersion, replayErr := committedEditReplay(r.db.WithContext(ctx), owner, operation.ID, hash); replayErr == nil && replayVersion != nil {
			return prior, replayVersion, nil
		}
		return nil, nil, err
	}
	if cancelled {
		return nil, nil, artifact.ErrLease
	}
	return saved, version, nil
}

func editOutcomeByKey(tx *gorm.DB, owner int64, key, hash string) (*model.ArtifactEditOutcome, error) {
	var outcome model.ArtifactEditOutcome
	err := tx.Where("user_id=? AND idempotency_key=?", owner, key).First(&outcome).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if outcome.RequestHash != hash {
		return nil, artifact.Err("idempotency_conflict", 409)
	}
	return &outcome, nil
}

func editOutcomeRecords(tx *gorm.DB, owner int64, outcome *model.ArtifactEditOutcome) (*model.ArtifactEditOperation, *model.ArtifactVersion, error) {
	if outcome == nil {
		return nil, nil, nil
	}
	op, err := editOperationByID(tx, owner, outcome.OperationID, false)
	if err != nil || op == nil {
		return nil, nil, err
	}
	var version model.ArtifactVersion
	if err = tx.Where("id=? AND artifact_id=?", outcome.VersionID, op.ArtifactID).First(&version).Error; err != nil {
		return nil, nil, err
	}
	return op, &version, nil
}

func editOutcomeResult(tx *gorm.DB, owner int64, outcome *model.ArtifactEditOutcome) (*model.ArtifactEditOperation, *model.ArtifactVersion, error) {
	op, version, err := editOutcomeRecords(tx, owner, outcome)
	if err != nil || op == nil || version == nil {
		return op, version, err
	}
	// Idempotency replay must not become a read path around source revocation.
	// Gate both the original operation and the immutable result: an undo result
	// can inherit a later head's different manifest.
	if _, err = readableManifest(tx, owner, op.ManifestID); err != nil {
		return nil, nil, err
	}
	if version.ManifestID != op.ManifestID {
		if _, err = readableManifest(tx, owner, version.ManifestID); err != nil {
			return nil, nil, err
		}
	}
	return op, version, nil
}

func lockReadableEditSources(tx *gorm.DB, owner int64, manifestIDs ...string) (map[string]*model.SourceManifest, map[int64]*model.VideoTask, error) {
	manifests := make(map[string]*model.SourceManifest, len(manifestIDs))
	ordered := make([]*model.SourceManifest, 0, len(manifestIDs))
	for _, manifestID := range manifestIDs {
		if manifestID == "" {
			continue
		}
		if _, exists := manifests[manifestID]; exists {
			continue
		}
		var manifest model.SourceManifest
		if err := tx.Where("id=? AND user_id=?", manifestID, owner).First(&manifest).Error; err != nil {
			return nil, nil, hideMissing(err)
		}
		manifests[manifestID] = &manifest
		ordered = append(ordered, &manifest)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].SourceID != ordered[j].SourceID {
			return ordered[i].SourceID < ordered[j].SourceID
		}
		return ordered[i].ID < ordered[j].ID
	})
	tasks := make(map[int64]*model.VideoTask, len(ordered))
	for _, manifest := range ordered {
		if _, exists := tasks[manifest.SourceID]; exists {
			continue
		}
		task, err := sourceLock(tx, owner, manifest.SourceID)
		if err != nil {
			return nil, nil, err
		}
		tasks[manifest.SourceID] = task
	}
	for _, manifest := range ordered {
		readable, err := readableManifest(tx, owner, manifest.ID)
		if err != nil {
			return nil, nil, err
		}
		manifests[manifest.ID] = readable
	}
	return manifests, tasks, nil
}

func verifyLockedEditSources(ctx context.Context, tx *gorm.DB, owner int64, manifests map[string]*model.SourceManifest, tasks map[int64]*model.VideoTask, read SourceReader) error {
	if read == nil {
		return artifact.Err("invalid_request", 400)
	}
	ids := make([]string, 0, len(manifests))
	for id := range manifests {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		manifest := manifests[id]
		task := tasks[manifest.SourceID]
		if task == nil {
			return artifact.Err("source_deleted", 410)
		}
		if task.Status == model.TaskStatusQueued || task.Status == model.TaskStatusRunning {
			return artifact.Err("source_changed", 409)
		}
		hash, _, err := read(ctx, NewRepositories(tx), owner, manifest.SourceID)
		if err != nil {
			return err
		}
		if hash != manifest.ContentHash {
			return artifact.Err("source_changed", 409)
		}
	}
	return nil
}

func (r *ArtifactRepository) undoOutcomeReplay(ctx context.Context, owner int64, key, requestHash string) (*model.ArtifactEditOperation, *model.ArtifactVersion, bool, error) {
	var operation *model.ArtifactEditOperation
	var version *model.ArtifactVersion
	found := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		outcome, err := editOutcomeByKey(tx, owner, key, requestHash)
		if err != nil || outcome == nil {
			return err
		}
		found = true
		operation, version, err = editOutcomeRecords(tx, owner, outcome)
		if err != nil || operation == nil || version == nil {
			return err
		}
		if _, _, err = lockReadableEditSources(tx, owner, operation.ManifestID, version.ManifestID); err != nil {
			return err
		}
		operation, version, err = editOutcomeResult(tx, owner, outcome)
		return err
	})
	return operation, version, found, err
}

func verifyEditSource(ctx context.Context, tx *gorm.DB, owner int64, manifest *model.SourceManifest, read SourceReader) error {
	if read == nil {
		return artifact.Err("invalid_request", 400)
	}
	task, err := sourceLock(tx, owner, manifest.SourceID)
	if err != nil {
		return err
	}
	if _, err = readableManifest(tx, owner, manifest.ID); err != nil {
		return err
	}
	if task.Status == model.TaskStatusQueued || task.Status == model.TaskStatusRunning {
		return artifact.Err("source_changed", 409)
	}
	hash, _, err := read(ctx, NewRepositories(tx), owner, manifest.SourceID)
	if err != nil {
		return err
	}
	if hash != manifest.ContentHash {
		return artifact.Err("source_changed", 409)
	}
	return nil
}

// ApplyEdit publishes a persisted proposal only when its frozen request mode
// is preview. Apply-mode proposals may be committed only by CommitEdit while
// the run lease and cancellation fence are still valid. Apply and undo share the
// owner's durable outcome-key namespace, so the action is part of the request
// hash and callers use a fresh key per action. Replay is resolved before the
// frozen-base head CAS.
func (r *ArtifactRepository) ApplyEdit(ctx context.Context, owner int64, operationID string, expectedHead int64, key, requestHash string, read SourceReader) (*model.ArtifactEditOperation, *model.ArtifactVersion, error) {
	if owner <= 0 || operationID == "" || expectedHead <= 0 || key == "" || requestHash == "" {
		return nil, nil, artifact.Err("invalid_request", 400)
	}
	if outcome, err := editOutcomeByKey(r.db.WithContext(ctx), owner, key, requestHash); err != nil {
		return nil, nil, err
	} else if outcome != nil {
		return editOutcomeResult(r.db.WithContext(ctx), owner, outcome)
	}
	var saved *model.ArtifactEditOperation
	var version *model.ArtifactVersion
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var operation model.ArtifactEditOperation
		if err := tx.Where("id=? AND user_id=?", operationID, owner).First(&operation).Error; err != nil {
			return hideMissing(err)
		}
		if operation.Kind != model.ArtifactEditOperationKindEdit || operation.RunID == nil || operation.RequestID == nil {
			return artifact.Err("invalid_request", 400)
		}
		var req model.ArtifactEditRequest
		if err := tx.Where("id=? AND run_id=? AND user_id=?", *operation.RequestID, *operation.RunID, owner).First(&req).Error; err != nil {
			return hideMissing(err)
		}
		if req.Mode != "preview" {
			return artifact.Err("target_scope_mismatch", 409)
		}
		m, err := readableManifest(tx, owner, req.ManifestID)
		if err != nil {
			return err
		}
		if err = verifyEditSource(ctx, tx, owner, m, read); err != nil {
			return err
		}
		a, err := ownedArtifact(tx, owner, operation.ArtifactID, true)
		if err != nil {
			return err
		}
		locked, err := editOperationByID(tx, owner, operationID, true)
		if err != nil {
			return err
		}
		if locked == nil {
			return artifact.Err("not_found", 404)
		}
		outcome, err := editOutcomeByKey(tx, owner, key, requestHash)
		if err != nil {
			return err
		}
		if outcome != nil {
			saved, version, err = editOutcomeResult(tx, owner, outcome)
			return err
		}
		if locked.Status == model.ArtifactEditOperationCommitted && locked.ResultVersionID != nil {
			if err = tx.Where("id=? AND artifact_id=?", *locked.ResultVersionID, locked.ArtifactID).First(&version).Error; err != nil {
				return err
			}
			outcome = &model.ArtifactEditOutcome{UserID: owner, IdempotencyKey: key, RequestHash: requestHash, Action: "apply", OperationID: locked.ID, VersionID: version.ID, CreatedAt: time.Now().UTC()}
			if err = tx.Create(outcome).Error; err != nil {
				return err
			}
			saved = locked
			return nil
		}
		if locked.Status != model.ArtifactEditOperationProposed {
			return artifact.Err("preview_expired", 409)
		}
		if expectedHead != req.BaseVersion || a.HeadVersion != req.BaseVersion || a.CurrentVersionID == nil || *a.CurrentVersionID != req.BaseVersionID {
			return artifact.Err("version_conflict", 409)
		}
		var patch artifact.Patch
		if err = json.Unmarshal([]byte(locked.CanonicalPatchJSON), &patch); err != nil {
			return err
		}
		_, baseBody, allowed, err := loadEditBase(tx, &req)
		if err != nil {
			return err
		}
		auth, err := canonicalEditAuthorization(locked.ID, &req, allowed)
		if err != nil {
			return err
		}
		result, err := artifact.EditPatch(baseBody, patch, auth)
		if err != nil {
			return err
		}
		version, err = insertEditRevision(tx, a, result.Body, req.ManifestID, "agent", req.BaseVersion, locked.RunID, locked.ID)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		locked.Status = model.ArtifactEditOperationCommitted
		locked.ResultVersionID = &version.ID
		locked.CommittedAt = &now
		locked.UpdatedAt = now
		locked.CountsJSON = artifact.JSON(result.Diff.Counts)
		locked.ChangesJSON = artifact.JSON(result.Diff.Changes)
		locked.BlockMappingsJSON = artifact.JSON(result.Diff.BlockMappings)
		if err = tx.Model(locked).Updates(map[string]any{"status": locked.Status, "result_version_id": version.ID, "committed_at": now, "updated_at": now, "counts_json": locked.CountsJSON, "changes_json": locked.ChangesJSON, "block_mappings_json": locked.BlockMappingsJSON}).Error; err != nil {
			return err
		}
		outcome = &model.ArtifactEditOutcome{UserID: owner, IdempotencyKey: key, RequestHash: requestHash, Action: "apply", OperationID: locked.ID, VersionID: version.ID, CreatedAt: now}
		if err = tx.Create(outcome).Error; err != nil {
			return err
		}
		saved = locked
		return nil
	})
	if err != nil {
		if outcome, replayErr := editOutcomeByKey(r.db.WithContext(ctx), owner, key, requestHash); replayErr == nil && outcome != nil {
			return editOutcomeResult(r.db.WithContext(ctx), owner, outcome)
		}
		return nil, nil, err
	}
	return saved, version, nil
}

func committedEditForRun(tx *gorm.DB, runID string, lock bool) (*model.ArtifactEditOperation, error) {
	var operation model.ArtifactEditOperation
	if lock {
		tx = tx.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := tx.Where("run_id=? AND status=? AND result_version_id IS NOT NULL", runID, model.ArtifactEditOperationCommitted).First(&operation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &operation, err
}

func recoverCommittedEditJournal(tx *gorm.DB, run *model.AgentRun, operation *model.ArtifactEditOperation) error {
	if operation == nil || operation.ResultVersionID == nil {
		return nil
	}
	now := time.Now().UTC()
	outputRef := "artifact-version:" + *operation.ResultVersionID
	outcome := map[string]any{"kind": "committed", "operation_id": operation.ID, "result_version_id": *operation.ResultVersionID}
	checkpoint := artifact.JSON(map[string]any{"result": map[string]any{"output": outcome, "step": map[string]any{"name": "commit_artifact_patch", "tool": "commit_artifact_patch", "output_ref": outputRef}}})
	digest := artifact.Hash(checkpoint)
	var calls []model.AgentToolCall
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("run_id=? AND tool_name=? AND status IN ?", run.ID, "commit_artifact_patch", []string{model.AgentToolCallStatusRunning, model.AgentToolCallStatusAmbiguous, model.AgentToolCallStatusFailed}).Find(&calls).Error; err != nil {
		return err
	}
	for i := range calls {
		call := &calls[i]
		if err := tx.Model(call).Updates(map[string]any{"status": model.AgentToolCallStatusCompleted, "output_ref": outputRef, "result_checkpoint": checkpoint, "result_digest": digest, "evidence_refs": operation.EvidenceIDsJSON, "metrics_json": `{"recovered":true}`, "error_code": "", "error_message": "", "finished_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.AgentStep{}).Where("id=? AND status IN ?", call.AgentStepID, []string{model.AgentStepStatusRunning, model.AgentStepStatusAmbiguous, model.AgentStepStatusFailed}).Updates(map[string]any{"status": model.AgentStepStatusCompleted, "output_ref": outputRef, "result_checkpoint": checkpoint, "result_digest": digest, "error_code": "", "error_message": "", "lease_token": "", "lease_expires_at": nil, "finished_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
	}
	if len(calls) > 0 {
		return nil
	}
	var completed int64
	if err := tx.Model(&model.AgentToolCall{}).Where("run_id=? AND tool_name=? AND status=?", run.ID, "commit_artifact_patch", model.AgentToolCallStatusCompleted).Count(&completed).Error; err != nil {
		return err
	}
	if completed > 0 {
		return nil
	}
	var sequence int
	if err := tx.Model(&model.AgentStep{}).Where("run_id=?", run.ID).Select("COALESCE(MAX(sequence),0)").Scan(&sequence).Error; err != nil {
		return err
	}
	stepPK := uuid.NewSHA1(uuid.NameSpaceOID, []byte(operation.ID+"\x00recovery-step")).String()
	callPK := uuid.NewSHA1(uuid.NameSpaceOID, []byte(operation.ID+"\x00recovery-call")).String()
	step := &model.AgentStep{ID: stepPK, RunID: run.ID, StepID: "commit_artifact_patch_recovery", Attempt: 1, Sequence: sequence + 1, Kind: "tool", Action: "commit_artifact_patch", Status: model.AgentStepStatusCompleted, SafeReason: "durable_commit_recovery", InputSummary: `{}`, OutputRef: outputRef, ResultCheckpoint: checkpoint, ResultDigest: digest, ReplaySafe: false, LeaseVersion: 1, StartedAt: now, FinishedAt: &now, CreatedAt: now, UpdatedAt: now}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(step).Error; err != nil {
		return err
	}
	call := &model.AgentToolCall{ID: callPK, RunID: run.ID, StepID: step.StepID, Attempt: 1, AgentStepID: step.ID, CallKind: model.AgentCallKindTool, ToolName: "commit_artifact_patch", Status: model.AgentToolCallStatusCompleted, InputSummary: `{}`, ArgumentsDigest: artifact.Hash(operation.PatchHash), CallDigest: artifact.Hash(operation.ID + ":commit_artifact_patch"), OutputRef: outputRef, ResultCheckpoint: checkpoint, ResultDigest: digest, EvidenceRefs: operation.EvidenceIDsJSON, FinalEvidenceRefs: `[]`, MetricsJSON: `{"recovered":true}`, UsageSource: model.AgentCallUsageUnknown, ContextUsageSource: model.AgentCallUsageUnknown, StartedAt: now, FinishedAt: &now, CreatedAt: now, UpdatedAt: now}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(call).Error
}

func completeCommittedEditTx(tx *gorm.DB, run *model.AgentRun) (*model.ArtifactEditOperation, bool, error) {
	if run.SubjectKind != model.AgentRunSubjectArtifactEdit {
		return nil, false, nil
	}
	operation, err := committedEditForRun(tx, run.ID, true)
	if err != nil || operation == nil {
		return operation, false, err
	}
	if run.ResultVersionID == nil || *run.ResultVersionID != *operation.ResultVersionID {
		run.ResultVersionID = operation.ResultVersionID
		if err = tx.Model(run).Update("result_version_id", *operation.ResultVersionID).Error; err != nil {
			return nil, false, err
		}
	}
	if err = recoverCommittedEditJournal(tx, run, operation); err != nil {
		return nil, false, err
	}
	if run.Status != model.AgentRunStatusCompleted {
		if err = terminal(tx, run, model.AgentRunStatusCompleted, ""); err != nil {
			return nil, false, err
		}
	}
	if operation.RunFinalizedAt == nil {
		now := time.Now().UTC()
		operation.RunFinalizedAt = &now
		operation.UpdatedAt = now
		if err = tx.Model(operation).Updates(map[string]any{"run_finalized_at": now, "updated_at": now}).Error; err != nil {
			return nil, false, err
		}
	}
	return operation, true, nil
}

// RecoverCommittedEdit closes the crash window between an atomic version
// commit and the later tool/run checkpoint. It never creates another version.
func (r *ArtifactRepository) RecoverCommittedEdit(ctx context.Context, runID string) (*model.ArtifactEditOperation, bool, error) {
	if runID == "" {
		return nil, false, artifact.Err("invalid_request", 400)
	}
	if operation, err := committedEditForRun(r.db.WithContext(ctx), runID, false); err != nil || operation == nil {
		return operation, false, err
	}
	var saved *model.ArtifactEditOperation
	var recovered bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		run, err := lockedRun(tx, runID)
		if err != nil {
			return err
		}
		saved, recovered, err = completeCommittedEditTx(tx, run)
		return err
	})
	return saved, recovered, err
}

// EditOperation returns only owner/source-gated product state. Recovery uses
// the private committed lookup above so revocation cannot expose content.
func (r *ArtifactRepository) EditOperation(ctx context.Context, owner int64, operationID string) (*model.ArtifactEditOperation, error) {
	var operation *model.ArtifactEditOperation
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		operation, err = editOperationByID(tx, owner, operationID, false)
		if err != nil {
			return err
		}
		if operation == nil {
			return artifact.Err("not_found", 404)
		}
		if _, err = ownedArtifact(tx, owner, operation.ArtifactID, false); err != nil {
			return err
		}
		_, err = readableManifest(tx, owner, operation.ManifestID)
		return err
	})
	return operation, err
}

func decodeArtifactBody(version *model.ArtifactVersion) (artifact.Body, error) {
	var body artifact.Body
	err := json.Unmarshal([]byte(version.BodyJSON), &body)
	return body, err
}

// UndoEdit publishes a three-way safe inverse. Later unrelated fields are
// retained; SafeUndo rejects any overlap or unsafe structural anchor.
func (r *ArtifactRepository) UndoEdit(ctx context.Context, owner int64, operationID string, expectedHead int64, key, requestHash string, read SourceReader) (*model.ArtifactEditOperation, *model.ArtifactVersion, error) {
	if owner <= 0 || operationID == "" || expectedHead <= 0 || key == "" || requestHash == "" {
		return nil, nil, artifact.Err("invalid_request", 400)
	}
	if operation, version, found, err := r.undoOutcomeReplay(ctx, owner, key, requestHash); err != nil || found {
		return operation, version, err
	}
	var saved *model.ArtifactEditOperation
	var undoVersion *model.ArtifactVersion
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var operation model.ArtifactEditOperation
		if err := tx.Where("id=? AND user_id=?", operationID, owner).First(&operation).Error; err != nil {
			return hideMissing(err)
		}
		if operation.Kind != model.ArtifactEditOperationKindEdit || operation.Status != model.ArtifactEditOperationCommitted || operation.ResultVersionID == nil {
			return artifact.Err("undo_conflict", 409)
		}
		// Discover the current immutable version before taking write locks so all
		// involved source rows can be locked first. The artifact CAS below proves
		// that this preflight view did not change while the locks were acquired.
		artifactSnapshot, err := ownedArtifact(tx, owner, operation.ArtifactID, false)
		if err != nil {
			return err
		}
		if artifactSnapshot.CurrentVersionID == nil {
			return artifact.Err("version_conflict", 409)
		}
		var currentVersion model.ArtifactVersion
		if err = tx.Where("id=? AND artifact_id=? AND version=?", *artifactSnapshot.CurrentVersionID, operation.ArtifactID, artifactSnapshot.HeadVersion).First(&currentVersion).Error; err != nil {
			return artifact.Err("version_conflict", 409)
		}
		manifestIDs := []string{operation.ManifestID, currentVersion.ManifestID}
		if operation.UndoVersionID != nil {
			var existingUndo model.ArtifactVersion
			if err = tx.Where("id=? AND artifact_id=?", *operation.UndoVersionID, operation.ArtifactID).First(&existingUndo).Error; err != nil {
				return err
			}
			manifestIDs = append(manifestIDs, existingUndo.ManifestID)
		}
		lockedManifests, lockedSources, err := lockReadableEditSources(tx, owner, manifestIDs...)
		if err != nil {
			return err
		}
		if err = verifyLockedEditSources(ctx, tx, owner, lockedManifests, lockedSources, read); err != nil {
			return err
		}
		a, err := ownedArtifact(tx, owner, operation.ArtifactID, true)
		if err != nil {
			return err
		}
		if a.HeadVersion != expectedHead || a.HeadVersion != currentVersion.Version || a.CurrentVersionID == nil || *a.CurrentVersionID != currentVersion.ID {
			return artifact.Err("version_conflict", 409)
		}
		locked, err := editOperationByID(tx, owner, operationID, true)
		if err != nil {
			return err
		}
		if locked == nil {
			return artifact.Err("not_found", 404)
		}
		if locked.Kind != model.ArtifactEditOperationKindEdit || locked.Status != model.ArtifactEditOperationCommitted || locked.ResultVersionID == nil || locked.ArtifactID != operation.ArtifactID || locked.ManifestID != operation.ManifestID {
			return artifact.Err("undo_conflict", 409)
		}
		outcome, err := editOutcomeByKey(tx, owner, key, requestHash)
		if err != nil {
			return err
		}
		if outcome != nil {
			outcomeOperation, outcomeVersion, outcomeErr := editOutcomeRecords(tx, owner, outcome)
			if outcomeErr != nil {
				return outcomeErr
			}
			if outcomeOperation == nil || outcomeVersion == nil || lockedManifests[outcomeOperation.ManifestID] == nil || lockedManifests[outcomeVersion.ManifestID] == nil {
				return artifact.Err("version_conflict", 409)
			}
			saved, undoVersion, err = editOutcomeResult(tx, owner, outcome)
			return err
		}
		if locked.UndoVersionID != nil {
			if err = tx.Where("id=? AND artifact_id=?", *locked.UndoVersionID, locked.ArtifactID).First(&undoVersion).Error; err != nil {
				return err
			}
			if lockedManifests[undoVersion.ManifestID] == nil {
				return artifact.Err("version_conflict", 409)
			}
			outcome = &model.ArtifactEditOutcome{UserID: owner, IdempotencyKey: key, RequestHash: requestHash, Action: "undo", OperationID: locked.ID, VersionID: undoVersion.ID, CreatedAt: time.Now().UTC()}
			if err = tx.Create(outcome).Error; err != nil {
				return err
			}
			saved = locked
			return nil
		}
		var baseVersion, resultVersion model.ArtifactVersion
		if err = tx.Where("id=? AND artifact_id=?", locked.BaseVersionID, locked.ArtifactID).First(&baseVersion).Error; err != nil {
			return err
		}
		if err = tx.Where("id=? AND artifact_id=?", *locked.ResultVersionID, locked.ArtifactID).First(&resultVersion).Error; err != nil {
			return err
		}
		if err = tx.Where("id=? AND artifact_id=? AND version=?", *a.CurrentVersionID, locked.ArtifactID, expectedHead).First(&currentVersion).Error; err != nil {
			return artifact.Err("version_conflict", 409)
		}
		baseBody, err := decodeArtifactBody(&baseVersion)
		if err != nil {
			return err
		}
		resultBody, err := decodeArtifactBody(&resultVersion)
		if err != nil {
			return err
		}
		currentBody, err := decodeArtifactBody(&currentVersion)
		if err != nil {
			return err
		}
		undoResult, err := artifact.SafeUndo(baseBody, resultBody, currentBody)
		if err != nil {
			return err
		}
		allowed, err := allowedEvidence(tx, currentVersion.ManifestID)
		if err != nil {
			return err
		}
		if err = undoResult.Body.Validate(allowed); err != nil {
			return err
		}
		undoOperationID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(locked.ID+"\x00undo\x00"+key)).String()
		undoVersion, err = insertEditRevision(tx, a, undoResult.Body, currentVersion.ManifestID, "undo", expectedHead, nil, undoOperationID)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		parentID := locked.ID
		undoOperation := &model.ArtifactEditOperation{ID: undoOperationID, UserID: owner, ArtifactID: locked.ArtifactID, RequestID: locked.RequestID, Kind: model.ArtifactEditOperationKindUndo, ParentOperationID: &parentID, BaseVersionID: currentVersion.ID, BaseVersion: currentVersion.Version, ManifestID: currentVersion.ManifestID, CanonicalPatchJSON: `{}`, PatchHash: requestHash, AuthorizationJSON: `{}`, ScopeJSON: locked.ScopeJSON, ToolSchemaDigest: locked.ToolSchemaDigest, Basis: locked.Basis, EvidenceIDsJSON: locked.EvidenceIDsJSON, Status: model.ArtifactEditOperationCommitted, Summary: "撤销：" + locked.Summary, CountsJSON: artifact.JSON(undoResult.Diff.Counts), ChangesJSON: artifact.JSON(undoResult.Diff.Changes), BlockMappingsJSON: artifact.JSON(undoResult.Diff.BlockMappings), ResultVersionID: &undoVersion.ID, CreatedAt: now, UpdatedAt: now, CommittedAt: &now}
		if err = tx.Create(undoOperation).Error; err != nil {
			return err
		}
		locked.UndoVersionID = &undoVersion.ID
		locked.UpdatedAt = now
		if err = tx.Model(locked).Updates(map[string]any{"undo_version_id": undoVersion.ID, "updated_at": now}).Error; err != nil {
			return err
		}
		outcome = &model.ArtifactEditOutcome{UserID: owner, IdempotencyKey: key, RequestHash: requestHash, Action: "undo", OperationID: locked.ID, VersionID: undoVersion.ID, CreatedAt: now}
		if err = tx.Create(outcome).Error; err != nil {
			return err
		}
		saved = locked
		return nil
	})
	if err != nil {
		if operation, version, found, replayErr := r.undoOutcomeReplay(ctx, owner, key, requestHash); found {
			return operation, version, replayErr
		}
		return nil, nil, err
	}
	return saved, undoVersion, nil
}
