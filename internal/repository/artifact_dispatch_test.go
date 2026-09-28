package repository

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/model"
)

func TestArtifactGenerationAndEditDispatchesUseSeparateOutboxes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&model.AgentRun{}, &model.GenerationDispatch{}, &model.ArtifactEditDispatch{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	runs := []model.AgentRun{
		artifactDispatchTestRun("generation-run", "generation_request", now),
		artifactDispatchTestRun("edit-run", model.AgentRunSubjectArtifactEdit, now),
	}
	if err = db.Create(&runs).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&model.GenerationDispatch{ID: "generation-dispatch", RunID: runs[0].ID, NextAttemptAt: now.Add(-time.Second), CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&model.ArtifactEditDispatch{ID: "edit-dispatch", RunID: runs[1].ID, NextAttemptAt: now.Add(-time.Second), CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}

	repo := NewArtifactRepository(db)
	generation, err := repo.Dispatches(context.Background())
	if err != nil {
		t.Fatalf("Dispatches() error = %v", err)
	}
	if len(generation) != 1 || generation[0].RunID != runs[0].ID || generation[0].SubjectKind != "generation_request" {
		t.Fatalf("Dispatches() = %+v, want only generation run", generation)
	}
	edit, err := repo.EditDispatches(context.Background())
	if err != nil {
		t.Fatalf("EditDispatches() error = %v", err)
	}
	if len(edit) != 1 || edit[0].RunID != runs[1].ID || edit[0].SubjectKind != model.AgentRunSubjectArtifactEdit {
		t.Fatalf("EditDispatches() = %+v, want only edit run", edit)
	}
	if generation[0].LeaseToken == "" || generation[0].LeaseUntil == nil || edit[0].LeaseToken == "" || edit[0].LeaseUntil == nil {
		t.Fatal("leased outbox rows must return their persisted lease")
	}
	var storedGeneration model.GenerationDispatch
	if err = db.Where("id=?", generation[0].ID).First(&storedGeneration).Error; err != nil {
		t.Fatal(err)
	}
	var storedEdit model.ArtifactEditDispatch
	if err = db.Where("id=?", edit[0].ID).First(&storedEdit).Error; err != nil {
		t.Fatal(err)
	}
	if storedGeneration.LeaseToken != generation[0].LeaseToken || storedEdit.LeaseToken != edit[0].LeaseToken {
		t.Fatal("outbox leases were not persisted to their respective tables")
	}
}

func TestArtifactEditResumeAndRecoverQueueOnlyEditOutbox(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(context.Context, *artifactEditFixture, *model.AgentRun) error
	}{
		{name: "resume", run: func(ctx context.Context, fx *artifactEditFixture, run *model.AgentRun) error {
			return fx.repo.Resume(ctx, fx.owner, run.ID, run.SubjectKind)
		}},
		{name: "recover", run: func(ctx context.Context, fx *artifactEditFixture, _ *model.AgentRun) error {
			return fx.repo.Recover(ctx)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := newArtifactEditFixture(t)
			ctx := context.Background()
			req, run := fx.requestAndRun(tc.name+"-key", tc.name+"-hash", tc.name)
			if _, err := fx.repo.SubmitEdit(ctx, req, run); err != nil {
				t.Fatal(err)
			}
			if err := fx.db.Where("run_id=?", run.ID).Delete(&model.ArtifactEditDispatch{}).Error; err != nil {
				t.Fatal(err)
			}
			if err := tc.run(ctx, fx, run); err != nil {
				t.Fatal(err)
			}
			var generationDispatches, editDispatches int64
			if err := fx.db.Model(&model.GenerationDispatch{}).Where("run_id=?", run.ID).Count(&generationDispatches).Error; err != nil {
				t.Fatal(err)
			}
			if err := fx.db.Model(&model.ArtifactEditDispatch{}).Where("run_id=?", run.ID).Count(&editDispatches).Error; err != nil {
				t.Fatal(err)
			}
			if generationDispatches != 0 || editDispatches != 1 {
				t.Fatalf("outboxes after %s: generation=%d edit=%d, want 0/1", tc.name, generationDispatches, editDispatches)
			}
		})
	}
}

func artifactDispatchTestRun(id, subject string, now time.Time) model.AgentRun {
	return model.AgentRun{
		ID: id, UserID: 7, SubjectKind: subject, SubjectID: id + "-request", ExecutionKind: "artifact", RecipeVersion: "test-v1",
		ScopeType: "video", Goal: "test", Mode: "artifact", ProfileSnapshot: `{}`, PolicySnapshot: `{}`, BudgetSnapshot: `{}`,
		Status: model.AgentRunStatusPending, CreatedAt: now, UpdatedAt: now,
	}
}
