package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

type artifactEditFixture struct {
	db       *gorm.DB
	repo     *ArtifactRepository
	owner    int64
	taskID   int64
	manifest model.SourceManifest
	artifact *model.Artifact
	base     *model.ArtifactVersion
}

func newArtifactEditFixture(t *testing.T) *artifactEditFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared&_pragma=busy_timeout(5000)"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = model.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return newArtifactEditFixtureOnDB(t, db)
}

func newArtifactEditFixtureOnDB(t *testing.T, db *gorm.DB) *artifactEditFixture {
	t.Helper()
	var err error
	fx := &artifactEditFixture{db: db, repo: NewArtifactRepository(db), owner: 17, taskID: 42}
	task := model.VideoTask{ID: fx.taskID, UserID: fx.owner, FileMD5: "11111111111111111111111111111111", Filename: "edit.mp4", Title: "Edit", Status: model.TaskStatusCompleted}
	if err = db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	fx.manifest = model.SourceManifest{ID: "manifest-edit", UserID: fx.owner, SourceID: fx.taskID, ContentHash: "source-hash", Title: "Edit"}
	if err = db.Create(&fx.manifest).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&model.SourceSnapshotItem{ID: "evidence-1", ManifestID: fx.manifest.ID, SourceID: fx.taskID, SourceTitle: "Edit", Content: "安装证据", ContentHash: "evidence-hash", Position: 0}).Error; err != nil {
		t.Fatal(err)
	}
	body := artifact.Body{SchemaVersion: 1, Kind: "study", Title: "安装笔记", Blocks: []artifact.Block{{BlockID: "install", Type: "concept", Title: "安装", Content: "旧内容", ClaimOrigin: "source", EvidenceRefs: []artifact.Ref{{EvidenceID: "evidence-1", Relation: "supports"}}}}, Warnings: []string{}}
	fx.artifact, err = fx.repo.Create(context.Background(), fx.owner, fx.manifest.ID, body)
	if err != nil {
		t.Fatal(err)
	}
	_, fx.base, err = fx.repo.Get(context.Background(), fx.owner, fx.artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	return fx
}

func (f *artifactEditFixture) requestAndRun(key, hash, suffix string) (*model.ArtifactEditRequest, *model.AgentRun) {
	now := time.Now().UTC()
	req := &model.ArtifactEditRequest{
		ID:                   "edit-request-" + suffix,
		RunID:                "edit-run-" + suffix,
		UserID:               f.owner,
		IdempotencyKey:       key,
		RequestHash:          hash,
		RequestJSON:          `{"instruction":"拆成步骤","expected_head_version":1,"selected_block_ids":["install"],"mode":"apply"}`,
		ArtifactID:           f.artifact.ID,
		BaseVersionID:        f.base.ID,
		BaseVersion:          f.base.Version,
		ManifestID:           f.manifest.ID,
		Instruction:          "拆成步骤",
		Mode:                 "apply",
		SelectedBlockIDsJSON: `["install"]`,
		Recipe:               "study-edit-v1",
		ProfileFingerprint:   "profile-hash",
		ToolPolicyJSON:       `["read_artifact","propose_artifact_patch","commit_artifact_patch"]`,
		QueueDeadline:        now.Add(time.Minute),
		CreatedAt:            now,
	}
	run := &model.AgentRun{ID: req.RunID, UserID: f.owner, SubjectKind: model.AgentRunSubjectArtifactEdit, SubjectID: req.ID, ExecutionKind: "artifact", RecipeVersion: req.Recipe, ScopeType: "video", TaskID: f.taskID, Goal: req.Instruction, Mode: req.Mode, AgentProfile: "default", ProfileSnapshot: `{}`, PolicySnapshot: `{}`, BudgetSnapshot: `{}`, Status: model.AgentRunStatusPending, Version: 1, MaxSteps: 8, MaxToolCalls: 8, MaxLLMCalls: 4, MaxAttemptsPerStep: 1, MaxDurationMs: 60_000, CreatedAt: now, UpdatedAt: now}
	return req, run
}

func TestArtifactEditSubmitReplaysSameRequestAndRejectsChangedPayload(t *testing.T) {
	fx := newArtifactEditFixture(t)
	ctx := context.Background()

	req, run := fx.requestAndRun("submit-key", "request-hash", "first")
	accepted, err := fx.repo.SubmitEdit(ctx, req, run)
	if err != nil {
		t.Fatalf("SubmitEdit() error = %v", err)
	}

	replay, replayRun := fx.requestAndRun("submit-key", "request-hash", "replay")
	replayed, err := fx.repo.SubmitEdit(ctx, replay, replayRun)
	if err != nil {
		t.Fatalf("SubmitEdit() replay error = %v", err)
	}
	if replayed.ID != accepted.ID || replayed.RunID != accepted.RunID {
		t.Fatalf("replay = %+v, accepted = %+v", replayed, accepted)
	}
	var generationDispatches, editDispatches int64
	if err = fx.db.Model(&model.GenerationDispatch{}).Where("run_id=?", accepted.RunID).Count(&generationDispatches).Error; err != nil {
		t.Fatal(err)
	}
	if err = fx.db.Model(&model.ArtifactEditDispatch{}).Where("run_id=?", accepted.RunID).Count(&editDispatches).Error; err != nil {
		t.Fatal(err)
	}
	if generationDispatches != 0 || editDispatches != 1 {
		t.Fatalf("edit run outboxes: generation=%d edit=%d, want 0/1", generationDispatches, editDispatches)
	}

	changed, changedRun := fx.requestAndRun("submit-key", "different-hash", "changed")
	_, err = fx.repo.SubmitEdit(ctx, changed, changedRun)
	var apiErr *artifact.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "idempotency_conflict" {
		t.Fatalf("changed payload error = %v, want idempotency_conflict", err)
	}

	storedRun, storedRequest, err := fx.repo.EditRun(ctx, fx.owner, accepted.RunID)
	if err != nil {
		t.Fatalf("EditRun() error = %v", err)
	}
	if storedRun.ID != accepted.RunID || storedRequest.ID != accepted.ID || storedRequest.BaseVersionID != fx.base.ID {
		t.Fatalf("EditRun() = run %+v request %+v", storedRun, storedRequest)
	}
}

