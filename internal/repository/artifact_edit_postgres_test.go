package repository

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

func postgresEditPatch(t *testing.T, fx *artifactEditFixture, operationID, content string) (artifact.Patch, artifact.PatchAuthorization) {
	t.Helper()
	var base artifact.Body
	if err := json.Unmarshal([]byte(fx.base.BodyJSON), &base); err != nil {
		t.Fatal(err)
	}
	patch := artifact.Patch{SchemaVersion: 1, ArtifactID: fx.artifact.ID, BaseVersionID: fx.base.ID, BaseVersion: fx.base.Version, Basis: artifact.PatchBasisUserInstruction, EvidenceIDs: []string{}, Operations: []artifact.PatchOperation{{Op: artifact.PatchOpUpdateBlock, BlockID: "install", ExpectedHash: artifact.BlockHash(base.Blocks[0]), Content: &content}}}
	auth := artifact.PatchAuthorization{OperationID: operationID, ArtifactID: fx.artifact.ID, BaseVersionID: fx.base.ID, BaseVersion: fx.base.Version, SelectedBlockIDs: []string{"install"}, AllowedEvidenceIDs: map[string]bool{"evidence-1": true}}
	return patch, auth
}

func postgresEditOperation(fx *artifactEditFixture, req *model.ArtifactEditRequest, operationID string) *model.ArtifactEditOperation {
	runID, requestID := req.RunID, req.ID
	return &model.ArtifactEditOperation{ID: operationID, UserID: fx.owner, ArtifactID: fx.artifact.ID, RequestID: &requestID, RunID: &runID, Kind: model.ArtifactEditOperationKindEdit, BaseVersionID: fx.base.ID, BaseVersion: fx.base.Version, ManifestID: fx.manifest.ID, Summary: "PG 修改"}
}

