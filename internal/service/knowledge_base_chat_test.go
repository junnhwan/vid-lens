package service

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

func TestKnowledgeBaseChatRetrievesAcrossMembersWithPureVectorAndSources(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	taskA := &model.VideoTask{UserID: 7, FileMD5: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1", Filename: "a.mp4", FileURL: "videos/a.mp4", Title: "视频 A"}
	taskB := &model.VideoTask{UserID: 7, FileMD5: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb2", Filename: "b.mp4", FileURL: "videos/b.mp4"}
	if err := repos.Task.Create(taskA); err != nil {
		t.Fatal(err)
	}
	if err := repos.Task.Create(taskB); err != nil {
		t.Fatal(err)
	}
	for _, taskID := range []int64{taskA.ID, taskB.ID} {
		if err := repos.RAGIndex.Upsert(&model.VideoRAGIndex{UserID: 7, TaskID: taskID, EmbeddingModel: "embed-v1", EmbeddingDim: 3, Status: model.RAGIndexStatusIndexed}); err != nil {
			t.Fatal(err)
		}
	}
	kb := &model.KnowledgeBase{UserID: 7, Name: "KB"}
	if err := repos.KnowledgeBase.Create(kb); err != nil {
		t.Fatal(err)
	}
	if _, err := repos.KnowledgeBase.AddVideoForUser(7, kb.ID, taskB.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repos.KnowledgeBase.AddVideoForUser(7, kb.ID, taskA.ID); err != nil {
		t.Fatal(err)
	}
	session := &model.ChatSession{UserID: 7, ScopeType: model.ChatScopeKnowledgeBase, KnowledgeBaseID: kb.ID, Title: kb.Name}
	if err := repos.Chat.CreateSession(session); err != nil {
		t.Fatal(err)
	}

	retriever := &pipelineTestRetriever{results: [][]RetrievedChunk{
		{{TaskID: taskA.ID, EvidenceID: "a-1", ChunkID: 11, ChunkIndex: 1, Content: "A 介绍 owner 校验"}, {TaskID: taskB.ID, EvidenceID: "b-1", ChunkID: 21, ChunkIndex: 2, Content: "B 介绍租约恢复"}},
		{{TaskID: taskB.ID, EvidenceID: "b-1", ChunkID: 21, ChunkIndex: 2, Content: "B 介绍租约恢复"}},
		{{TaskID: taskA.ID, EvidenceID: "a-1", ChunkID: 11, ChunkIndex: 1, Content: "A 介绍 owner 校验"}},
	}}
	chat := &scriptedChatClient{responses: []string{`{"queries":["owner 校验","租约恢复"]}`, "A 需要校验 owner。[C1] B 通过租约恢复。[C2]"}}
	cfg := DefaultRAGRetrievalConfig()
	cfg.NeighborRadius = 0
	svc := NewChatService(repos, retriever, ChatConfig{TopK: 5, Retrieval: &cfg})

	result, err := svc.AskWithMode(context.Background(), ChatModeVideoAssistant, 7, session.ID, "两个视频怎么处理失败恢复？", 0, &fakeEmbeddingClient{dim: 3}, chat, ai.Profile{EmbeddingModel: "embed-v1", LLMModel: "chat"})
	if err != nil {
		t.Fatal(err)
	}
	if len(retriever.requests) != 3 {
		t.Fatalf("requests=%+v", retriever.requests)
	}
	wantIDs := []int64{taskA.ID, taskB.ID}
	for _, req := range retriever.requests {
		if !reflect.DeepEqual(req.TaskIDs, wantIDs) {
			t.Fatalf("task ids=%v want=%v", req.TaskIDs, wantIDs)
		}
	}
	if len(result.Citations) != 2 {
		t.Fatalf("citations=%+v", result.Citations)
	}
	titles := map[int64]string{}
	for _, citation := range result.Citations {
		titles[citation.TaskID] = citation.VideoTitle
	}
	if titles[taskA.ID] != "视频 A" || titles[taskB.ID] != "b.mp4" {
		t.Fatalf("titles=%v", titles)
	}
	sourceIDs, err := repos.Chat.ListSourceTaskIDsByMessageID(7, result.MessageID)
	if err != nil || !reflect.DeepEqual(sourceIDs, wantIDs) {
		t.Fatalf("sources=%v err=%v", sourceIDs, err)
	}

	_, err = svc.Ask(context.Background(), 7, session.ID, "模型切换后继续问", 0, &fakeEmbeddingClient{dim: 3}, &recordingChatClient{}, ai.Profile{EmbeddingModel: "embed-v2", LLMModel: "chat"})
	if err == nil || !strings.Contains(err.Error(), "task_ids=[") {
		t.Fatalf("model switch err=%v", err)
	}
}
