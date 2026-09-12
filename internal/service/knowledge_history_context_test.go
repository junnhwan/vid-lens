package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

type removeHistoryMemberChatClient struct {
	repos      *repository.Repositories
	session    *model.ChatSession
	taskID     int64
	sawHistory bool
}

func (c *removeHistoryMemberChatClient) Chat(ctx context.Context, messages []ai.ChatMessage) (string, error) {
	answerCall := false
	for _, message := range messages {
		if strings.Contains(message.Content, "HISTORY_FROM_B") {
			c.sawHistory = true
		}
		if strings.Contains(message.Content, "检索到的视频片段") {
			answerCall = true
		}
	}
	if answerCall {
		err := c.repos.TransactionContext(ctx, func(repos *repository.Repositories) error {
			if _, err := repos.KnowledgeBase.FindByIDForUserForUpdate(7, c.session.KnowledgeBaseID); err != nil {
				return err
			}
			return repos.KnowledgeBase.RemoveVideoForUser(7, c.session.KnowledgeBaseID, c.taskID)
		})
		return "本轮最终只引用仍在集合的视频 A [C1]", err
	}
	return `{"queries":["owner"]}`, nil
}

func TestKnowledgeChatRejectsChangedHistoryScopeEvenWhenFinalCitationsRemainValid(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "ask", true: "stream"}[stream], func(t *testing.T) {
			repos, session, ids := knowledgeAgentFixture(t)
			ctx := context.Background()
			cfg := DefaultRAGRetrievalConfig()
			cfg.NeighborRadius = 0
			svc := NewChatService(repos, &fakeRetriever{results: []RetrievedChunk{{TaskID: ids[0], ChunkID: 1, ChunkIndex: 0, Content: "owner 课程 1 的证据"}}}, ChatConfig{TopK: 5, Retrieval: &cfg})
			if _, err := svc.saveChatExchange(ctx, 7, session.ID, "视频B怎么说？", "HISTORY_FROM_B", []Citation{{TaskID: ids[1], Content: "owner 课程 2 的证据"}}, 0, "fixture"); err != nil {
				t.Fatal(err)
			}
			client := &removeHistoryMemberChatClient{repos: repos, session: session, taskID: ids[1]}
			var err error
			done := false
			if stream {
				_, err = svc.AskStreamWithMode(ctx, ChatModeNatural, 7, session.ID, "owner 的第二步是什么？", 5, &fakeEmbeddingClient{dim: 3}, client, ai.Profile{EmbeddingModel: "embed", LLMModel: "fixture"}, func(event ChatStreamEvent) error {
					if event.Type == "done" {
						done = true
					}
					return nil
				})
			} else {
				_, err = svc.AskWithMode(ctx, ChatModeNatural, 7, session.ID, "owner 的第二步是什么？", 5, &fakeEmbeddingClient{dim: 3}, client, ai.Profile{EmbeddingModel: "embed", LLMModel: "fixture"})
			}
			if err == nil || !strings.Contains(err.Error(), "成员已变更") {
				t.Fatalf("changed history scope accepted: %v", err)
			}
			if !client.sawHistory {
				t.Fatal("fixture did not include authorized B history before removal")
			}
			if done {
				t.Fatal("unsaved answer emitted success")
			}
			messages, err := repos.Chat.ListMessages(7, session.ID)
			if err != nil || len(messages) != 2 {
				t.Fatalf("new exchange escaped transaction: %+v %v", messages, err)
			}
		})
	}
}

func TestKnowledgeHistoryRequiresCompleteProvenanceAndKeepsWholePairs(t *testing.T) {
	snapshot := `[{"task_id":1},{"task_id":2}]`
	messages := []model.ChatMessage{{ID: 1, Role: "user", Content: "removed video discussion"}, {ID: 2, Role: "assistant", Content: "both videos", RetrievalSnapshot: &snapshot}}
	edges := []model.ChatMessageSource{{MessageID: 2, TaskID: 1}, {MessageID: 2, TaskID: 2}}
	if got := safeKnowledgeHistoryPairs(messages, edges, map[int64]bool{1: true, 2: true}, 6); len(got) != 2 {
		t.Fatalf("safe pair lost %+v", got)
	}
	for _, tc := range []struct {
		name    string
		edges   []model.ChatMessageSource
		allowed map[int64]bool
	}{
		{"removed member", edges, map[int64]bool{1: true}}, {"missing edge", edges[:1], map[int64]bool{1: true, 2: true}}, {"unknown sources", nil, map[int64]bool{1: true, 2: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := safeKnowledgeHistoryPairs(messages, tc.edges, tc.allowed, 6); len(got) != 0 {
				t.Fatalf("unsafe user/answer pair %+v", got)
			}
		})
	}
}

