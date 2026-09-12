package repository

import (
	"context"
	"errors"
	"gorm.io/gorm"
	"strings"
	"testing"
	"vid-lens/internal/model"
)

func feedbackTestRepo(t *testing.T) (*ChatFeedbackRepository, *model.ChatSession, *model.ChatMessage) {
	t.Helper()
	chat := newChatTestRepo(t)
	if e := chat.db.AutoMigrate(&model.ChatFeedback{}, &model.AgentRun{}, &model.ChatMessageSource{}, &model.VideoTranscription{}, &model.VideoTask{}); e != nil {
		t.Fatal(e)
	}
	s := &model.ChatSession{UserID: 7, TaskID: 2}
	if e := chat.db.Create(s).Error; e != nil {
		t.Fatal(e)
	}
	chat.db.Create(&model.ChatMessage{UserID: 7, SessionID: s.ID, Role: "user", Content: "question"})
	m := &model.ChatMessage{UserID: 7, SessionID: s.ID, Role: "assistant", Content: "observed answer"}
	if e := chat.db.Create(m).Error; e != nil {
		t.Fatal(e)
	}
	return NewChatFeedbackRepository(chat.db), s, m
}
func TestFeedbackOwnerUpdateClearAndCandidate(t *testing.T) {
	r, s, m := feedbackTestRepo(t)
	ctx := context.Background()
	first, e := r.Put(ctx, 7, s.ID, m.ID, "problem", "content", "first")
	if e != nil {
		t.Fatal(e)
	}
	updated, e := r.Put(ctx, 7, s.ID, m.ID, "problem", "incomplete", "changed")
	if e != nil || first.ID != updated.ID {
		t.Fatalf("upsert %+v %v", updated, e)
	}
	got, e := r.Get(ctx, 7, s.ID, m.ID)
	if e != nil || got.Note != "changed" {
		t.Fatalf("get %+v %v", got, e)
	}
	if _, e = r.Put(ctx, 8, s.ID, m.ID, "helpful", "", ""); !errors.Is(e, gorm.ErrRecordNotFound) {
		t.Fatalf("owner write %v", e)
	}
	if _, e = r.Get(ctx, 8, s.ID, m.ID); !errors.Is(e, gorm.ErrRecordNotFound) {
		t.Fatalf("owner read %v", e)
	}
	if e = r.Delete(ctx, 8, s.ID, m.ID); !errors.Is(e, gorm.ErrRecordNotFound) {
		t.Fatalf("owner clear %v", e)
	}
	list, total, e := r.Candidates(ctx, 7, 1, 1)
	if e != nil || total != 1 || len(list) != 1 || list[0].Question != "question" || list[0].ObservedAnswer != "observed answer" {
		t.Fatalf("candidates %+v %d %v", list, total, e)
	}
	list, total, e = r.Candidates(ctx, 8, 1, 1)
	if e != nil || total != 0 || len(list) != 0 {
		t.Fatal("candidate owner isolation")
	}
	if _, e = r.Put(ctx, 7, s.ID, m.ID, "helpful", "", ""); e != nil {
		t.Fatal(e)
	}
	_, total, e = r.Candidates(ctx, 7, 1, 1)
	if e != nil || total != 0 {
		t.Fatal("positive feedback exported")
	}
	if e = r.Delete(ctx, 7, s.ID, m.ID); e != nil {
		t.Fatal(e)
	}
	if e = r.Delete(ctx, 7, s.ID, m.ID); e != nil {
		t.Fatal(e)
	}
	if got, e = r.Get(ctx, 7, s.ID, m.ID); e != nil || got != nil {
		t.Fatalf("clear %+v %v", got, e)
	}
}
func TestFeedbackRejectsInvalidAndForeignSnapshot(t *testing.T) {
	r, s, m := feedbackTestRepo(t)
	ctx := context.Background()
	for _, v := range [][3]string{{"problem", "", ""}, {"helpful", "content", ""}, {"other", "", ""}, {"problem", "slow", strings.Repeat("字", 2001)}} {
		if _, e := r.Put(ctx, 7, s.ID, m.ID, v[0], v[1], v[2]); !errors.Is(e, ErrInvalidFeedback) {
			t.Fatalf("invalid accepted: %v", e)
		}
	}
	var user model.ChatMessage
	r.db.Where("role=?", "user").First(&user)
	if _, e := r.Put(ctx, 7, s.ID, user.ID, "helpful", "", ""); !errors.Is(e, gorm.ErrRecordNotFound) {
		t.Fatal("user message accepted")
	}
	snapshot := `{"run_id":"foreign-run"}`
	r.db.Model(m).Update("retrieval_snapshot", snapshot)
	r.db.Create(&model.AgentRun{ID: "foreign-run", UserID: 8, SessionID: s.ID, ScopeType: "video", TaskID: 2, Goal: "q", Mode: "research", ProfileSnapshot: "{}", PolicySnapshot: "{}", BudgetSnapshot: "{}", Status: "completed"})
	if _, e := r.Put(ctx, 7, s.ID, m.ID, "problem", "content", ""); !errors.Is(e, gorm.ErrRecordNotFound) {
		t.Fatalf("foreign snapshot accepted %v", e)
	}
}
