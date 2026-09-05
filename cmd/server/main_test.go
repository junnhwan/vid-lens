package main

import (
	"net"
	"testing"

	"vid-lens/internal/config"
	"vid-lens/internal/handler"
)

func TestServerListenAddressDefaultsToLoopback(t *testing.T) {
	got := serverListenAddress(config.ServerConfig{Port: 8080})
	if got != "127.0.0.1:8080" {
		t.Fatalf("serverListenAddress() = %q, want loopback address", got)
	}
}

func TestServerListenAddressAllowsExplicitHost(t *testing.T) {
	got := serverListenAddress(config.ServerConfig{Host: "0.0.0.0", Port: 8080})
	if got != "0.0.0.0:8080" {
		t.Fatalf("serverListenAddress() = %q, want explicit host", got)
	}
	if _, _, err := net.SplitHostPort(got); err != nil {
		t.Fatalf("serverListenAddress() returned invalid address: %v", err)
	}
}

func TestServerAIProfileUsesGenericConfigFallbacks(t *testing.T) {
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

	labeled := serverAIProfile(config.AIConfig{
		Provider: "openai_compatible",
		BaseURL:  "https://relay.example.com/v1",
		APIKey:   "sk-relay",
	})
	if labeled.LLMProvider != "openai_compatible" || labeled.LLMBaseURL != "https://relay.example.com/v1" || labeled.LLMAPIKey != "sk-relay" {
		t.Fatalf("shared-only configuration was not resolved: %+v", labeled)
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
