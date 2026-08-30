package main

import (
	"testing"

	"vid-lens/internal/config"
	"vid-lens/internal/handler"
)

func TestServerAIProfileUsesGenericConfigAndLegacyFallback(t *testing.T) {
	profile := serverAIProfile(config.AIConfig{
		Provider: "openai_compatible",
		BaseURL:  "https://relay.example.com/v1",
		APIKey:   "sk-relay",
		LLMModel: "chat-model",
		ASRModel: "asr-model",
	})
	if profile.LLMProvider != "openai_compatible" || profile.ASRProvider != "openai_compatible" {
		t.Fatalf("generic provider = %q/%q", profile.LLMProvider, profile.ASRProvider)
	}
	if profile.LLMBaseURL != "https://relay.example.com/v1" || profile.ASRBaseURL != profile.LLMBaseURL || profile.LLMAPIKey != "sk-relay" || profile.ASRAPIKey != profile.LLMAPIKey {
		t.Fatalf("generic endpoint/key were not shared: %+v", profile)
	}

	legacy := serverAIProfile(config.AIConfig{
		Provider:    "mimo",
		MimoBaseURL: "https://mimo.example.com/v1",
		MimoAPIKey:  "mimo-key",
		LLMModel:    "mimo-chat",
		ASRModel:    "mimo-asr",
	})
	if legacy.LLMProvider != "mimo" || legacy.ASRProvider != "mimo" || legacy.LLMBaseURL != "https://mimo.example.com/v1" || legacy.LLMAPIKey != "mimo-key" {
		t.Fatalf("legacy MIMO fallback was not preserved: %+v", legacy)
	}

	inferred := serverAIProfile(config.AIConfig{
		Provider:    "openai_compatible",
		MimoBaseURL: "https://mimo.example.com/v1",
		MimoAPIKey:  "old-mimo-key",
	})
	if inferred.LLMProvider != "mimo" || inferred.LLMBaseURL != "https://mimo.example.com/v1" || inferred.LLMAPIKey != "old-mimo-key" {
		t.Fatalf("old key-only MIMO configuration was not inferred: %+v", inferred)
	}
}

func TestRuntimeServerHandlersIncludesKnowledgeBaseHandler(t *testing.T) {
	expected := serverHandlers{
		user:           &handler.UserHandler{},
		profiles:       &handler.AIProfileHandler{},
		rag:            &handler.RAGHandler{},
		chat:           &handler.ChatHandler{},
		media:          &handler.MediaHandler{},
		knowledgeBases: &handler.KnowledgeBaseHandler{},
		memory:         &handler.MemoryHandler{},
	}
	app := &serverApplication{handlers: expected}

	got := runtimeServerHandlers(app)
	if got.user != expected.user || got.profiles != expected.profiles || got.rag != expected.rag ||
		got.chat != expected.chat || got.media != expected.media || got.knowledgeBases != expected.knowledgeBases || got.memory != expected.memory {
		t.Fatalf("runtime handlers were not preserved: got=%+v expected=%+v", got, expected)
	}
	if got.knowledgeBases == nil {
		t.Fatal("runtime knowledge base handler is nil")
	}
	if got.memory == nil {
		t.Fatal("runtime memory handler is nil")
	}
}