func (f *artifactEditFixture) sourceReader() SourceReader {
	return func(context.Context, *Repositories, int64, int64) (string, []model.SourceSnapshotItem, error) {
		return f.manifest.ContentHash, nil, nil
	}
}

func TestArtifactEditProposalAndDirectCommitAreDurableAndReplaySafe(t *testing.T) {
	fx := newArtifactEditFixture(t)
	ctx := context.Background()
	req, run := fx.requestAndRun("commit-request", "request-hash", "commit")
	if _, err := fx.repo.SubmitEdit(ctx, req, run); err != nil {
		t.Fatal(err)
	}
	claimed, err := fx.repo.Claim(ctx, run.ID, "lease-token", time.Now().UTC())
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	journal := NewAgentExecutionRepository(fx.db)
	stepNow := time.Now().UTC()
	step, err := journal.ClaimStep(ctx, AgentStepClaimRequest{UserID: fx.owner, RunID: run.ID, StepID: "commit", Attempt: 1, Sequence: 1, Kind: "tool", Action: "commit_artifact_patch", InputSummary: `{}`, ArgumentsDigest: artifact.Hash(`{"operation_id":"edit-operation-commit"}`), CallDigest: artifact.Hash(`commit_artifact_patch`), ToolName: "commit_artifact_patch", CallKind: model.AgentCallKindTool, ReplaySafe: false, LeaseToken: "commit-step-lease", Now: stepNow, LeaseUntil: stepNow.Add(time.Minute)})
	if err != nil || step.Outcome != AgentStepClaimAcquired {
		t.Fatalf("ClaimStep(commit) = %+v error %v", step, err)
	}

	content := "第一步：安装。第二步：验证。"
	var baseBody artifact.Body
	if err := json.Unmarshal([]byte(fx.base.BodyJSON), &baseBody); err != nil {
		t.Fatal(err)
	}
	patch := artifact.Patch{SchemaVersion: 1, ArtifactID: fx.artifact.ID, BaseVersionID: fx.base.ID, BaseVersion: fx.base.Version, Basis: artifact.PatchBasisUserInstruction, EvidenceIDs: []string{}, Operations: []artifact.PatchOperation{{Op: artifact.PatchOpUpdateBlock, BlockID: "install", ExpectedHash: artifact.BlockHash(baseBody.Blocks[0]), Content: &content}}}
	auth := artifact.PatchAuthorization{OperationID: "edit-operation-commit", ArtifactID: fx.artifact.ID, BaseVersionID: fx.base.ID, BaseVersion: fx.base.Version, SelectedBlockIDs: []string{"install"}, AllowedEvidenceIDs: map[string]bool{"evidence-1": true}}
	runID, requestID := run.ID, req.ID
	op := &model.ArtifactEditOperation{ID: auth.OperationID, UserID: fx.owner, ArtifactID: fx.artifact.ID, RequestID: &requestID, RunID: &runID, Kind: model.ArtifactEditOperationKindEdit, BaseVersionID: fx.base.ID, BaseVersion: fx.base.Version, ManifestID: fx.manifest.ID, Summary: "拆成步骤", ScopeJSON: `["install"]`, ToolSchemaDigest: "tool-schema"}
	proposal, err := fx.repo.PersistEditProposal(ctx, fx.owner, op, patch, auth, "lease-token", claimed.RunLeaseEpoch)
	if err != nil {
		t.Fatalf("PersistEditProposal() error = %v", err)
	}
	if proposal.Status != model.ArtifactEditOperationProposed {
		t.Fatalf("proposal status = %q", proposal.Status)
	}
	before, _, err := fx.repo.Get(ctx, fx.owner, fx.artifact.ID)
	if err != nil || before.HeadVersion != fx.base.Version {
		t.Fatalf("proposal moved head: artifact=%+v err=%v", before, err)
	}

	committed, version, err := fx.repo.CommitEdit(ctx, fx.owner, op, patch, auth, "lease-token", claimed.RunLeaseEpoch, fx.sourceReader())
	if err != nil {
		t.Fatalf("CommitEdit() error = %v", err)
	}
	if committed.Status != model.ArtifactEditOperationCommitted || committed.ResultVersionID == nil || *committed.ResultVersionID != version.ID {
		t.Fatalf("committed operation = %+v version = %+v", committed, version)
	}
	if version.Origin != "agent" || version.EditOperationID == nil || *version.EditOperationID != op.ID {
		t.Fatalf("version provenance = %+v", version)
	}

	// A replay is resolved by operation ID+patch hash before the stale head and
	// lease are checked, so it cannot create a second version.
	replayed, replayVersion, err := fx.repo.CommitEdit(ctx, fx.owner, op, patch, auth, "stale-token", claimed.RunLeaseEpoch, fx.sourceReader())
	if err != nil || replayed.ResultVersionID == nil || replayVersion.ID != version.ID {
		t.Fatalf("CommitEdit() replay = op %+v version %+v err %v", replayed, replayVersion, err)
	}
	var versions int64
	if err = fx.db.Model(&model.ArtifactVersion{}).Where("artifact_id=?", fx.artifact.ID).Count(&versions).Error; err != nil || versions != 2 {
		t.Fatalf("version count = %d err = %v", versions, err)
	}

	different := patch
	different.Basis = artifact.PatchBasisEvidenceSupported
	_, _, err = fx.repo.CommitEdit(ctx, fx.owner, op, different, auth, "stale-token", claimed.RunLeaseEpoch, fx.sourceReader())
	var apiErr *artifact.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "idempotency_conflict" {
		t.Fatalf("different operation payload error = %v", err)
	}
	if err = fx.repo.Cancel(ctx, fx.owner, run.ID, model.AgentRunSubjectArtifactEdit); err != nil {
		t.Fatalf("Cancel() after committed operation error = %v", err)
	}
	finalRun, _, err := fx.repo.EditRun(ctx, fx.owner, run.ID)
	if err != nil || finalRun.Status != model.AgentRunStatusCompleted || finalRun.ResultVersionID == nil {
		t.Fatalf("committed operation did not win cancellation: run=%+v err=%v", finalRun, err)
	}
	records, err := journal.GetExecution(ctx, fx.owner, run.ID)
	if err != nil || len(records.ToolCalls) != 1 || records.ToolCalls[0].Status != model.AgentToolCallStatusCompleted || records.ToolCalls[0].ResultCheckpoint == "" {
		t.Fatalf("commit recovery journal = %+v err=%v", records, err)
	}
	recovered, ok, err := fx.repo.RecoverCommittedEdit(ctx, run.ID)
	if err != nil || !ok || recovered.RunFinalizedAt == nil {
		t.Fatalf("RecoverCommittedEdit() = operation %+v ok %v err %v", recovered, ok, err)
	}
}

