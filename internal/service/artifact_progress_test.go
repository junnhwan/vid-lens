package service

import (
	"context"
	"testing"
	"time"

	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

func TestArtifactGlobalCallPreservesOrganizingProgress(t *testing.T) {
	svc, db, _ := artifactFixture(t, artifactModelResponse)
	ctx := context.Background()
	run, err := svc.Submit(ctx, 7, "progress", artifactRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := svc.repos.Artifact.Claim(ctx, run.ID, "worker", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repos.Artifact.Progress(ctx, run.ID, "worker", claimed.RunLeaseEpoch, "organizing", 1, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.repos.Artifact.BeginCall(ctx, run.ID, "worker", claimed.RunLeaseEpoch, "study-v2.global.0", artifact.Hash("input"), 200, 100); err != nil {
		t.Fatal(err)
	}
	var current model.AgentRun
	if err := db.First(&current, "id=?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if current.Stage != "organizing" {
		t.Fatalf("global provider call regressed stage to %q", current.Stage)
	}
}
