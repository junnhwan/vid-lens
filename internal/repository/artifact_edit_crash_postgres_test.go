package repository

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

// This kills a real OS process after its PostgreSQL transaction commits and
// before its journal checkpoint. It is intentionally independent of RabbitMQ:
// queue redelivery is covered by the dispatcher/worker integration tests.
func TestPostgresArtifactEditHardKillAfterCommitBeforeCheckpoint(t *testing.T) {
	if os.Getenv("VIDLENS_EDIT_CRASH_CHILD") == "1" {
		db := openPostgresRepositoryPeer(t, os.Getenv("VIDLENS_EDIT_CRASH_SCOPED_DSN"))
		fx := &artifactEditFixture{db: db, repo: NewArtifactRepository(db), owner: 17, taskID: 42}
		run, req, err := fx.repo.EditRun(context.Background(), fx.owner, "edit-run-hard-kill")
		if err != nil {
			t.Fatal(err)
		}
		fx.artifact, fx.base, err = fx.repo.Get(context.Background(), fx.owner, req.ArtifactID)
		if err != nil {
			t.Fatal(err)
		}
		if err = db.First(&fx.manifest, "id=?", req.ManifestID).Error; err != nil {
			t.Fatal(err)
		}
		patch, auth := postgresEditPatch(t, fx, "pg-hard-kill-op", "survives process death")
		if _, _, err = fx.repo.CommitEdit(context.Background(), fx.owner, postgresEditOperation(fx, req, auth.OperationID), patch, auth, "hard-kill-lease", run.RunLeaseEpoch, fx.sourceReader()); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(os.Getenv("VIDLENS_EDIT_CRASH_MARKER"), []byte("committed"), 0600); err != nil {
			t.Fatal(err)
		}
		// No deferred cleanup/checkpoint can run when the parent kills us here.
		for {
			time.Sleep(time.Second)
		}
	}

	testDB := openPostgresRepositoryTestDB(t)
	fx := newArtifactEditFixtureOnDB(t, testDB.db)
	ctx := context.Background()
	req, run := fx.requestAndRun("hard-kill", artifact.Hash("hard-kill"), "hard-kill")
	if _, err := fx.repo.SubmitEdit(ctx, req, run); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.repo.Claim(ctx, run.ID, "hard-kill-lease", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	journal := NewAgentExecutionRepository(testDB.db)
	claimed, err := journal.ClaimStep(ctx, AgentStepClaimRequest{UserID: fx.owner, RunID: run.ID, StepID: "commit", Attempt: 1, Sequence: 1, Kind: "tool", Action: "commit_artifact_patch", InputSummary: `{}`, ArgumentsDigest: artifact.Hash("hard-kill-op"), CallDigest: artifact.Hash("hard-kill-call"), ToolName: "commit_artifact_patch", CallKind: model.AgentCallKindTool, ReplaySafe: false, LeaseToken: "step-lease", Now: now, LeaseUntil: now.Add(time.Minute)})
	if err != nil || claimed.Outcome != AgentStepClaimAcquired {
		t.Fatalf("claim step: %+v, %v", claimed, err)
	}
	marker := filepath.Join(t.TempDir(), "committed")
	cmd := exec.Command(os.Args[0], "-test.run=^TestPostgresArtifactEditHardKillAfterCommitBeforeCheckpoint$")
	cmd.Env = append(os.Environ(), "VIDLENS_EDIT_CRASH_CHILD=1", "VIDLENS_EDIT_CRASH_SCOPED_DSN="+testDB.scopedDSN, "VIDLENS_EDIT_CRASH_MARKER="+marker)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	deadline := time.After(20 * time.Second)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
waiting:
	for {
		select {
		case err = <-done:
			t.Fatalf("child exited before kill: %v\n%s", err, output.String())
		case <-deadline:
			_ = cmd.Process.Kill()
			<-done
			t.Fatalf("child did not reach commit: %s", output.String())
		case <-ticker.C:
			if _, err = os.Stat(marker); err == nil {
				break waiting
			}
		}
	}
	before, err := journal.GetExecution(ctx, fx.owner, run.ID)
	if err != nil || len(before.ToolCalls) != 1 || before.ToolCalls[0].Status != model.AgentToolCallStatusRunning || before.ToolCalls[0].ResultCheckpoint != "" {
		t.Fatalf("did not stop before checkpoint: %+v %v", before, err)
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err == nil {
		t.Fatal("expected abrupt process termination")
	}
	t.Log("child was killed after durable commit with the tool checkpoint still empty")
	peer := NewArtifactRepository(openPostgresRepositoryPeer(t, testDB.scopedDSN))
	for i := 0; i < 2; i++ {
		operation, recovered, recoverErr := peer.RecoverCommittedEdit(ctx, run.ID)
		if recoverErr != nil || !recovered || operation.RunFinalizedAt == nil {
			t.Fatalf("recovery %d: %+v %v %v", i, operation, recovered, recoverErr)
		}
	}
	versions, err := peer.Versions(ctx, fx.owner, fx.artifact.ID)
	if err != nil || len(versions) != 2 {
		t.Fatalf("replay created duplicate version: %d %v", len(versions), err)
	}
	finalRun, _, err := peer.EditRun(ctx, fx.owner, run.ID)
	if err != nil || finalRun.Status != model.AgentRunStatusCompleted || finalRun.ResultVersionID == nil {
		t.Fatalf("recovered run: %+v %v", finalRun, err)
	}
	after, err := journal.GetExecution(ctx, fx.owner, run.ID)
	if err != nil || len(after.ToolCalls) != 1 || after.ToolCalls[0].Status != model.AgentToolCallStatusCompleted || after.ToolCalls[0].ResultCheckpoint == "" {
		t.Fatalf("recovered checkpoint: %+v %v", after, err)
	}
}
