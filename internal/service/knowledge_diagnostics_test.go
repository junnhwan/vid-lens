package service

import (
	"context"
	"testing"
	"vid-lens/internal/ai"
)

type knowledgeTestClients struct{}

func (knowledgeTestClients) NewEmbeddingClient(ai.Profile) (ai.EmbeddingClient, error) {
	return &fakeEmbeddingClient{dim: 3}, nil
}
func (knowledgeTestClients) NewChatClient(ai.Profile) (ai.ChatClient, error) {
	return &scriptedChatClient{}, nil
}

func TestKnowledgeRetrievalDiagnosticsUsesRealKeywordPipelineWithoutAnswer(t *testing.T) {
	repos, session, _ := knowledgeAgentFixture(t)
	chat := NewChatService(repos, &fakeRetriever{}, ChatConfig{TopK: 5})
	executor := NewConversationExecution(chat, nil, stubConversationProfileProvider{profile: ai.Profile{EmbeddingModel: "embed"}}, knowledgeTestClients{})
	result, err := executor.TestKnowledgeRetrieval(context.Background(), 7, session.KnowledgeBaseID, KnowledgeRetrievalRequest{Question: "owner", Mode: "keyword", TopK: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Citations) != 2 || len(result.Trace.Stages) != 4 {
		t.Fatalf("result=%+v", result)
	}
	for _, stage := range result.Trace.Stages {
		if stage.Name == "vector" && len(stage.Citations) != 0 {
			t.Fatal("keyword-only called vector")
		}
		if stage.Name == "keyword" && len(stage.Citations) != 2 {
			t.Fatal("lost keyword candidates")
		}
	}
	msgs, _ := repos.Chat.ListMessages(7, session.ID)
	if len(msgs) != 0 {
		t.Fatal("diagnostics saved chat messages")
	}
	if _, err := executor.TestKnowledgeRetrieval(context.Background(), 8, session.KnowledgeBaseID, KnowledgeRetrievalRequest{Question: "owner", Mode: "keyword"}); err == nil {
		t.Fatal("cross-owner diagnostics allowed")
	}
}
