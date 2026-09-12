package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

func TestKnowledgeToolHonorsResultLimitWithoutChangingGlobalConfig(t *testing.T) {
	repos, _, ids := knowledgeAgentFixture(t)
	cfg := DefaultRAGRetrievalConfig()
	cfg.TopK, cfg.EnableVector, cfg.EnableBM25 = 5, false, true
	pipeline := &RetrievalPipeline{repos: repos, Config: &cfg, rewriter: NoopQueryRewriter{}}
	tools := NewVideoAgentTools(repos, pipeline, nil)
	runtime := VideoAgentToolRuntime{UserID: 7, TaskIDs: ids, EmbeddingModel: "embed", TopK: 2}
	for _, count := range []int{1, 2} {
		result, err := tools.Registry().Execute(context.Background(), VideoAgentToolSearchTranscript, VideoAgentToolRequest{
			Runtime: runtime, Arguments: []byte(fmt.Sprintf(`{"question":"owner","top_k":%d}`, count)),
		})
		if err != nil {
			t.Fatal(err)
		}
		var output SearchTranscriptResult
		if err := json.Unmarshal(result.Output, &output); err != nil {
			t.Fatal(err)
		}
		if len(output.Citations) != count {
			t.Fatalf("requested %d citations, got %d", count, len(output.Citations))
		}
	}
	if pipeline.Config.TopK != 5 {
		t.Fatal("tool mutated shared retrieval configuration")
	}
}

func knowledgeAgentFixture(t *testing.T) (*repository.Repositories, *model.ChatSession, []int64) {
	t.Helper()
	repos := newChatServiceTestRepositories(t)
	if err := repos.User.Create(&model.User{ID: 7, Username: "knowledge-test", PasswordHash: "fixture"}); err != nil {
		t.Fatal(err)
	}
	kb := &model.KnowledgeBase{UserID: 7, Name: "事务课程"}
	if err := repos.KnowledgeBase.Create(kb); err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for i := 0; i < 2; i++ {
		task := &model.VideoTask{UserID: 7, FileMD5: fmt.Sprintf("%032d", i+1), Filename: fmt.Sprintf("lesson-%d.mp4", i), FileURL: "video.mp4", Title: fmt.Sprintf("课程 %d", i+1)}
		if err := repos.Task.Create(task); err != nil {
			t.Fatal(err)
		}
		if _, err := repos.KnowledgeBase.AddVideoForUser(7, kb.ID, task.ID); err != nil {
			t.Fatal(err)
		}
		if err := repos.RAGIndex.Upsert(&model.VideoRAGIndex{UserID: 7, TaskID: task.ID, FileMD5: task.FileMD5, EmbeddingModel: "embed", Status: model.RAGIndexStatusIndexed}); err != nil {
			t.Fatal(err)
		}
		seedVideoChunks(t, repos, 7, task.ID, "embed", []string{fmt.Sprintf("owner 课程 %d 的证据", i+1)})
		ids = append(ids, task.ID)
	}
	session := &model.ChatSession{UserID: 7, ScopeType: model.ChatScopeKnowledgeBase, KnowledgeBaseID: kb.ID, Title: "research"}
	if err := repos.Chat.CreateSession(session); err != nil {
		t.Fatal(err)
	}
	return repos, session, ids
}

