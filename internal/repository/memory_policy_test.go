package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/model"
)

func newMemoryPolicyRepositoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.ChatSession{}, &model.AgentMemoryPreference{}, &model.AgentMemoryPolicyEvent{},
		&model.AgentMemoryItem{}, &model.AgentMemoryEvent{}, &model.AgentRun{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestMemoryPolicyRepositoryDefaultsVersionsAndAuditsChanges(t *testing.T) {
	db := newMemoryPolicyRepositoryTestDB(t)
	repo := NewMemoryRepository(db)
	ctx := context.Background()
	session := model.ChatSession{UserID: 7, TaskID: 42, ScopeType: model.ChatScopeVideo, MemoryPolicy: model.MemorySessionPolicyInherit}
	if err := db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}

	preference, err := repo.GetMemoryPreference(ctx, 7)
	if err != nil || preference.Enabled || preference.Version != 0 {
		t.Fatalf("default preference = %+v err=%v", preference, err)
	}
	inputs, err := repo.ResolveMemoryPolicyInputs(ctx, 7, session.ID)
	if err != nil || inputs.UserEnabled || inputs.UserPreferenceVersion != 0 || inputs.SessionPolicy != model.MemorySessionPolicyInherit || inputs.SessionPolicyVersion != 0 {
		t.Fatalf("default policy inputs = %+v err=%v", inputs, err)
	}

	preference, err = repo.UpdateMemoryPreference(ctx, 7, true, 0, true)
	if err != nil || !preference.Enabled || preference.Version != 1 {
		t.Fatalf("enabled preference = %+v err=%v", preference, err)
	}
	if _, err := repo.UpdateMemoryPreference(ctx, 7, false, 0, true); !errors.Is(err, ErrMemoryPolicyVersionConflict) {
		t.Fatalf("stale preference update error = %v", err)
	}
	session, _, err = repo.UpdateSessionMemoryPolicy(ctx, 7, session.ID, model.MemorySessionPolicyDisabled, 0, true)
	if err != nil || session.MemoryPolicy != model.MemorySessionPolicyDisabled || session.MemoryPolicyVersion != 1 {
		t.Fatalf("disabled session policy = %+v err=%v", session, err)
	}
	if _, _, err := repo.UpdateSessionMemoryPolicy(ctx, 8, session.ID, model.MemorySessionPolicyEnabled, 1, true); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-owner session update error = %v", err)
	}

	events, err := repo.ListMemoryPolicyEvents(ctx, 7)
	if err != nil || len(events) != 2 {
		t.Fatalf("policy events = %+v err=%v", events, err)
	}
	byTarget := make(map[string]model.AgentMemoryPolicyEvent, len(events))
	for _, event := range events {
		byTarget[event.TargetType] = event
	}
	userEvent := byTarget[model.MemoryPolicyTargetUser]
	if userEvent.PreviousValue != model.MemorySessionPolicyDisabled || userEvent.NewValue != model.MemorySessionPolicyEnabled || !userEvent.EffectiveAfter {
		t.Fatalf("user policy event = %+v", userEvent)
	}
	sessionEvent := byTarget[model.MemoryPolicyTargetSession]
	if sessionEvent.PreviousValue != model.MemorySessionPolicyInherit || sessionEvent.NewValue != model.MemorySessionPolicyDisabled || !sessionEvent.EffectiveBefore || sessionEvent.EffectiveAfter {
		t.Fatalf("session policy event = %+v", sessionEvent)
	}
}

func TestMemoryPolicyRepositoryConcurrentExpectedVersionHasSingleWinner(t *testing.T) {
	db := newMemoryPolicyRepositoryTestDB(t)
	repo := NewMemoryRepository(db)
	var succeeded atomic.Int64
	var conflicted atomic.Int64
	var unexpected atomic.Value
	var wait sync.WaitGroup
	for index := 0; index < 16; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := repo.UpdateMemoryPreference(context.Background(), 11, true, 0, true)
			switch {
			case err == nil:
				succeeded.Add(1)
			case errors.Is(err, ErrMemoryPolicyVersionConflict):
				conflicted.Add(1)
			default:
				unexpected.Store(err)
			}
		}()
	}
	wait.Wait()
	if value := unexpected.Load(); value != nil {
		t.Fatalf("unexpected concurrent error = %v", value)
	}
	if succeeded.Load() != 1 || conflicted.Load() != 15 {
		t.Fatalf("concurrent results succeeded=%d conflicted=%d", succeeded.Load(), conflicted.Load())
	}
	events, err := repo.ListMemoryPolicyEvents(context.Background(), 11)
	if err != nil || len(events) != 1 {
		t.Fatalf("concurrent audit events = %+v err=%v", events, err)
	}
}

func TestAppendCapturedRechecksPolicyScopeAndKeepsExistingMemoryOnOptOut(t *testing.T) {
	db := newMemoryPolicyRepositoryTestDB(t)
	repo := NewMemoryRepository(db)
	ctx := context.Background()
	session := model.ChatSession{UserID: 5, TaskID: 9, ScopeType: model.ChatScopeVideo, MemoryPolicy: model.MemorySessionPolicyInherit}
	if err := db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	newItem := func(id, content string) *model.AgentMemoryItem {
		return &model.AgentMemoryItem{
			ID: id, UserID: 5, ScopeType: model.MemoryScopeUser, ScopeID: "5", Kind: "preference",
			Content: content, SourceType: "user_message", SourceRef: fmt.Sprintf("message:%s", id), Importance: .7,
		}
	}
	if _, allowed, err := repo.AppendCaptured(ctx, session.ID, newItem("disabled", "默认关闭")); err != nil || allowed {
		t.Fatalf("default-disabled capture allowed=%v err=%v", allowed, err)
	}
	if _, _, err := repo.UpdateSessionMemoryPolicy(ctx, 5, session.ID, model.MemorySessionPolicyEnabled, 0, true); err != nil {
		t.Fatal(err)
	}
	if _, allowed, err := repo.AppendCaptured(ctx, session.ID, newItem("kept", "显式开启")); err != nil || !allowed {
		t.Fatalf("enabled capture allowed=%v err=%v", allowed, err)
	}
	badScope := newItem("bad-scope", "错误范围")
	badScope.ScopeType, badScope.ScopeID = model.MemoryScopeVideo, "999"
	if _, _, err := repo.AppendCaptured(ctx, session.ID, badScope); !errors.Is(err, gorm.ErrInvalidData) {
		t.Fatalf("scope mismatch error = %v", err)
	}
	if _, _, err := repo.UpdateSessionMemoryPolicy(ctx, 5, session.ID, model.MemorySessionPolicyDisabled, 1, true); err != nil {
		t.Fatal(err)
	}
	if _, allowed, err := repo.AppendCaptured(ctx, session.ID, newItem("after-opt-out", "关闭后")); err != nil || allowed {
		t.Fatalf("post-opt-out capture allowed=%v err=%v", allowed, err)
	}
	items, err := repo.ListForUser(ctx, 5, model.MemoryScopeUser, "5")
	if err != nil || len(items) != 1 || items[0].ID != "kept" {
		t.Fatalf("memories after opt-out = %+v err=%v", items, err)
	}
}