func TestArtifactEditApplyUsesSeparateIdempotencyBeforeHeadCAS(t *testing.T) {
	fx := newArtifactEditFixture(t)
	ctx := context.Background()
	req, run := fx.requestAndRun("preview-request", "preview-request-hash", "preview")
	req.Mode, run.Mode = "preview", "preview"
	req.ToolPolicyJSON = `["read_artifact","propose_artifact_patch"]`
	if _, err := fx.repo.SubmitEdit(ctx, req, run); err != nil {
		t.Fatal(err)
	}
	claimed, err := fx.repo.Claim(ctx, run.ID, "preview-lease", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	var baseBody artifact.Body
	if err = json.Unmarshal([]byte(fx.base.BodyJSON), &baseBody); err != nil {
		t.Fatal(err)
	}
	content := "预览修改"
	patch := artifact.Patch{SchemaVersion: 1, ArtifactID: fx.artifact.ID, BaseVersionID: fx.base.ID, BaseVersion: fx.base.Version, Basis: artifact.PatchBasisUserInstruction, EvidenceIDs: []string{}, Operations: []artifact.PatchOperation{{Op: artifact.PatchOpUpdateBlock, BlockID: "install", ExpectedHash: artifact.BlockHash(baseBody.Blocks[0]), Content: &content}}}
	auth := artifact.PatchAuthorization{OperationID: "edit-operation-preview", ArtifactID: fx.artifact.ID, BaseVersionID: fx.base.ID, BaseVersion: fx.base.Version, SelectedBlockIDs: []string{"install"}, AllowedEvidenceIDs: map[string]bool{"evidence-1": true}}
	runID, requestID := run.ID, req.ID
	op := &model.ArtifactEditOperation{ID: auth.OperationID, UserID: fx.owner, ArtifactID: fx.artifact.ID, RequestID: &requestID, RunID: &runID, Kind: model.ArtifactEditOperationKindEdit, BaseVersionID: fx.base.ID, BaseVersion: fx.base.Version, ManifestID: fx.manifest.ID, Summary: "预览修改", ScopeJSON: `["install"]`}
	if _, err = fx.repo.PersistEditProposal(ctx, fx.owner, op, patch, auth, "preview-lease", claimed.RunLeaseEpoch); err != nil {
		t.Fatal(err)
	}
	if err = fx.repo.Finish(ctx, run.ID, "preview-lease", claimed.RunLeaseEpoch, model.AgentRunStatusCompleted, ""); err != nil {
		t.Fatal(err)
	}

	committed, version, err := fx.repo.ApplyEdit(ctx, fx.owner, op.ID, fx.base.Version, "apply-key", "apply-hash", fx.sourceReader())
	if err != nil {
		t.Fatalf("ApplyEdit() error = %v", err)
	}
	if committed.ResultVersionID == nil || *committed.ResultVersionID != version.ID || version.Origin != "agent" {
		t.Fatalf("ApplyEdit() = operation %+v version %+v", committed, version)
	}

	replayed, replayVersion, err := fx.repo.ApplyEdit(ctx, fx.owner, op.ID, fx.base.Version, "apply-key", "apply-hash", fx.sourceReader())
	if err != nil || replayVersion.ID != version.ID || replayed.ID != committed.ID {
		t.Fatalf("ApplyEdit() replay = operation %+v version %+v error %v", replayed, replayVersion, err)
	}
	_, _, err = fx.repo.ApplyEdit(ctx, fx.owner, op.ID, fx.base.Version, "apply-key", "changed-hash", fx.sourceReader())
	var apiErr *artifact.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "idempotency_conflict" {
		t.Fatalf("ApplyEdit() changed replay error = %v", err)
	}
}

func TestArtifactEditCancelledApplyRunProposalCannotUsePublicApply(t *testing.T) {
	fx := newArtifactEditFixture(t)
	ctx := context.Background()
	req, run := fx.requestAndRun("cancelled-apply-request", "cancelled-apply-request-hash", "cancelled-apply-proposal")
	if _, err := fx.repo.SubmitEdit(ctx, req, run); err != nil {
		t.Fatal(err)
	}
	claimed, err := fx.repo.Claim(ctx, run.ID, "cancelled-apply-lease", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}

	var baseBody artifact.Body
	if err = json.Unmarshal([]byte(fx.base.BodyJSON), &baseBody); err != nil {
		t.Fatal(err)
	}
	content := "取消后不能保存"
	patch := artifact.Patch{SchemaVersion: 1, ArtifactID: fx.artifact.ID, BaseVersionID: fx.base.ID, BaseVersion: fx.base.Version, Basis: artifact.PatchBasisUserInstruction, EvidenceIDs: []string{}, Operations: []artifact.PatchOperation{{Op: artifact.PatchOpUpdateBlock, BlockID: "install", ExpectedHash: artifact.BlockHash(baseBody.Blocks[0]), Content: &content}}}
	auth := artifact.PatchAuthorization{OperationID: "edit-operation-cancelled-apply-proposal", ArtifactID: fx.artifact.ID, BaseVersionID: fx.base.ID, BaseVersion: fx.base.Version, SelectedBlockIDs: []string{"install"}, AllowedEvidenceIDs: map[string]bool{"evidence-1": true}}
	runID, requestID := run.ID, req.ID
	op := &model.ArtifactEditOperation{ID: auth.OperationID, UserID: fx.owner, ArtifactID: fx.artifact.ID, RequestID: &requestID, RunID: &runID, Kind: model.ArtifactEditOperationKindEdit, BaseVersionID: fx.base.ID, BaseVersion: fx.base.Version, ManifestID: fx.manifest.ID, Summary: "取消后不能保存", ScopeJSON: `["install"]`}
	proposal, err := fx.repo.PersistEditProposal(ctx, fx.owner, op, patch, auth, "cancelled-apply-lease", claimed.RunLeaseEpoch)
	if err != nil || proposal.Status != model.ArtifactEditOperationProposed {
		t.Fatalf("PersistEditProposal() = %+v error %v", proposal, err)
	}
	if err = fx.repo.Cancel(ctx, fx.owner, run.ID, model.AgentRunSubjectArtifactEdit); err != nil {
		t.Fatal(err)
	}
	storedRun, _, err := fx.repo.EditRun(ctx, fx.owner, run.ID)
	if err != nil || storedRun.CancelRequestedAt == nil {
		t.Fatalf("cancelled run = %+v error %v", storedRun, err)
	}

	_, _, err = fx.repo.ApplyEdit(ctx, fx.owner, op.ID, fx.base.Version, "cancelled-public-apply-key", "cancelled-public-apply-hash", fx.sourceReader())
	if artifactErrorCodeForTest(err) != "target_scope_mismatch" {
		t.Fatalf("ApplyEdit() after cancellation error = %v, want target_scope_mismatch", err)
	}
	versions, err := fx.repo.Versions(ctx, fx.owner, fx.artifact.ID)
	if err != nil || len(versions) != 1 {
		t.Fatalf("versions after cancelled public apply = %d error %v, want one base version", len(versions), err)
	}
}

func TestArtifactEditCancellationBeforeCommitCreatesNoVersion(t *testing.T) {
	fx := newArtifactEditFixture(t)
	ctx := context.Background()
	req, run := fx.requestAndRun("cancel-request", "cancel-request-hash", "cancel")
	if _, err := fx.repo.SubmitEdit(ctx, req, run); err != nil {
		t.Fatal(err)
	}
	claimed, err := fx.repo.Claim(ctx, run.ID, "cancel-lease", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err = fx.repo.Cancel(ctx, fx.owner, run.ID, model.AgentRunSubjectArtifactEdit); err != nil {
		t.Fatal(err)
	}
	var baseBody artifact.Body
	if err = json.Unmarshal([]byte(fx.base.BodyJSON), &baseBody); err != nil {
		t.Fatal(err)
	}
	content := "不应保存"
	patch := artifact.Patch{SchemaVersion: 1, ArtifactID: fx.artifact.ID, BaseVersionID: fx.base.ID, BaseVersion: fx.base.Version, Basis: artifact.PatchBasisUserInstruction, EvidenceIDs: []string{}, Operations: []artifact.PatchOperation{{Op: artifact.PatchOpUpdateBlock, BlockID: "install", ExpectedHash: artifact.BlockHash(baseBody.Blocks[0]), Content: &content}}}
	auth := artifact.PatchAuthorization{OperationID: "edit-operation-cancel", ArtifactID: fx.artifact.ID, BaseVersionID: fx.base.ID, BaseVersion: fx.base.Version, SelectedBlockIDs: []string{"install"}, AllowedEvidenceIDs: map[string]bool{"evidence-1": true}}
	runID, requestID := run.ID, req.ID
	op := &model.ArtifactEditOperation{ID: auth.OperationID, UserID: fx.owner, ArtifactID: fx.artifact.ID, RequestID: &requestID, RunID: &runID, Kind: model.ArtifactEditOperationKindEdit, BaseVersionID: fx.base.ID, BaseVersion: fx.base.Version, ManifestID: fx.manifest.ID, Summary: "不应保存"}
	_, _, err = fx.repo.CommitEdit(ctx, fx.owner, op, patch, auth, "cancel-lease", claimed.RunLeaseEpoch, fx.sourceReader())
	if !errors.Is(err, artifact.ErrLease) {
		t.Fatalf("CommitEdit() after cancel error = %v, want lease loss", err)
	}
	versions, err := fx.repo.Versions(ctx, fx.owner, fx.artifact.ID)
	if err != nil || len(versions) != 1 {
		t.Fatalf("versions after cancelled commit = %d err=%v", len(versions), err)
	}
}

func TestArtifactEditJournalAcceptsEditRuns(t *testing.T) {
	fx := newArtifactEditFixture(t)
	ctx := context.Background()
	req, run := fx.requestAndRun("journal-request", "journal-request-hash", "journal")
	if _, err := fx.repo.SubmitEdit(ctx, req, run); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	journal := NewAgentExecutionRepository(fx.db)
	claim, err := journal.ClaimStep(ctx, AgentStepClaimRequest{UserID: fx.owner, RunID: run.ID, StepID: "read-base", Attempt: 1, Sequence: 1, Kind: "tool", Action: "read_artifact", InputSummary: `{}`, ArgumentsDigest: artifact.Hash(`{}`), CallDigest: artifact.Hash(`read_artifact:{}`), ToolName: "read_artifact", CallKind: model.AgentCallKindTool, ReplaySafe: true, LeaseToken: "step-lease", Now: now, LeaseUntil: now.Add(time.Minute)})
	if err != nil || claim.Outcome != AgentStepClaimAcquired {
		t.Fatalf("ClaimStep() = %+v error %v", claim, err)
	}
	changed, err := journal.CompleteStep(ctx, AgentStepCompletion{UserID: fx.owner, RunID: run.ID, StepID: "read-base", Attempt: 1, LeaseToken: "step-lease", OutputRef: "artifact:" + fx.artifact.ID, ResultCheckpoint: `{"ok":true}`, EvidenceRefs: `[]`, MetricsJSON: `{}`, UsageSource: model.AgentCallUsageActual, ContextUsageSource: model.AgentCallUsageActual, Now: now.Add(time.Second)})
	if err != nil || !changed {
		t.Fatalf("CompleteStep() changed=%v error=%v", changed, err)
	}
	records, err := journal.GetExecution(ctx, fx.owner, run.ID)
	if err != nil || records == nil || len(records.Steps) != 1 || len(records.ToolCalls) != 1 {
		t.Fatalf("GetExecution() = %+v error %v", records, err)
	}
}

func TestArtifactEditUndoPreservesLaterUnrelatedHumanEdit(t *testing.T) {
	fx := newArtifactEditFixture(t)
	ctx := context.Background()
	req, run := fx.requestAndRun("undo-request", "undo-request-hash", "undo")
	if _, err := fx.repo.SubmitEdit(ctx, req, run); err != nil {
		t.Fatal(err)
	}
	claimed, err := fx.repo.Claim(ctx, run.ID, "undo-lease", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	var baseBody artifact.Body
	if err = json.Unmarshal([]byte(fx.base.BodyJSON), &baseBody); err != nil {
		t.Fatal(err)
	}
	content := "Agent 修改内容"
	patch := artifact.Patch{SchemaVersion: 1, ArtifactID: fx.artifact.ID, BaseVersionID: fx.base.ID, BaseVersion: fx.base.Version, Basis: artifact.PatchBasisUserInstruction, EvidenceIDs: []string{}, Operations: []artifact.PatchOperation{{Op: artifact.PatchOpUpdateBlock, BlockID: "install", ExpectedHash: artifact.BlockHash(baseBody.Blocks[0]), Content: &content}}}
	auth := artifact.PatchAuthorization{OperationID: "edit-operation-undo", ArtifactID: fx.artifact.ID, BaseVersionID: fx.base.ID, BaseVersion: fx.base.Version, SelectedBlockIDs: []string{"install"}, AllowedEvidenceIDs: map[string]bool{"evidence-1": true}}
	runID, requestID := run.ID, req.ID
	op := &model.ArtifactEditOperation{ID: auth.OperationID, UserID: fx.owner, ArtifactID: fx.artifact.ID, RequestID: &requestID, RunID: &runID, Kind: model.ArtifactEditOperationKindEdit, BaseVersionID: fx.base.ID, BaseVersion: fx.base.Version, ManifestID: fx.manifest.ID, Summary: "修改安装内容"}
	committed, resultVersion, err := fx.repo.CommitEdit(ctx, fx.owner, op, patch, auth, "undo-lease", claimed.RunLeaseEpoch, fx.sourceReader())
	if err != nil {
		t.Fatal(err)
	}
	var later artifact.Body
	if err = json.Unmarshal([]byte(resultVersion.BodyJSON), &later); err != nil {
		t.Fatal(err)
	}
	later.Title = "用户后来修改的标题"
	if err = fx.repo.Save(ctx, fx.owner, fx.artifact.ID, resultVersion.Version, &later, ""); err != nil {
		t.Fatalf("Save() later human edit error = %v", err)
	}
	current, _, err := fx.repo.Get(ctx, fx.owner, fx.artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	undone, undoVersion, err := fx.repo.UndoEdit(ctx, fx.owner, committed.ID, current.HeadVersion, "undo-key", "undo-hash", fx.sourceReader())
	if err != nil {
		t.Fatalf("UndoEdit() error = %v", err)
	}
	if undone.UndoVersionID == nil || *undone.UndoVersionID != undoVersion.ID || undoVersion.Origin != "undo" || undoVersion.EditOperationID == nil {
		t.Fatalf("UndoEdit() = operation %+v version %+v", undone, undoVersion)
	}
	var undoBody artifact.Body
	if err = json.Unmarshal([]byte(undoVersion.BodyJSON), &undoBody); err != nil {
		t.Fatal(err)
	}
	if undoBody.Title != later.Title || undoBody.Blocks[0].Content != baseBody.Blocks[0].Content {
		t.Fatalf("undo body = %+v, want later title and original content", undoBody)
	}
	replayed, replayVersion, err := fx.repo.UndoEdit(ctx, fx.owner, committed.ID, current.HeadVersion, "undo-key", "undo-hash", fx.sourceReader())
	if err != nil || replayed.ID != committed.ID || replayVersion.ID != undoVersion.ID {
		t.Fatalf("UndoEdit() replay = operation %+v version %+v error %v", replayed, replayVersion, err)
	}
}

type crossSourceUndoFixture struct {
	*artifactEditFixture
	operation       *model.ArtifactEditOperation
	current         *model.ArtifactVersion
	currentManifest model.SourceManifest
	read            SourceReader
}

func newCrossSourceUndoFixture(t *testing.T) *crossSourceUndoFixture {
	t.Helper()
	fx := newArtifactEditFixture(t)
	ctx := context.Background()
	req, run := fx.requestAndRun("cross-source-undo-request", "cross-source-undo-hash", "cross-source-undo")
	if _, err := fx.repo.SubmitEdit(ctx, req, run); err != nil {
		t.Fatal(err)
	}
	claimed, err := fx.repo.Claim(ctx, run.ID, "cross-source-undo-lease", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	var baseBody artifact.Body
	if err = json.Unmarshal([]byte(fx.base.BodyJSON), &baseBody); err != nil {
		t.Fatal(err)
	}
	content := "Agent 修改内容"
	patch := artifact.Patch{SchemaVersion: 1, ArtifactID: fx.artifact.ID, BaseVersionID: fx.base.ID, BaseVersion: fx.base.Version, Basis: artifact.PatchBasisUserInstruction, EvidenceIDs: []string{}, Operations: []artifact.PatchOperation{{Op: artifact.PatchOpUpdateBlock, BlockID: "install", ExpectedHash: artifact.BlockHash(baseBody.Blocks[0]), Content: &content}}}
	auth := artifact.PatchAuthorization{OperationID: "edit-operation-cross-source-undo", ArtifactID: fx.artifact.ID, BaseVersionID: fx.base.ID, BaseVersion: fx.base.Version, SelectedBlockIDs: []string{"install"}, AllowedEvidenceIDs: map[string]bool{"evidence-1": true}}
	runID, requestID := run.ID, req.ID
	operation := &model.ArtifactEditOperation{ID: auth.OperationID, UserID: fx.owner, ArtifactID: fx.artifact.ID, RequestID: &requestID, RunID: &runID, Kind: model.ArtifactEditOperationKindEdit, BaseVersionID: fx.base.ID, BaseVersion: fx.base.Version, ManifestID: fx.manifest.ID, Summary: "修改安装内容"}
	operation, resultVersion, err := fx.repo.CommitEdit(ctx, fx.owner, operation, patch, auth, "cross-source-undo-lease", claimed.RunLeaseEpoch, fx.sourceReader())
	if err != nil {
		t.Fatal(err)
	}

	otherTask := model.VideoTask{ID: 43, UserID: fx.owner, FileMD5: "22222222222222222222222222222222", Filename: "other.mp4", Title: "Other", Status: model.TaskStatusCompleted}
	if err = fx.db.Create(&otherTask).Error; err != nil {
		t.Fatal(err)
	}
	otherManifest := model.SourceManifest{ID: "manifest-other", UserID: fx.owner, SourceID: otherTask.ID, ContentHash: "other-source-hash", Title: "Other"}
	if err = fx.db.Create(&otherManifest).Error; err != nil {
		t.Fatal(err)
	}
	if err = fx.db.Create(&model.SourceSnapshotItem{ID: "evidence-other", ManifestID: otherManifest.ID, SourceID: otherTask.ID, SourceTitle: "Other", Content: "另一来源证据", ContentHash: "other-evidence-hash", Position: 0}).Error; err != nil {
		t.Fatal(err)
	}
	var later artifact.Body
	if err = json.Unmarshal([]byte(resultVersion.BodyJSON), &later); err != nil {
		t.Fatal(err)
	}
	later.Title = "另一来源的后续标题"
	later.Blocks[0].ClaimOrigin = "user"
	later.Blocks[0].EvidenceRefs = []artifact.Ref{}
	var current *model.ArtifactVersion
	if err = fx.db.Transaction(func(tx *gorm.DB) error {
		locked, lockErr := ownedArtifact(tx, fx.owner, fx.artifact.ID, true)
		if lockErr != nil {
			return lockErr
		}
		current, lockErr = insertRevision(tx, locked, later, otherManifest.ID, "generated", resultVersion.Version, nil, true)
		return lockErr
	}); err != nil {
		t.Fatal(err)
	}
	read := func(_ context.Context, _ *Repositories, owner, sourceID int64) (string, []model.SourceSnapshotItem, error) {
		if owner != fx.owner {
			return "", nil, artifact.Err("not_found", 404)
		}
		switch sourceID {
		case fx.taskID:
			return fx.manifest.ContentHash, nil, nil
		case otherTask.ID:
			return otherManifest.ContentHash, nil, nil
		default:
			return "", nil, artifact.Err("source_deleted", 410)
		}
	}
	return &crossSourceUndoFixture{artifactEditFixture: fx, operation: operation, current: current, currentManifest: otherManifest, read: read}
}

func TestArtifactEditUndoGatesCrossSourceCurrentManifestAndReplay(t *testing.T) {
	t.Run("first undo", func(t *testing.T) {
		fx := newCrossSourceUndoFixture(t)
		ctx := context.Background()
		if err := fx.repo.RevokeSource(fx.currentManifest.SourceID); err != nil {
			t.Fatal(err)
		}
		var before int64
		if err := fx.db.Model(&model.ArtifactVersion{}).Where("artifact_id=?", fx.artifact.ID).Count(&before).Error; err != nil {
			t.Fatal(err)
		}
		_, _, err := fx.repo.UndoEdit(ctx, fx.owner, fx.operation.ID, fx.current.Version, "cross-source-undo", "cross-source-undo-hash", fx.read)
		if artifactErrorCodeForTest(err) != "source_deleted" {
			t.Fatalf("UndoEdit() error = %v, want source_deleted", err)
		}
		var after int64
		var stored model.Artifact
		if err = fx.db.Model(&model.ArtifactVersion{}).Where("artifact_id=?", fx.artifact.ID).Count(&after).Error; err != nil {
			t.Fatal(err)
		}
		if err = fx.db.Where("id=?", fx.artifact.ID).First(&stored).Error; err != nil {
			t.Fatal(err)
		}
		if after != before || stored.HeadVersion != fx.current.Version || stored.CurrentVersionID == nil || *stored.CurrentVersionID != fx.current.ID {
			t.Fatalf("revoked-source undo wrote state: versions=%d->%d artifact=%+v", before, after, stored)
		}
	})

	t.Run("idempotency replay", func(t *testing.T) {
		fx := newCrossSourceUndoFixture(t)
		ctx := context.Background()
		key, hash := "cross-source-replay", "cross-source-replay-hash"
		_, undoVersion, err := fx.repo.UndoEdit(ctx, fx.owner, fx.operation.ID, fx.current.Version, key, hash, fx.read)
		if err != nil || undoVersion == nil || undoVersion.ManifestID != fx.currentManifest.ID {
			t.Fatalf("initial UndoEdit() version=%+v err=%v", undoVersion, err)
		}
		if err = fx.repo.RevokeSource(fx.currentManifest.SourceID); err != nil {
			t.Fatal(err)
		}
		_, _, err = fx.repo.UndoEdit(ctx, fx.owner, fx.operation.ID, fx.current.Version, key, hash, fx.read)
		if artifactErrorCodeForTest(err) != "source_deleted" {
			t.Fatalf("UndoEdit() replay error = %v, want source_deleted", err)
		}
	})
}

func TestArtifactEditSourceRevocationCancelsRunAndClearsPrivateJournal(t *testing.T) {
	fx := newArtifactEditFixture(t)
	ctx := context.Background()
	req, run := fx.requestAndRun("revoke-request", "revoke-request-hash", "revoke")
	if _, err := fx.repo.SubmitEdit(ctx, req, run); err != nil {
		t.Fatal(err)
	}
	journal := NewAgentExecutionRepository(fx.db)
	now := time.Now().UTC()
	claim, err := journal.ClaimStep(ctx, AgentStepClaimRequest{UserID: fx.owner, RunID: run.ID, StepID: "inspect", Attempt: 1, Sequence: 1, Kind: "tool", Action: "inspect_artifact_evidence", InputSummary: `{}`, ArgumentsDigest: artifact.Hash(`{}`), CallDigest: artifact.Hash(`inspect`), ToolName: "inspect_artifact_evidence", CallKind: model.AgentCallKindTool, ReplaySafe: true, LeaseToken: "inspect-lease", Now: now, LeaseUntil: now.Add(time.Minute)})
	if err != nil || claim.Outcome != AgentStepClaimAcquired {
		t.Fatalf("ClaimStep() = %+v error %v", claim, err)
	}
	if changed, completeErr := journal.CompleteStep(ctx, AgentStepCompletion{UserID: fx.owner, RunID: run.ID, StepID: "inspect", Attempt: 1, LeaseToken: "inspect-lease", OutputRef: "evidence", ResultCheckpoint: `{"private":"snapshot"}`, EvidenceRefs: `["evidence-1"]`, MetricsJSON: `{}`, Now: now.Add(time.Second)}); completeErr != nil || !changed {
		t.Fatalf("CompleteStep() changed=%v error=%v", changed, completeErr)
	}
	if err = fx.repo.RevokeSource(fx.taskID); err != nil {
		t.Fatal(err)
	}
	stored, _, err := fx.repo.EditRun(ctx, fx.owner, run.ID)
	if err != nil || stored.CancelRequestedAt == nil {
		t.Fatalf("revoked run = %+v error %v", stored, err)
	}
	records, err := journal.GetExecution(ctx, fx.owner, run.ID)
	if err != nil || len(records.Steps) != 1 || len(records.ToolCalls) != 1 || records.Steps[0].Status != model.AgentStepStatusCompleted || records.ToolCalls[0].Status != model.AgentToolCallStatusCompleted || records.Steps[0].ResultCheckpoint != "" || records.ToolCalls[0].ResultCheckpoint != "" {
		t.Fatalf("revoked journal = %+v error %v", records, err)
	}
	if _, err = fx.repo.EditContext(ctx, fx.owner, run.ID); artifactErrorCodeForTest(err) != "source_deleted" {
		t.Fatalf("EditContext() after revocation error = %v", err)
	}
}

func TestArtifactEditSourceRevocationRejectsLateStepCompletion(t *testing.T) {
	fx := newArtifactEditFixture(t)
	ctx := context.Background()
	req, run := fx.requestAndRun("late-revoke-request", "late-revoke-request-hash", "late revoke")
	if _, err := fx.repo.SubmitEdit(ctx, req, run); err != nil {
		t.Fatal(err)
	}
	journal := NewAgentExecutionRepository(fx.db)
	now := time.Now().UTC()
	claim, err := journal.ClaimStep(ctx, AgentStepClaimRequest{UserID: fx.owner, RunID: run.ID, StepID: "inspect", Attempt: 1, Sequence: 1, Kind: "tool", Action: "inspect_artifact_evidence", InputSummary: `{}`, ArgumentsDigest: artifact.Hash(`{}`), CallDigest: artifact.Hash(`inspect`), ToolName: "inspect_artifact_evidence", CallKind: model.AgentCallKindTool, ReplaySafe: true, LeaseToken: "inspect-lease", Now: now, LeaseUntil: now.Add(time.Minute)})
	if err != nil || claim.Outcome != AgentStepClaimAcquired {
		t.Fatalf("ClaimStep() = %+v error %v", claim, err)
	}
	if err = fx.repo.RevokeSource(fx.taskID); err != nil {
		t.Fatal(err)
	}
	changed, completeErr := journal.CompleteStep(ctx, AgentStepCompletion{UserID: fx.owner, RunID: run.ID, StepID: "inspect", Attempt: 1, LeaseToken: "inspect-lease", OutputRef: "evidence", ResultCheckpoint: `{"private":"late snapshot"}`, EvidenceRefs: `["evidence-1"]`, MetricsJSON: `{}`, Now: now.Add(time.Second)})
	if completeErr != nil || changed {
		t.Fatalf("late CompleteStep() changed=%v error=%v, want unchanged without error", changed, completeErr)
	}
	records, err := journal.GetExecution(ctx, fx.owner, run.ID)
	if err != nil || len(records.Steps) != 1 || len(records.ToolCalls) != 1 {
		t.Fatalf("GetExecution() = %+v error %v", records, err)
	}
	step, call := records.Steps[0], records.ToolCalls[0]
	if step.Status != model.AgentStepStatusFailed || call.Status != model.AgentToolCallStatusFailed {
		t.Fatalf("revoked statuses = step %q tool %q, want failed", step.Status, call.Status)
	}
	if step.ErrorCode != "source_deleted" || call.ErrorCode != "source_deleted" {
		t.Fatalf("revoked error codes = step %q tool %q, want source_deleted", step.ErrorCode, call.ErrorCode)
	}
	if step.LeaseToken != "" || step.LeaseExpiresAt != nil || step.FinishedAt == nil || call.FinishedAt == nil {
		t.Fatalf("revoked leases/timestamps = step %+v tool %+v", step, call)
	}
	if step.OutputRef != "" || call.OutputRef != "" || step.ResultCheckpoint != "" || call.ResultCheckpoint != "" || step.ResultDigest != "" || call.ResultDigest != "" {
		t.Fatalf("late completion restored private journal data: step %+v tool %+v", step, call)
	}
}

func TestArtifactEditAnswerModeCannotPersistOrCommitPatch(t *testing.T) {
	fx := newArtifactEditFixture(t)
	ctx := context.Background()
	req, run := fx.requestAndRun("answer-request", "answer-request-hash", "answer")
	req.Mode, run.Mode = "answer", "answer"
	req.ToolPolicyJSON = `["read_artifact","answer_artifact_question"]`
	if _, err := fx.repo.SubmitEdit(ctx, req, run); err != nil {
		t.Fatal(err)
	}
	claimed, err := fx.repo.Claim(ctx, run.ID, "answer-lease", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	var base artifact.Body
	if err = json.Unmarshal([]byte(fx.base.BodyJSON), &base); err != nil {
		t.Fatal(err)
	}
	content := "越权修改"
	patch := artifact.Patch{SchemaVersion: 1, ArtifactID: fx.artifact.ID, BaseVersionID: fx.base.ID, BaseVersion: fx.base.Version, Basis: artifact.PatchBasisUserInstruction, EvidenceIDs: []string{}, Operations: []artifact.PatchOperation{{Op: artifact.PatchOpUpdateBlock, BlockID: "install", ExpectedHash: artifact.BlockHash(base.Blocks[0]), Content: &content}}}
	auth := artifact.PatchAuthorization{OperationID: "edit-operation-answer", ArtifactID: fx.artifact.ID, BaseVersionID: fx.base.ID, BaseVersion: fx.base.Version, SelectedBlockIDs: []string{"install"}, AllowedEvidenceIDs: map[string]bool{"evidence-1": true}}
	runID, requestID := run.ID, req.ID
	op := &model.ArtifactEditOperation{ID: auth.OperationID, UserID: fx.owner, ArtifactID: fx.artifact.ID, RequestID: &requestID, RunID: &runID, Kind: model.ArtifactEditOperationKindEdit, BaseVersionID: fx.base.ID, BaseVersion: fx.base.Version, ManifestID: fx.manifest.ID}
	if _, err = fx.repo.PersistEditProposal(ctx, fx.owner, op, patch, auth, "answer-lease", claimed.RunLeaseEpoch); artifactErrorCodeForTest(err) != "target_scope_mismatch" {
		t.Fatalf("answer PersistEditProposal error = %v", err)
	}
	if _, _, err = fx.repo.CommitEdit(ctx, fx.owner, op, patch, auth, "answer-lease", claimed.RunLeaseEpoch, fx.sourceReader()); artifactErrorCodeForTest(err) != "target_scope_mismatch" {
		t.Fatalf("answer CommitEdit error = %v", err)
	}
	versions, err := fx.repo.Versions(ctx, fx.owner, fx.artifact.ID)
	if err != nil || len(versions) != 1 {
		t.Fatalf("answer mode versions=%d error=%v", len(versions), err)
	}
}

func TestArtifactEditStaleHeadRejectsCommitWithoutOperation(t *testing.T) {
	fx := newArtifactEditFixture(t)
	ctx := context.Background()
	req, run := fx.requestAndRun("stale-request", "stale-request-hash", "stale")
	if _, err := fx.repo.SubmitEdit(ctx, req, run); err != nil {
		t.Fatal(err)
	}
	claimed, err := fx.repo.Claim(ctx, run.ID, "stale-lease", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	var base artifact.Body
	if err = json.Unmarshal([]byte(fx.base.BodyJSON), &base); err != nil {
		t.Fatal(err)
	}
	human := base
	human.Title = "人工先改标题"
	if err = fx.repo.Save(ctx, fx.owner, fx.artifact.ID, fx.base.Version, &human, ""); err != nil {
		t.Fatal(err)
	}
	content := "过期 Agent 修改"
	patch := artifact.Patch{SchemaVersion: 1, ArtifactID: fx.artifact.ID, BaseVersionID: fx.base.ID, BaseVersion: fx.base.Version, Basis: artifact.PatchBasisUserInstruction, EvidenceIDs: []string{}, Operations: []artifact.PatchOperation{{Op: artifact.PatchOpUpdateBlock, BlockID: "install", ExpectedHash: artifact.BlockHash(base.Blocks[0]), Content: &content}}}
	auth := artifact.PatchAuthorization{OperationID: "edit-operation-stale", ArtifactID: fx.artifact.ID, BaseVersionID: fx.base.ID, BaseVersion: fx.base.Version, SelectedBlockIDs: []string{"install"}, AllowedEvidenceIDs: map[string]bool{"evidence-1": true}}
	runID, requestID := run.ID, req.ID
	op := &model.ArtifactEditOperation{ID: auth.OperationID, UserID: fx.owner, ArtifactID: fx.artifact.ID, RequestID: &requestID, RunID: &runID, Kind: model.ArtifactEditOperationKindEdit, BaseVersionID: fx.base.ID, BaseVersion: fx.base.Version, ManifestID: fx.manifest.ID}
	if _, _, err = fx.repo.CommitEdit(ctx, fx.owner, op, patch, auth, "stale-lease", claimed.RunLeaseEpoch, fx.sourceReader()); artifactErrorCodeForTest(err) != "version_conflict" {
		t.Fatalf("stale CommitEdit error = %v", err)
	}
	if _, err = fx.repo.EditOperation(ctx, fx.owner, op.ID); artifactErrorCodeForTest(err) != "not_found" {
		t.Fatalf("stale operation lookup error = %v", err)
	}
	versions, err := fx.repo.Versions(ctx, fx.owner, fx.artifact.ID)
	if err != nil || len(versions) != 2 {
		t.Fatalf("stale commit versions=%d error=%v", len(versions), err)
	}
}
