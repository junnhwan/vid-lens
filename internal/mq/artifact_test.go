package mq

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	amqp "github.com/rabbitmq/amqp091-go"
	"gorm.io/gorm"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

type recordingArtifactExecutor struct {
	generation []string
	edits      []string
}

func (e *recordingArtifactExecutor) ExecuteArtifact(_ context.Context, runID string) error {
	e.generation = append(e.generation, runID)
	return nil
}

func (e *recordingArtifactExecutor) ExecuteArtifactEdit(_ context.Context, runID string) error {
	e.edits = append(e.edits, runID)
	return nil
}

func TestArtifactQueueSubjectsAreStable(t *testing.T) {
	for _, tc := range []struct {
		queue   string
		subject string
	}{
		{queue: ArtifactQueue, subject: artifactGenerationSubject},
		{queue: ArtifactEditQueue, subject: model.AgentRunSubjectArtifactEdit},
	} {
		got, err := artifactSubjectForQueue(tc.queue)
		if err != nil || got != tc.subject {
			t.Errorf("artifactSubjectForQueue(%q) = %q, %v; want %q", tc.queue, got, err, tc.subject)
		}
	}
	if _, err := artifactSubjectForQueue("vidlens.artifact.unknown"); err == nil {
		t.Fatal("unknown artifact queue was accepted")
	}
}

func TestArtifactWorkerRejectsWrongQueueBeforePublish(t *testing.T) {
	worker, _ := artifactQueueTestWorker(t)
	payload := ArtifactDispatch{SchemaVersion: 1, RunID: "edit-run", DispatchID: "edit-dispatch", TraceID: "edit-run"}
	err := worker.publish(context.Background(), ArtifactQueue, model.AgentRunSubjectArtifactEdit, payload)
	if !errors.Is(err, errArtifactQueueMismatch) {
		t.Fatalf("publishing edit run to generation queue error = %v, want queue mismatch", err)
	}
	payload.RunID, payload.DispatchID, payload.TraceID = "generation-run", "generation-dispatch", "generation-run"
	err = worker.publish(context.Background(), ArtifactEditQueue, artifactGenerationSubject, payload)
	if !errors.Is(err, errArtifactQueueMismatch) {
		t.Fatalf("publishing generation run to edit queue error = %v, want queue mismatch", err)
	}
}

func TestArtifactWorkerAcknowledgesStaleEditFromGenerationQueueWithoutExecuting(t *testing.T) {
	worker, executor := artifactQueueTestWorker(t)
	err := worker.handle(context.Background(), ArtifactQueue, artifactDelivery(t, "edit-run", "legacy-generation-dispatch"))
	if err != nil {
		t.Fatalf("stale edit delivery from generation queue error = %v, want ACK-safe nil", err)
	}
	if len(executor.generation) != 0 || len(executor.edits) != 0 {
		t.Fatalf("stale cross-queue delivery invoked executor: %+v", executor)
	}
}

func TestArtifactWorkerConsumesOnlyMatchingQueueExecutor(t *testing.T) {
	worker, executor := artifactQueueTestWorker(t)
	ctx := context.Background()

	edit := artifactDelivery(t, "edit-run", "edit-dispatch")
	if err := worker.handle(ctx, ArtifactQueue, edit); err != nil {
		t.Fatalf("generation handler stale edit error = %v, want ACK-safe nil", err)
	}
	if len(executor.generation) != 0 || len(executor.edits) != 0 {
		t.Fatalf("wrong-queue edit invoked executor: %+v", executor)
	}
	if err := worker.handle(ctx, ArtifactEditQueue, edit); err != nil {
		t.Fatalf("edit handler error = %v", err)
	}
	if err := worker.handle(ctx, ArtifactEditQueue, edit); err != nil {
		t.Fatalf("duplicate edit delivery error = %v", err)
	}
	if len(executor.edits) != 2 || executor.edits[0] != "edit-run" || len(executor.generation) != 0 {
		t.Fatalf("edit deliveries invoked wrong executor: %+v", executor)
	}

	generation := artifactDelivery(t, "generation-run", "generation-dispatch")
	if err := worker.handle(ctx, ArtifactEditQueue, generation); err != nil {
		t.Fatalf("edit handler stale generation error = %v, want ACK-safe nil", err)
	}
	if err := worker.handle(ctx, ArtifactQueue, generation); err != nil {
		t.Fatalf("generation handler error = %v", err)
	}
	if len(executor.generation) != 1 || executor.generation[0] != "generation-run" || len(executor.edits) != 2 {
		t.Fatalf("generation delivery invoked wrong executor: %+v", executor)
	}
}

func artifactQueueTestWorker(t *testing.T) (*ArtifactWorker, *recordingArtifactExecutor) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&model.AgentRun{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	runs := []model.AgentRun{
		artifactQueueTestRun("generation-run", artifactGenerationSubject, now),
		artifactQueueTestRun("edit-run", model.AgentRunSubjectArtifactEdit, now),
	}
	if err = db.Create(&runs).Error; err != nil {
		t.Fatal(err)
	}
	executor := &recordingArtifactExecutor{}
	return &ArtifactWorker{repo: repository.NewArtifactRepository(db), svc: executor}, executor
}

func artifactQueueTestRun(id, subject string, now time.Time) model.AgentRun {
	return model.AgentRun{
		ID: id, UserID: 7, SubjectKind: subject, SubjectID: id + "-request", ExecutionKind: "artifact", RecipeVersion: "test-v1",
		ScopeType: "video", Goal: "test", Mode: "artifact", ProfileSnapshot: `{}`, PolicySnapshot: `{}`, BudgetSnapshot: `{}`,
		Status: model.AgentRunStatusPending, CreatedAt: now, UpdatedAt: now,
	}
}

func artifactDelivery(t *testing.T, runID, dispatchID string) amqp.Delivery {
	t.Helper()
	body, err := json.Marshal(ArtifactDispatch{SchemaVersion: 1, RunID: runID, DispatchID: dispatchID, TraceID: runID})
	if err != nil {
		t.Fatal(err)
	}
	return amqp.Delivery{Body: body}
}