func TestPostgresArtifactEditReplayPrecedesHeadCASAndStaleCommitConflicts(t *testing.T) {
	testDB := openPostgresRepositoryTestDB(t)
	fx := newArtifactEditFixtureOnDB(t, testDB.db)
	ctx := context.Background()
	requestHash := artifact.Hash("pg-replay-request")
	req, run := fx.requestAndRun("pg-replay-key", requestHash, "pg-replay")
	accepted, err := fx.repo.SubmitEdit(ctx, req, run)
	if err != nil {
		t.Fatal(err)
	}
	var human artifact.Body
	if err = json.Unmarshal([]byte(fx.base.BodyJSON), &human); err != nil {
		t.Fatal(err)
	}
	human.Title = "人工移动 head"
	if err = fx.repo.Save(ctx, fx.owner, fx.artifact.ID, fx.base.Version, &human, ""); err != nil {
		t.Fatal(err)
	}
	replay, replayRun := fx.requestAndRun("pg-replay-key", requestHash, "pg-replay-second")
	replayed, err := fx.repo.SubmitEdit(ctx, replay, replayRun)
	if err != nil || replayed.ID != accepted.ID {
		t.Fatalf("SubmitEdit replay = %+v error %v", replayed, err)
	}
	changed, changedRun := fx.requestAndRun("pg-replay-key", artifact.Hash("different-request"), "pg-replay-changed")
	if _, err = fx.repo.SubmitEdit(ctx, changed, changedRun); artifactErrorCodeForTest(err) != "idempotency_conflict" {
		t.Fatalf("changed replay error = %v", err)
	}
	claimed, err := fx.repo.Claim(ctx, run.ID, "pg-stale-lease", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	patch, auth := postgresEditPatch(t, fx, "pg-op-stale", "stale")
	_, _, err = fx.repo.CommitEdit(ctx, fx.owner, postgresEditOperation(fx, req, auth.OperationID), patch, auth, "pg-stale-lease", claimed.RunLeaseEpoch, fx.sourceReader())
	if artifactErrorCodeForTest(err) != "version_conflict" {
		t.Fatalf("stale CommitEdit error = %v", err)
	}
}

func TestPostgresArtifactEditCommitCancelCompetitionHasOneWinner(t *testing.T) {
	testDB := openPostgresRepositoryTestDB(t)
	fx := newArtifactEditFixtureOnDB(t, testDB.db)
	ctx := context.Background()
	req, run := fx.requestAndRun("pg-race-key", artifact.Hash("pg-race-request"), "pg-race")
	if _, err := fx.repo.SubmitEdit(ctx, req, run); err != nil {
		t.Fatal(err)
	}
	claimed, err := fx.repo.Claim(ctx, run.ID, "pg-race-lease", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	patch, auth := postgresEditPatch(t, fx, "pg-op-race", "race result")
	op := postgresEditOperation(fx, req, auth.OperationID)
	start := make(chan struct{})
	var commitErr, cancelErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, _, commitErr = fx.repo.CommitEdit(ctx, fx.owner, op, patch, auth, "pg-race-lease", claimed.RunLeaseEpoch, fx.sourceReader())
	}()
	go func() {
		defer wg.Done()
		<-start
		cancelErr = fx.repo.Cancel(ctx, fx.owner, run.ID, model.AgentRunSubjectArtifactEdit)
	}()
	close(start)
	wg.Wait()
	if cancelErr != nil {
		t.Fatalf("Cancel() error = %v", cancelErr)
	}
	stored, _, err := fx.repo.EditRun(ctx, fx.owner, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	versions, err := fx.repo.Versions(ctx, fx.owner, fx.artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	if commitErr == nil {
		if _, ok, recoverErr := fx.repo.RecoverCommittedEdit(ctx, run.ID); recoverErr != nil || !ok {
			t.Fatalf("RecoverCommittedEdit() ok=%v err=%v", ok, recoverErr)
		}
		stored, _, _ = fx.repo.EditRun(ctx, fx.owner, run.ID)
		if len(versions) != 2 || stored.Status != model.AgentRunStatusCompleted {
			t.Fatalf("commit winner: versions=%d run=%+v", len(versions), stored)
		}
		return
	}
	if !errors.Is(commitErr, artifact.ErrLease) || len(versions) != 1 || stored.Status != model.AgentRunStatusCancelled {
		t.Fatalf("cancel winner: commitErr=%v versions=%d run=%+v", commitErr, len(versions), stored)
	}
}

func TestPostgresArtifactEditUndoPreservesUnrelatedLaterEdit(t *testing.T) {
	testDB := openPostgresRepositoryTestDB(t)
	fx := newArtifactEditFixtureOnDB(t, testDB.db)
	ctx := context.Background()
	req, run := fx.requestAndRun("pg-undo-key", artifact.Hash("pg-undo-request"), "pg-undo")
	if _, err := fx.repo.SubmitEdit(ctx, req, run); err != nil {
		t.Fatal(err)
	}
	claimed, err := fx.repo.Claim(ctx, run.ID, "pg-undo-lease", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	patch, auth := postgresEditPatch(t, fx, "pg-op-undo", "agent content")
	op, result, err := fx.repo.CommitEdit(ctx, fx.owner, postgresEditOperation(fx, req, auth.OperationID), patch, auth, "pg-undo-lease", claimed.RunLeaseEpoch, fx.sourceReader())
	if err != nil {
		t.Fatal(err)
	}
	var later artifact.Body
	if err = json.Unmarshal([]byte(result.BodyJSON), &later); err != nil {
		t.Fatal(err)
	}
	later.Title = "later human title"
	if err = fx.repo.Save(ctx, fx.owner, fx.artifact.ID, result.Version, &later, ""); err != nil {
		t.Fatal(err)
	}
	current, _, err := fx.repo.Get(ctx, fx.owner, fx.artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, undone, err := fx.repo.UndoEdit(ctx, fx.owner, op.ID, current.HeadVersion, "pg-undo-action", "pg-undo-action-hash", fx.sourceReader())
	if err != nil {
		t.Fatal(err)
	}
	var body artifact.Body
	if err = json.Unmarshal([]byte(undone.BodyJSON), &body); err != nil {
		t.Fatal(err)
	}
	var base artifact.Body
	_ = json.Unmarshal([]byte(fx.base.BodyJSON), &base)
	if body.Title != later.Title || body.Blocks[0].Content != base.Blocks[0].Content {
		t.Fatalf("undo body = %+v", body)
	}
}

func artifactErrorCodeForTest(err error) string {
	var apiErr *artifact.Error
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}
	return ""
}
