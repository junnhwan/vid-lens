package repository

import (
	"context"
	"testing"
	"time"
	"vid-lens/internal/model"
)

func TestLegacyPreferenceMigrationPreservesUnknownAndConflictingDimensions(t *testing.T) {
	db := newMemoryRepositoryTestDB(t)
	repo := NewMemoryRepository(db)
	ctx := context.Background()
	for i, content := range []string{"回答语言：中文", "回答语言：英文", "回答风格：简洁", "unknown legacy"} {
		item := model.AgentMemoryItem{ID: []string{"zh", "en", "brief", "unknown"}[i], UserID: 7, ScopeType: model.MemoryScopeUser, ScopeID: "7", Kind: "response_preference", Content: content, SourceType: "manual", SourceRef: "manual:fixture", Importance: .7, Status: model.MemoryStatusConflicted, Version: 1}
		if err := db.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
	}
	items, err := repo.ListStructuredPreferences(ctx, 7, time.Now())
	if err != nil || len(items) != 1 || items[0].Kind != "response.verbosity" {
		t.Fatalf("migration guessed conflict %+v %v", items, err)
	}
	var unknown model.AgentMemoryItem
	if err := db.First(&unknown, "id = ?", "unknown").Error; err != nil {
		t.Fatal(err)
	}
	if unknown.Kind != "response_preference" || unknown.Version != 1 {
		t.Fatal("unknown legacy mutated")
	}
	var eventCount int64
	db.Model(&model.AgentMemoryEvent{}).Where("event_type = ?", "preference_migrated").Count(&eventCount)
	if eventCount != 3 {
		t.Fatalf("missing migration audit %d", eventCount)
	}
	if _, err := repo.ListStructuredPreferences(ctx, 7, time.Now()); err != nil {
		t.Fatal(err)
	}
	db.Model(&model.AgentMemoryEvent{}).Where("event_type = ?", "preference_migrated").Count(&eventCount)
	if eventCount != 3 {
		t.Fatal("migration replay changed versions")
	}
}

func TestMemoryCaptureVersionCannotCompleteNewerIdentity(t *testing.T) {
	db := newMemoryRepositoryTestDB(t)
	if err := db.AutoMigrate(&model.MemoryCaptureJob{}); err != nil {
		t.Fatal(err)
	}
	repo := NewMemoryRepository(db)
	now := time.Now()
	ctx := context.Background()
	if err := db.Create(&model.MemoryCaptureJob{MessageID: 1, UserID: 7, SessionID: 9, Status: "pending", AvailableAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	job, err := repo.ClaimMemoryCapture(ctx, now)
	if err != nil || job == nil {
		t.Fatal(err)
	}
	wrong := *job
	wrong.ExtractorVersion = "old-version"
	if err := repo.FinishMemoryCapture(ctx, wrong, true); err != nil {
		t.Fatal(err)
	}
	var current model.MemoryCaptureJob
	db.First(&current, 1)
	if current.Status != "processing" {
		t.Fatal("old extraction version completed another identity")
	}
	job.ProjectionPending = true
	if err := repo.FinishMemoryCapture(ctx, *job, false); err != nil {
		t.Fatal(err)
	}
	status, err := repo.CaptureStatus(ctx, 7)
	if err != nil || status.ProjectionPending != 1 {
		t.Fatalf("projection status %+v %v", status, err)
	}
}
