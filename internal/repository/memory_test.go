package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/model"
)

func newMemoryRepositoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AgentMemoryItem{}, &model.AgentMemoryEvent{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestMemoryRepositoryPersistsAndIsolatesAllScopes(t *testing.T) {
	db := newMemoryRepositoryTestDB(t)
	repo := NewMemoryRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()
	items := []model.AgentMemoryItem{
		{ID: "u1", UserID: 1, ScopeType: model.MemoryScopeUser, ScopeID: "1", Kind: "preference", Content: "中文", SourceType: "user_message", SourceRef: "message:1", Importance: .9, Status: model.MemoryStatusActive, Version: 1},
		{ID: "v1", UserID: 1, ScopeType: model.MemoryScopeVideo, ScopeID: "10", Kind: "alias", Content: "视频 A", SourceType: "user_confirmation", SourceRef: "message:2", Importance: .8, Status: model.MemoryStatusActive, Version: 1},
		{ID: "kb1", UserID: 1, ScopeType: model.MemoryScopeKnowledgeBase, ScopeID: "20", Kind: "term", Content: "术语 A", SourceType: "manual", SourceRef: "manual:3", Importance: .7, Status: model.MemoryStatusActive, Version: 1},
		{ID: "r1", UserID: 1, ScopeType: model.MemoryScopeRun, ScopeID: "run-a", Kind: "open", Content: "待确认", SourceType: "run_observation", SourceRef: "run:4", Importance: .6, Status: model.MemoryStatusActive, Version: 1},
		{ID: "u2", UserID: 2, ScopeType: model.MemoryScopeUser, ScopeID: "2", Kind: "preference", Content: "English", SourceType: "user_message", SourceRef: "message:5", Importance: 1, Status: model.MemoryStatusActive, Version: 1},
	}
	for i := range items {
		if _, err := repo.Append(ctx, &items[i]); err != nil {
			t.Fatalf("Append(%s): %v", items[i].ID, err)
		}
	}
	got, err := repo.ListRecallable(ctx, 1, map[string][]string{
		model.MemoryScopeUser: {"1"}, model.MemoryScopeVideo: {"10"}, model.MemoryScopeKnowledgeBase: {"20"}, model.MemoryScopeRun: {"run-a"},
	}, 10, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("recallable items = %+v", got)
	}
	for _, item := range got {
		if item.UserID != 1 || item.SourceRef == "" {
			t.Fatalf("isolated/source invariant failed: %+v", item)
		}
	}
}

func TestMemoryRepositoryPreservesConflictsAndLifecycleEvents(t *testing.T) {
	db := newMemoryRepositoryTestDB(t)
	repo := NewMemoryRepository(db)
	ctx := context.Background()
	first := &model.AgentMemoryItem{ID: "c1", UserID: 1, ScopeType: model.MemoryScopeVideo, ScopeID: "10", Kind: "speaker", Content: "Alice", SourceType: "user_confirmation", SourceRef: "message:1", Importance: .8, Status: model.MemoryStatusActive, Version: 1}
	second := &model.AgentMemoryItem{ID: "c2", UserID: 1, ScopeType: model.MemoryScopeVideo, ScopeID: "10", Kind: "speaker", Content: "Bob", SourceType: "user_confirmation", SourceRef: "message:2", Importance: .8, Status: model.MemoryStatusActive, Version: 1}
	if _, err := repo.Append(ctx, first); err != nil {
		t.Fatal(err)
	}
	result, err := repo.Append(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ConflictIDs) != 2 || result.Item.Status != model.MemoryStatusConflicted {
		t.Fatalf("append result = %+v", result)
	}
	items, err := repo.ListRecallable(ctx, 1, map[string][]string{model.MemoryScopeVideo: {"10"}}, 10, time.Now().UTC())
	if err != nil || len(items) != 2 || items[0].Status != model.MemoryStatusConflicted || items[1].Status != model.MemoryStatusConflicted {
		t.Fatalf("conflicted items = %+v err=%v", items, err)
	}
	if err := repo.WithdrawForUser(ctx, 1, "c1", "user_request:1"); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteForUser(ctx, 1, "c2", "user_request:2"); err != nil {
		t.Fatal(err)
	}
	items, err = repo.ListRecallable(ctx, 1, map[string][]string{model.MemoryScopeVideo: {"10"}}, 10, time.Now().UTC())
	if err != nil || len(items) != 0 {
		t.Fatalf("lifecycle recall = %+v err=%v", items, err)
	}
	var events []model.AgentMemoryEvent
	if err := db.Order("occurred_at ASC").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) < 5 {
		t.Fatalf("events = %+v", events)
	}
	for _, event := range events {
		if event.SourceRef == "" {
			t.Fatalf("event missing source_ref: %+v", event)
		}
	}
}

func TestMemoryRepositoryRejectsMissingSourceRef(t *testing.T) {
	repo := NewMemoryRepository(newMemoryRepositoryTestDB(t))
	_, err := repo.Append(context.Background(), &model.AgentMemoryItem{
		ID: "bad", UserID: 1, ScopeType: model.MemoryScopeUser, ScopeID: "1", Kind: "preference", Content: "中文", SourceType: "user_message", Importance: .5,
	})
	if err == nil {
		t.Fatal("Append() accepted item without source_ref")
	}
	_ = fmt.Sprint(err)
}
