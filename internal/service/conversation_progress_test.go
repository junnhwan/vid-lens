package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vid-lens/internal/ai"
)

func TestAgentStreamsPlannerReasoningAndPersistsPublicDecisions(t *testing.T) {
	repos, task, session := newVideoAgentTestSession(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release := make(chan struct{})
	calls := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request struct {
			Stream bool `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || !request.Stream {
			t.Error("planner and answer must stream")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		send := func(delta map[string]string) {
			encoded, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": delta}}})
			fmt.Fprintf(w, "data: %s\n\n", encoded)
			w.(http.Flusher).Flush()
		}
		if calls == 1 {
			send(map[string]string{"reasoning_content": "provider-thought-not-persisted"})
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			send(map[string]string{"content": `{"tool":"search_transcript","reason":"internal reason","public_summary":"先检索原始讲解来定位证据。","arguments":{"question":"owner"}}`})
		} else if calls == 2 {
			send(map[string]string{"content": testAnswerDecision("ev-progress", task.ID, 1)})
		} else {
			send(map[string]string{"reasoning_content": "组织已有证据"})
			send(map[string]string{"content": "最终"})
			send(map[string]string{"content": "回答 [C1]"})
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer provider.Close()
	svc := NewVideoAgentService(NewChatService(repos, &fakeRetriever{results: []RetrievedChunk{{TaskID: task.ID, ChunkID: 1, EvidenceID: "ev-progress", Content: "owner"}}}, ChatConfig{TopK: 1}))
	planned, reasoning, answers := false, 0, 0
	result, err := svc.Stream(ctx, VideoAgentStreamRequest{UserID: 7, SessionID: session.ID, Question: "owner"}, &fakeEmbeddingClient{dim: 3}, ai.NewOpenAIChatClient(provider.URL, "", "test"), ai.Profile{EmbeddingModel: "embed"}, func(event AgentStreamEvent) error {
		if event.Type == "progress" {
			p := event.Data.(ConversationProgress)
			if p.ID == "plan-1" && p.Status == "running" {
				planned = true
			}
		}
		if event.Type == "reasoning" {
			reasoning++
			if reasoning == 1 {
				if !planned {
					t.Error("reasoning before planning_start")
				}
				close(release)
			}
		}
		if event.Type == "answer" {
			answers++
		}
		return nil
	})
	if err != nil || reasoning != 2 || answers != 2 || result.Answer != "最终回答" {
		t.Fatalf("result=%+v reasoning=%d answers=%d err=%v", result, reasoning, answers, err)
	}
	messages, err := repos.Chat.ListMessages(7, session.ID)
	if err != nil || len(messages) != 2 {
		t.Fatalf("messages=%d err=%v", len(messages), err)
	}
	if messages[1].RetrievalSnapshot == nil {
		t.Fatal("missing snapshot")
	}
	snapshot := *messages[1].RetrievalSnapshot
	if !strings.Contains(snapshot, "先检索原始讲解") || strings.Contains(snapshot, "provider-thought-not-persisted") || strings.Contains(snapshot, "internal reason") {
		t.Fatalf("public snapshot has wrong content: %s", snapshot)
	}
}

func TestFailedRunHistoryIsSessionAndOwnerScoped(t *testing.T) {
	repos, task, session := newVideoAgentTestSession(t)
	chat := NewChatService(repos, &fakeRetriever{}, ChatConfig{TopK: 1})
	_, err := NewVideoAgentService(chat).Stream(context.Background(), VideoAgentStreamRequest{UserID: 7, SessionID: session.ID, Question: "test failure"}, &fakeEmbeddingClient{dim: 3}, &scriptedChatClient{responses: []string{"invalid planner JSON"}}, ai.Profile{EmbeddingModel: "embed"}, func(AgentStreamEvent) error { return nil })
	if err == nil {
		t.Fatal("expected failed plan")
	}
	history, err := chat.ListUnfinishedRunHistory(context.Background(), 7, session.ID)
	if err != nil || len(history) != 1 || len(history[0].Steps) != 1 || history[0].Steps[0].Status != "error" {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	encoded, _ := json.Marshal(history)
	if strings.Contains(string(encoded), "result_checkpoint") || strings.Contains(string(encoded), "profile_snapshot") {
		t.Fatal("internal record exposed")
	}
	if _, err := chat.ListUnfinishedRunHistory(context.Background(), 8, session.ID); err == nil {
		t.Fatal("cross-owner access allowed")
	}
	other, err := chat.CreateSession(7, task.ID, "other")
	if err != nil {
		t.Fatal(err)
	}
	if history, err := chat.ListUnfinishedRunHistory(context.Background(), 7, other.ID); err != nil || len(history) != 0 {
		t.Fatalf("cross-session history=%+v err=%v", history, err)
	}
}
