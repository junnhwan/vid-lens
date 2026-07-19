package main

import (
	"testing"

	"vid-lens/internal/handler"
)

func TestRuntimeServerHandlersIncludesKnowledgeBaseHandler(t *testing.T) {
	expected := serverHandlers{
		user:           &handler.UserHandler{},
		profiles:       &handler.AIProfileHandler{},
		rag:            &handler.RAGHandler{},
		chat:           &handler.ChatHandler{},
		media:          &handler.MediaHandler{},
		knowledgeBases: &handler.KnowledgeBaseHandler{},
	}
	app := &serverApplication{handlers: expected}

	got := runtimeServerHandlers(app)
	if got.user != expected.user || got.profiles != expected.profiles || got.rag != expected.rag ||
		got.chat != expected.chat || got.media != expected.media || got.knowledgeBases != expected.knowledgeBases {
		t.Fatalf("runtime handlers were not preserved: got=%+v expected=%+v", got, expected)
	}
	if got.knowledgeBases == nil {
		t.Fatal("runtime knowledge base handler is nil")
	}
}