func TestKnowledgeChatAgentFollowupsUseOnlyCurrentSafePGPairs(t *testing.T) {
	repos, session, ids := knowledgeAgentFixture(t)
	ctx := context.Background()
	retriever := &fakeRetriever{results: []RetrievedChunk{{TaskID: ids[0], ChunkID: 1, ChunkIndex: 0, Content: "owner 课程 1 的证据"}, {TaskID: ids[1], ChunkID: 2, ChunkIndex: 0, Content: "owner 课程 2 的证据"}}}
	svc := NewChatService(repos, retriever, ChatConfig{TopK: 5, RecentTurns: 3})
	agent := NewVideoAgentService(svc)
	profile := ai.Profile{EmbeddingModel: "embed", LLMModel: "fixture"}
	embedding := &fakeEmbeddingClient{dim: 3}
	first := &scriptedChatClient{responses: []string{testSearchDecision, `{"done":true}`, "FIRST_FRAMEWORK owner 两视频说明 [C1][C2]"}}
	if _, err := agent.RunAgent(ctx, VideoAgentLoopRequest{UserID: 7, SessionID: session.ID, Goal: "比较 owner 两个视频", RunID: "kb-history-first"}, embedding, first, profile); err != nil {
		t.Fatal(err)
	}
	chat := &scriptedChatClient{responses: []string{`{"queries":["owner"]}`, "CHAT_FOLLOWUP 第二步 owner [C1][C2]"}}
	if _, err := svc.AskWithMode(ctx, ChatModeNatural, 7, session.ID, "刚才的第二步是什么？", 5, embedding, chat, profile); err != nil {
		t.Fatal(err)
	}
	assertPromptContains := func(client *scriptedChatClient, want string, present bool) {
		t.Helper()
		found := false
		for _, call := range client.messages {
			for _, message := range call {
				if strings.Contains(message.Content, want) {
					found = true
				}
			}
		}
		if found != present {
			t.Fatalf("prompt %q present=%v want=%v", want, found, present)
		}
	}
	assertPromptContains(chat, "FIRST_FRAMEWORK", true)
	follow := &scriptedChatClient{responses: []string{testSearchDecision, `{"done":true}`, "AGENT_FOLLOWUP 再次核对 owner [C1][C2]"}}
	if _, err := agent.RunAgent(ctx, VideoAgentLoopRequest{UserID: 7, SessionID: session.ID, Goal: "继续解释这一步", RunID: "kb-history-follow"}, embedding, follow, profile); err != nil {
		t.Fatal(err)
	}
	assertPromptContains(follow, "FIRST_FRAMEWORK", true)
	assertPromptContains(follow, "CHAT_FOLLOWUP", true)
	// A displayed old answer with no normalized provenance is never fed back.
	snapshot, _ := json.Marshal([]Citation{{TaskID: ids[0]}})
	text := string(snapshot)
	if err := repos.Chat.CreateMessage(&model.ChatMessage{UserID: 7, SessionID: session.ID, Role: "user", Content: "UNSAFE_USER"}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Chat.CreateMessage(&model.ChatMessage{UserID: 7, SessionID: session.ID, Role: "assistant", Content: "UNKNOWN_SOURCE", RetrievalSnapshot: &text}); err != nil {
		t.Fatal(err)
	}
	if err := repos.KnowledgeBase.RemoveVideoForUser(7, session.KnowledgeBaseID, ids[1]); err != nil {
		t.Fatal(err)
	}
	retriever.results = retriever.results[:1]
	after := &scriptedChatClient{responses: []string{testSearchDecision, `{"done":true}`, "当前来源可确认 owner [C1]"}}
	if _, err := agent.RunAgent(ctx, VideoAgentLoopRequest{UserID: 7, SessionID: session.ID, Goal: "再看刚才那个比较", RunID: "kb-history-after-removal"}, embedding, after, profile); err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"FIRST_FRAMEWORK", "CHAT_FOLLOWUP", "AGENT_FOLLOWUP", "UNSAFE_USER", "UNKNOWN_SOURCE"} {
		assertPromptContains(after, marker, false)
	}
	// Repository read is owner/session scoped even when message IDs are known.
	messages, err := repos.Chat.ListMessages(7, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	messageIDs := []int64{}
	for _, message := range messages {
		messageIDs = append(messageIDs, message.ID)
	}
	sources, err := repos.Chat.ListMessageSourcesForUser(ctx, 8, session.ID, messageIDs)
	if err != nil || len(sources) != 0 {
		t.Fatal("source owner isolation")
	}
}