func TestKnowledgeAgentStreamsCitationsFromTwoVideosAndPersistsSources(t *testing.T) {
	repos, session, ids := knowledgeAgentFixture(t)
	chatSvc := NewChatService(repos, &fakeRetriever{}, ChatConfig{TopK: 5})
	agent := NewVideoAgentService(chatSvc)
	client := &scriptedChatClient{responses: []string{testSearchDecision, `{"done":true,"stop_reason":"ready"}`, "两个视频都要求 owner 校验 [C1][C2]"}}
	var events []AgentStreamEvent
	result, err := agent.Stream(context.Background(), VideoAgentStreamRequest{UserID: 7, SessionID: session.ID, Question: "比较 owner 校验"}, &fakeEmbeddingClient{dim: 3}, client, ai.Profile{EmbeddingModel: "embed", LLMModel: "chat"}, func(e AgentStreamEvent) error { events = append(events, e); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Citations) != 2 || result.Citations[0].TaskID == result.Citations[1].TaskID {
		t.Fatalf("citations: %+v", result.Citations)
	}
	sources, err := repos.Chat.ListSourceTaskIDsByMessageID(7, result.MessageID)
	if err != nil || !sameTaskIDs(sources, ids) {
		t.Fatalf("sources=%v err=%v", sources, err)
	}
	if len(events) == 0 || events[len(events)-1].Type != AgentEventDone {
		t.Fatalf("missing done: %+v", events)
	}
	detail, err := chatSvc.GetAgentRunDetail(context.Background(), 7, session.ID, result.RunID)
	if err != nil || detail.Status != model.AgentRunStatusCompleted || detail.ToolsUsed != 2 {
		t.Fatalf("detail=%+v err=%v", detail, err)
	}
	if _, err := chatSvc.GetAgentRunDetail(context.Background(), 8, session.ID, result.RunID); err == nil {
		t.Fatal("cross-owner run exposed")
	}
	if err := repos.KnowledgeBase.RemoveVideoForUser(7, session.KnowledgeBaseID, ids[1]); err != nil {
		t.Fatal(err)
	}
	_, err = agent.RunAgent(context.Background(), VideoAgentLoopRequest{UserID: 7, SessionID: session.ID, Goal: "比较 owner 校验", RunID: result.RunID}, &fakeEmbeddingClient{dim: 3}, client, ai.Profile{EmbeddingModel: "embed", LLMModel: "chat"})
	if !errors.Is(err, errKnowledgeMembershipChanged) {
		t.Fatalf("replayed removed evidence: %v", err)
	}
}

func TestKnowledgeAgentStopsWhenMembershipChangesDuringRetrieval(t *testing.T) {
	repos, session, ids := knowledgeAgentFixture(t)
	agent := NewVideoAgentService(NewChatService(repos, &fakeRetriever{}, ChatConfig{TopK: 5}))
	client := &scriptedChatClient{responses: []string{testSearchDecision, `{"done":true}`, "should not answer"}}
	_, err := agent.Stream(context.Background(), VideoAgentStreamRequest{UserID: 7, SessionID: session.ID, Question: "owner"}, &fakeEmbeddingClient{dim: 3}, client, ai.Profile{EmbeddingModel: "embed", LLMModel: "chat"}, func(e AgentStreamEvent) error {
		if e.Type == AgentEventRetrieveHits {
			return repos.KnowledgeBase.RemoveVideoForUser(7, session.KnowledgeBaseID, ids[1])
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "变化") {
		t.Fatalf("expected membership failure: %v", err)
	}
	msgs, _ := repos.Chat.ListMessages(7, session.ID)
	if len(msgs) != 0 {
		t.Fatalf("saved a stale answer: %+v", msgs)
	}
}

func TestKnowledgeWindowToolsRequireAuthorizedVideo(t *testing.T) {
	repos, _, ids := knowledgeAgentFixture(t)
	tools := NewVideoAgentTools(repos, nil, nil)
	tools.Registry().useCollectionSchemas()
	runtime := VideoAgentToolRuntime{UserID: 7, TaskIDs: ids, EmbeddingModel: "embed"}
	for _, args := range []string{`{"chunk_index":0}`, `{"chunk_index":0,"task_id":999}`} {
		if _, err := tools.Registry().Execute(context.Background(), VideoAgentToolGetTranscriptWindow, VideoAgentToolRequest{Runtime: runtime, Arguments: []byte(args)}); err == nil {
			t.Fatalf("accepted %s", args)
		}
	}
	result, err := tools.Registry().Execute(context.Background(), VideoAgentToolGetTranscriptWindow, VideoAgentToolRequest{Runtime: runtime, Arguments: []byte(fmt.Sprintf(`{"chunk_index":0,"task_id":%d}`, ids[1]))})
	if err != nil || !strings.Contains(string(result.Output), "课程 2") {
		t.Fatalf("wrong video window: %s %v", result.Output, err)
	}
	if err := runtime.checkScope(context.Background(), []RetrievedChunk{{TaskID: 999}}); err == nil {
		t.Fatal("accepted foreign evidence")
	}
}
