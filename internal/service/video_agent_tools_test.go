package service

import (
	"context"
	"strings"
	"testing"

	"vid-lens/internal/ai"
)

func TestVideoAgentToolSearchTranscriptCallsRetrievalPipeline(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	embedding := &fakeEmbeddingClient{dim: 3}
	retriever := &pipelineTestRetriever{results: [][]RetrievedChunk{{
		{ChunkID: 1, ChunkIndex: 2, Content: "Redis owner 风险片段"},
	}}}
	pipeline := &RetrievalPipeline{repos: repos, retriever: retriever, rewriter: NoopQueryRewriter{}, CandidateK: 5}
	tools := NewVideoAgentTools(repos, pipeline, &recordingChatClient{})

	result, step, err := tools.SearchTranscript(context.Background(), SearchTranscriptInput{
		UserID:         7,
		TaskID:         1,
		Question:       "Redis owner 风险",
		TopK:           3,
		EmbeddingModel: "text-embedding-3-small",
		Embedding:      embedding,
	})
	if err != nil {
		t.Fatalf("SearchTranscript() error = %v", err)
	}
	if len(result.Citations) != 1 || result.Citations[0].Content != "Redis owner 风险片段" {
		t.Fatalf("citations = %+v", result.Citations)
	}
	if len(embedding.inputs) != 1 || embedding.inputs[0] != "Redis owner 风险" {
		t.Fatalf("embedding inputs = %+v", embedding.inputs)
	}
	if step.Tool != VideoAgentToolSearchTranscript || step.Error != "" || step.OutputRef == "" {
		t.Fatalf("step = %+v", step)
	}
}

func TestVideoAgentToolGetTranscriptWindowLoadsNeighborChunks(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	seedVideoChunks(t, repos, 7, 1, "text-embedding-3-small", []string{
		"chunk-0", "chunk-1 owner", "chunk-2 risk",
	})
	tools := NewVideoAgentTools(repos, nil, &recordingChatClient{})

	result, step, err := tools.GetTranscriptWindow(context.Background(), TranscriptWindowInput{
		UserID:         7,
		TaskID:         1,
		EmbeddingModel: "text-embedding-3-small",
		ChunkIndex:     1,
		Radius:         1,
	})
	if err != nil {
		t.Fatalf("GetTranscriptWindow() error = %v", err)
	}
	if result.StartIndex != 0 || result.EndIndex != 2 {
		t.Fatalf("window range = %d-%d, want 0-2", result.StartIndex, result.EndIndex)
	}
	if !strings.Contains(result.Content, "chunk-0") || !strings.Contains(result.Content, "chunk-2 risk") {
		t.Fatalf("content = %q", result.Content)
	}
	if step.Tool != VideoAgentToolGetTranscriptWindow || step.Error != "" {
		t.Fatalf("step = %+v", step)
	}
}

func TestVideoAgentToolSummarizeSegmentsCallsChatClient(t *testing.T) {
	chatClient := &scriptedChatClient{responses: []string{"总结结果"}}
	tools := NewVideoAgentTools(nil, nil, chatClient)

	result, step, err := tools.SummarizeSegments(context.Background(), SummarizeSegmentsInput{
		Question: "总结 Redis 风险",
		Segments: []TranscriptSegment{
			{ChunkIndex: 1, Content: "Redis owner 风险"},
			{ChunkIndex: 2, Content: "释放锁要校验 owner"},
		},
	})
	if err != nil {
		t.Fatalf("SummarizeSegments() error = %v", err)
	}
	if result.Summary != "总结结果" {
		t.Fatalf("summary = %q", result.Summary)
	}
	if len(chatClient.messages) != 1 || !messagesContain(chatClient.messages[0], "Redis owner 风险") {
		t.Fatalf("chat messages = %+v", chatClient.messages)
	}
	if step.Tool != VideoAgentToolSummarizeSegments || step.OutputRef == "" {
		t.Fatalf("step = %+v", step)
	}
}

func TestVideoAgentToolCompareSegmentsCallsChatClientWithGroups(t *testing.T) {
	chatClient := &scriptedChatClient{responses: []string{"对比结果"}}
	tools := NewVideoAgentTools(nil, nil, chatClient)

	result, step, err := tools.CompareSegments(context.Background(), CompareSegmentsInput{
		Question: "对比前后变化",
		Groups: []TranscriptSegmentGroup{
			{Label: "A", Segments: []TranscriptSegment{{ChunkIndex: 1, Content: "前半段说法"}}},
			{Label: "B", Segments: []TranscriptSegment{{ChunkIndex: 8, Content: "后半段说法"}}},
		},
	})
	if err != nil {
		t.Fatalf("CompareSegments() error = %v", err)
	}
	if result.Comparison != "对比结果" {
		t.Fatalf("comparison = %q", result.Comparison)
	}
	if len(chatClient.messages) != 1 || !messagesContain(chatClient.messages[0], "前半段说法") || !messagesContain(chatClient.messages[0], "后半段说法") {
		t.Fatalf("chat messages = %+v", chatClient.messages)
	}
	if step.Tool != VideoAgentToolCompareSegments || step.OutputRef == "" {
		t.Fatalf("step = %+v", step)
	}
}

func TestVideoAgentToolBuildCitedAnswerPreservesCitations(t *testing.T) {
	chatClient := &scriptedChatClient{responses: []string{"最终回答"}}
	tools := NewVideoAgentTools(nil, nil, chatClient)
	citations := []RetrievedChunk{
		{ChunkID: 10, ChunkIndex: 3, Content: "第一条唯一引用片段"},
		{ChunkID: 20, ChunkIndex: 7, Content: "第二条唯一引用片段"},
	}

	result, step, err := tools.BuildCitedAnswer(context.Background(), BuildCitedAnswerInput{
		Question:     "为什么要校验 owner？",
		Intermediate: "中间总结",
		Citations:    citations,
	})
	if err != nil {
		t.Fatalf("BuildCitedAnswer() error = %v", err)
	}
	if result.Answer != "最终回答" {
		t.Fatalf("answer = %q", result.Answer)
	}
	if len(result.Citations) != 2 || result.Citations[0].ChunkID != 10 || result.Citations[1].ChunkID != 20 {
		t.Fatalf("citations = %+v", result.Citations)
	}
	if len(chatClient.messages) != 1 || len(chatClient.messages[0]) < 2 {
		t.Fatalf("chat messages = %+v", chatClient.messages)
	}
	for _, want := range []string{"内部标记", "[C1][C2]", "不要写成 [C1, C2]", "展示前隐藏"} {
		if !strings.Contains(chatClient.messages[0][0].Content, want) {
			t.Fatalf("agent instruction prompt = %q, missing %q", chatClient.messages[0][0].Content, want)
		}
	}
	for _, wantMapping := range []string{
		"[C1] (chunk 3) 第一条唯一引用片段",
		"[C2] (chunk 7) 第二条唯一引用片段",
	} {
		if !strings.Contains(chatClient.messages[0][1].Content, wantMapping) {
			t.Fatalf("agent evidence prompt = %q, missing concrete mapping %q", chatClient.messages[0][1].Content, wantMapping)
		}
	}
	if step.Tool != VideoAgentToolBuildCitedAnswer || step.OutputRef == "" {
		t.Fatalf("step = %+v", step)
	}
}

func messagesContain(messages []ai.ChatMessage, want string) bool {
	for _, message := range messages {
		if strings.Contains(message.Content, want) {
			return true
		}
	}
	return false
}
