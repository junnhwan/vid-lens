package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

func TestChatSeparatesLLMContextFromPublicCitation(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "abababababababababababababababab", Filename: "evidence.mp4", FileURL: "videos/evidence.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	session := &model.ChatSession{UserID: 7, TaskID: task.ID, Title: "session"}
	if err := repos.Chat.CreateSession(session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	anchor := strings.Repeat("背景介绍。", 40) + "工具调用结果会作为新消息反馈给模型。" + strings.Repeat("其他介绍。", 40)
	expanded := "前邻居上下文只给模型。\n" + anchor + "\n后邻居上下文也只给模型。"
	chatClient := &recordingChatClient{}
	svc := NewChatService(repos, &fakeRetriever{results: []RetrievedChunk{{
		EvidenceID: "ev-1", ChunkID: 12, ChunkIndex: 4,
		Content: expanded, AnchorContent: anchor, MatchedQuery: "工具调用结果反馈模型",
	}}}, ChatConfig{TopK: 5, MinScore: 0.3, RecentTurns: 8})

	result, err := svc.Ask(context.Background(), 7, session.ID, "工具调用结果如何反馈给模型？", 0, &fakeEmbeddingClient{dim: 3}, chatClient, ai.Profile{
		EmbeddingModel: "text-embedding-3-small",
		LLMModel:       "chat-model",
	})
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}

	var prompt strings.Builder
	for _, message := range chatClient.messages {
		prompt.WriteString(message.Content)
		prompt.WriteByte('\n')
	}
	if !strings.Contains(prompt.String(), "前邻居上下文只给模型") || !strings.Contains(prompt.String(), "后邻居上下文也只给模型") {
		t.Fatalf("LLM prompt lost expanded context: %s", prompt.String())
	}
	if !strings.Contains(prompt.String(), "[C1]") {
		t.Fatalf("LLM prompt does not label citation context: %s", prompt.String())
	}

	if len(result.Citations) != 1 {
		t.Fatalf("citations = %#v, want one", result.Citations)
	}
	citation := result.Citations[0]
	if strings.Contains(citation.Content, "邻居上下文") {
		t.Fatalf("public citation leaked expanded context: %q", citation.Content)
	}
	if !strings.Contains(citation.Content, "工具调用结果会作为新消息反馈给模型") {
		t.Fatalf("public citation = %q, want relevant evidence", citation.Content)
	}
	if !strings.Contains(anchor, citation.Content) {
		t.Fatalf("public citation must be verbatim anchor evidence: %q", citation.Content)
	}

	messages, err := repos.Chat.ListMessages(7, session.ID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(messages) != 2 || messages[1].RetrievalSnapshot == nil {
		t.Fatalf("stored messages = %#v", messages)
	}
	var snapshot []Citation
	if err := json.Unmarshal([]byte(*messages[1].RetrievalSnapshot), &snapshot); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	if len(snapshot) != 1 || snapshot[0].Content != citation.Content {
		t.Fatalf("snapshot = %#v, citation = %#v", snapshot, citation)
	}
	if strings.Contains(*messages[1].RetrievalSnapshot, "邻居上下文") || strings.Contains(*messages[1].RetrievalSnapshot, "anchor_content") {
		t.Fatalf("snapshot leaked internal context: %s", *messages[1].RetrievalSnapshot)
	}
}

func TestChatPublishesOnlyAnswerReferencedCitations(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	task := &model.VideoTask{UserID: 9, FileMD5: "efefefefefefefefefefefefefefefef", Filename: "answer-citations.mp4", FileURL: "videos/answer-citations.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	session := &model.ChatSession{UserID: 9, TaskID: task.ID, Title: "session"}
	if err := repos.Chat.CreateSession(session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	results := make([]RetrievedChunk, 0, 5)
	for i, content := range []string{
		"第一条证据：工具结果会作为新消息反馈给模型。",
		"第二条候选：系统提示词会声明工具列表。",
		"第三条候选：模型可以继续选择工具。",
		"第四条候选：最终会输出文件列表。",
		"第五条候选：这里讨论开源历史。",
	} {
		results = append(results, RetrievedChunk{
			EvidenceID: "ev-chat-" + string(rune('1'+i)),
			ChunkID:    int64(i + 1), ChunkIndex: i, Content: content, AnchorContent: content,
		})
	}
	chatClient := &scriptedChatClient{responses: []string{
		"not-json",
		"工具调用结果会以新消息形式反馈给模型 [C1, C3]。",
	}}
	svc := NewChatService(repos, &fakeRetriever{results: results}, ChatConfig{TopK: 5, CandidateK: 5, MinScore: 0.3})

	result, err := svc.Ask(context.Background(), 9, session.ID, "工具调用结果如何反馈给模型？", 5, &fakeEmbeddingClient{dim: 3}, chatClient, ai.Profile{
		EmbeddingModel: "text-embedding-3-small", LLMModel: "chat-model",
	})
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if len(chatClient.messages) != 2 {
		t.Fatalf("chat calls = %d, want rewrite and answer", len(chatClient.messages))
	}
	for _, content := range []string{"第一条证据", "第二条候选", "第三条候选", "第四条候选", "第五条候选"} {
		if !messagesContain(chatClient.messages[1], content) {
			t.Fatalf("answer prompt lost retrieval candidate %q: %+v", content, chatClient.messages[1])
		}
	}
	if result.Answer != "工具调用结果会以新消息形式反馈给模型。" {
		t.Fatalf("result answer = %q, want clean answer", result.Answer)
	}
	if len(result.Citations) != 2 || result.Citations[0].CitationID != "C1" || result.Citations[1].CitationID != "C3" {
		t.Fatalf("result citations = %+v, want C1 and C3", result.Citations)
	}

	messages, err := repos.Chat.ListMessages(9, session.ID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 2 || messages[1].RetrievalSnapshot == nil {
		t.Fatalf("messages = %+v", messages)
	}
	if messages[1].Content != result.Answer || strings.Contains(messages[1].Content, "[C") {
		t.Fatalf("stored assistant content = %q, want clean answer", messages[1].Content)
	}
	var snapshot []Citation
	if err := json.Unmarshal([]byte(*messages[1].RetrievalSnapshot), &snapshot); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	if len(snapshot) != 2 || snapshot[0].CitationID != "C1" || snapshot[1].CitationID != "C3" {
		t.Fatalf("snapshot citations = %+v, want only public C1 and C3", snapshot)
	}
}

func TestChatStreamEmitsFinalAnswerReferencedCitations(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	task := &model.VideoTask{UserID: 8, FileMD5: "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd", Filename: "stream-evidence.mp4", FileURL: "videos/stream-evidence.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	session := &model.ChatSession{UserID: 8, TaskID: task.ID, Title: "session"}
	if err := repos.Chat.CreateSession(session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	results := []RetrievedChunk{
		{EvidenceID: "ev-stream-1", ChunkID: 1, ChunkIndex: 1, Content: "第一条候选讲普通背景。", AnchorContent: "第一条候选讲普通背景。"},
		{EvidenceID: "ev-stream-2", ChunkID: 2, ChunkIndex: 2, Content: "第二条证据明确说明重试状态会持久化。", AnchorContent: "第二条证据明确说明重试状态会持久化。"},
		{EvidenceID: "ev-stream-3", ChunkID: 3, ChunkIndex: 3, Content: "第三条候选讲其他内容。", AnchorContent: "第三条候选讲其他内容。"},
	}
	chatClient := &streamingRecordingChatClient{streamed: []string{"第一条和第三条", " [C3, C1]"}}
	svc := NewChatService(repos, &fakeRetriever{results: results}, ChatConfig{TopK: 3, CandidateK: 3, MinScore: 0.3})

	var events []ChatStreamEvent
	result, err := svc.AskStream(context.Background(), 8, session.ID, "重试状态怎么保存？", 3, &fakeEmbeddingClient{dim: 3}, chatClient, ai.Profile{
		EmbeddingModel: "text-embedding-3-small", LLMModel: "chat-model",
	}, func(event ChatStreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("AskStream() error = %v", err)
	}

	lastAnswerIndex, citationIndex, doneIndex := -1, -1, -1
	var rawDeltas strings.Builder
	var emitted []Citation
	var doneData map[string]interface{}
	for i, event := range events {
		switch event.Type {
		case "answer":
			lastAnswerIndex = i
			delta, ok := event.Data.(string)
			if !ok {
				t.Fatalf("answer event data = %#v, want string", event.Data)
			}
			rawDeltas.WriteString(delta)
		case "citations":
			citationIndex = i
			var ok bool
			emitted, ok = event.Data.([]Citation)
			if !ok {
				t.Fatalf("citation event data = %#v, want []Citation", event.Data)
			}
		case "done":
			doneIndex = i
			var ok bool
			doneData, ok = event.Data.(map[string]interface{})
			if !ok {
				t.Fatalf("done event data = %#v, want map", event.Data)
			}
		}
	}
	if lastAnswerIndex < 0 || citationIndex <= lastAnswerIndex || doneIndex <= citationIndex {
		t.Fatalf("event order = %#v, want answer... -> citations -> done", events)
	}
	if len(emitted) != 2 || emitted[0].CitationID != "C1" || emitted[1].CitationID != "C3" {
		t.Fatalf("emitted citations = %+v, want stable candidate order C1, C3", emitted)
	}
	if rawDeltas.String() != "第一条和第三条 [C3, C1]" {
		t.Fatalf("raw answer deltas = %q", rawDeltas.String())
	}
	if result.Answer != "第一条和第三条" {
		t.Fatalf("result answer = %q, want clean answer", result.Answer)
	}
	if len(result.Citations) != 2 || result.Citations[0].CitationID != "C1" || result.Citations[1].CitationID != "C3" {
		t.Fatalf("result citations = %+v, want stable candidate order C1, C3", result.Citations)
	}
	if doneData["answer"] != result.Answer || doneData["model"] != result.Model || doneData["message_id"] != result.MessageID {
		t.Fatalf("done data = %#v, result = %+v", doneData, result)
	}

	messages, err := repos.Chat.ListMessages(8, session.ID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 2 || messages[1].RetrievalSnapshot == nil {
		t.Fatalf("messages = %+v", messages)
	}
	if messages[1].Content != result.Answer || strings.Contains(messages[1].Content, "[C") {
		t.Fatalf("stored assistant content = %q, want clean answer", messages[1].Content)
	}
	var snapshot []Citation
	if err := json.Unmarshal([]byte(*messages[1].RetrievalSnapshot), &snapshot); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	if len(snapshot) != 2 || snapshot[0].CitationID != "C1" || snapshot[1].CitationID != "C3" {
		t.Fatalf("snapshot citations = %+v, want stable candidate order C1, C3", snapshot)
	}
}

func TestRAGPromptTreatsCitationIDsAsInternalIndependentTokens(t *testing.T) {
	contexts := []RetrievedChunk{
		{ChunkID: 11, ChunkIndex: 3, Content: "第一条唯一证据内容"},
		{ChunkID: 22, ChunkIndex: 8, Content: "第二条唯一证据内容"},
	}
	messages := buildRAGMessages(contexts, nil, "发生了什么？")
	if len(messages) < 2 {
		t.Fatalf("buildRAGMessages() returned %d messages, want instruction and evidence context", len(messages))
	}
	for _, want := range []string{"内部标记", "最小充分证据", "[C1][C2]", "不要写成 [C1, C2]", "展示前隐藏"} {
		if !strings.Contains(messages[0].Content, want) {
			t.Fatalf("RAG instruction prompt = %q, missing %q", messages[0].Content, want)
		}
	}
	for index, context := range contexts {
		wantMapping := fmt.Sprintf("[C%d]\n%s\n%s", index+1, describeRetrievedChunk(context), context.Content)
		if !strings.Contains(messages[1].Content, wantMapping) {
			t.Fatalf("RAG evidence prompt = %q, missing concrete mapping %q", messages[1].Content, wantMapping)
		}
	}
}
