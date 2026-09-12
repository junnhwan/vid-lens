package handler

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"net/http/httptest"
	"strings"
	"testing"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

func TestFeedbackHandlerRejectsClientFactsAndValidatesOwnership(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, e := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	if e = db.AutoMigrate(&model.ChatSession{}, &model.ChatMessage{}, &model.ChatFeedback{}); e != nil {
		t.Fatal(e)
	}
	s := model.ChatSession{ID: 1, UserID: 7, TaskID: 2}
	db.Create(&s)
	db.Create(&model.ChatMessage{ID: 2, UserID: 7, SessionID: 1, Role: "assistant", Content: "answer"})
	repo := repository.NewChatFeedbackRepository(db)
	h := NewChatFeedbackHandler(repo)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("userID", int64(7)); c.Next() })
	router.PUT("/sessions/:session_id/messages/:message_id/feedback", h.Put)
	for _, body := range []string{`{"rating":"helpful","run_id":"foreign"}`, `{"rating":"problem","category":"wrong"}`, `{"rating":"helpful"} {}`} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("PUT", "/sessions/1/messages/2/feedback", strings.NewReader(body))
		router.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatalf("status %d for %s: %s", w.Code, body, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("PUT", "/sessions/1/messages/2/feedback", strings.NewReader(`{"rating":"problem","category":"slow","note":"等待过久"}`)))
	if w.Code != 200 {
		t.Fatalf("valid feedback %d %s", w.Code, w.Body.String())
	}
	f, e := repo.Get(context.Background(), 7, 1, 2)
	if e != nil || f == nil || f.Note != "等待过久" {
		t.Fatalf("persist %+v %v", f, e)
	}
}
