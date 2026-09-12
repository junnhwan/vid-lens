package repository

import (
	"context"
	"testing"
	"time"
	"vid-lens/internal/model"
)

func TestMemoryCaptureLeaseRecoveryAndStaleCompletion(t *testing.T) {
	db := newMemoryRepositoryTestDB(t)
	if err := db.AutoMigrate(&model.MemoryCaptureJob{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := db.Create(&model.MemoryCaptureJob{MessageID: 1, UserID: 7, SessionID: 9, Status: "pending", AvailableAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	r := NewMemoryRepository(db)
	ctx := context.Background()
	first, err := r.ClaimMemoryCapture(ctx, now)
	if err != nil || first == nil {
		t.Fatalf("claim=%+v %v", first, err)
	}
	if job, err := r.ClaimMemoryCapture(ctx, now.Add(time.Second)); err != nil || job != nil {
		t.Fatalf("claimed leased task: %+v %v", job, err)
	}
	second, err := r.ClaimMemoryCapture(ctx, now.Add(2*time.Minute))
	if err != nil || second == nil || second.LeaseToken == first.LeaseToken {
		t.Fatalf("recovery=%+v %v", second, err)
	}
	if err := r.FinishMemoryCapture(ctx, *first, true); err != nil {
		t.Fatal(err)
	}
	var persisted model.MemoryCaptureJob
	db.First(&persisted, 1)
	if persisted.Status != "processing" || persisted.LeaseToken != second.LeaseToken {
		t.Fatal("stale worker completed a new lease")
	}
	if err := r.FinishMemoryCapture(ctx, *second, true); err != nil {
		t.Fatal(err)
	}
	if job, err := r.ClaimMemoryCapture(ctx, now.Add(time.Hour)); err != nil || job != nil {
		t.Fatalf("completed job replayed: %+v %v", job, err)
	}
}

func TestMemoryCaptureReplayCannotResurrectDeletedSource(t *testing.T) {
	db := newMemoryRepositoryTestDB(t)
	repo := NewMemoryRepository(db)
	ctx := context.Background()
	item := model.AgentMemoryItem{UserID: 7, ScopeType: model.MemoryScopeUser, ScopeID: "7", Kind: "response_preference", Content: "中文", SourceType: "user_message", SourceRef: "chat_message:9", Importance: .7}
	first, err := repo.Append(ctx, &item)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteForUser(ctx, 7, first.Item.ID, "user-delete"); err != nil {
		t.Fatal(err)
	}
	item.ID = ""
	item.Status = model.MemoryStatusActive
	replay, err := repo.Append(ctx, &item)
	if err != nil || replay.Created || replay.Item.Status != model.MemoryStatusDeleted {
		t.Fatalf("resurrection: %+v %v", replay, err)
	}
}

func TestMemoryCaptureDeletedPreferenceCannotReturnViaAnotherSource(t *testing.T) {
	db := newMemoryRepositoryTestDB(t)
	repo := NewMemoryRepository(db)
	ctx := context.Background()
	item := model.AgentMemoryItem{UserID: 7, ScopeType: model.MemoryScopeUser, ScopeID: "7", Kind: "response_preference", Content: "中文", SourceType: "user_message", SourceRef: "chat_message:1", Importance: .7}
	first, err := repo.Append(ctx, &item)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WithdrawForUser(ctx, 7, first.Item.ID, "user"); err != nil {
		t.Fatal(err)
	}
	item.ID = ""
	item.SourceRef = "chat_message:2"
	item.Status = model.MemoryStatusActive
	replay, err := repo.Append(ctx, &item)
	if err != nil || replay.Created || replay.Item.Status != model.MemoryStatusWithdrawn {
		t.Fatalf("recreated withdrawn preference: %+v %v", replay, err)
	}
}
