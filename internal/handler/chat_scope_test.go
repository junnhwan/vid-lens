package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"vid-lens/internal/model"
	"vid-lens/internal/service"
)

func TestChatHandlerCreatesAndListsKnowledgeBaseSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	kbSvc, repos, _ := newKnowledgeBaseHandlerServiceTestEnv(t)
	kb, err := kbSvc.Create(context.Background(), 7, service.CreateKnowledgeBaseRequest{Name: "跨视频知识库"})
	if err != nil {
		t.Fatal(err)
	}
	chatSvc := service.NewChatService(repos, nil, service.ChatConfig{})
	h := NewChatHandler(chatSvc, nil, nil)
	r := gin.New()
	r.POST("/chat/sessions", withTestUser(7), h.CreateSession)
	r.GET("/chat/sessions", withTestUser(7), h.ListSessions)

	rec := serveKnowledgeBaseRequest(r, http.MethodPost, "/chat/sessions", fmt.Sprintf(`{"scope_type":"knowledge_base","knowledge_base_id":%d}`, kb.ID))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"scope_type":"knowledge_base"`) || !strings.Contains(rec.Body.String(), `"title":"跨视频知识库"`) {
		t.Fatalf("create status/body=%d/%s", rec.Code, rec.Body.String())
	}
	list := serveKnowledgeBaseRequest(r, http.MethodGet, fmt.Sprintf("/chat/sessions?scope_type=%s&knowledge_base_id=%d", model.ChatScopeKnowledgeBase, kb.ID), "")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"knowledge_base_id":`) {
		t.Fatalf("list=%d/%s", list.Code, list.Body.String())
	}
}

func TestChatHandlerLegacyTaskIDStillCreatesVideoSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, repos, _ := newKnowledgeBaseHandlerServiceTestEnv(t)
	task := createKnowledgeBaseHandlerTask(t, repos, 7, "legacy")
	h := NewChatHandler(service.NewChatService(repos, nil, service.ChatConfig{}), nil, nil)
	r := gin.New()
	r.POST("/chat/sessions", withTestUser(7), h.CreateSession)
	rec := serveKnowledgeBaseRequest(r, http.MethodPost, "/chat/sessions", fmt.Sprintf(`{"task_id":%d}`, task.ID))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"scope_type":"video"`) {
		t.Fatalf("status/body=%d/%s", rec.Code, rec.Body.String())
	}
}
