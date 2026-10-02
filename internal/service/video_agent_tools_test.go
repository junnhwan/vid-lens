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

func TestVideoAgentWriterAndObserverUseSameTailSentenceCandidate(t *testing.T) {
	var source strings.Builder
	for i := 0; i < 90; i++ {
		fmt.Fprintf(&source, "第%d句独立背景材料。", i)
	}
	tail := "末尾明确说明应用层缓存可以取消。"
	source.WriteString(tail)
	canonical := RetrievedChunk{TaskID: 65, EvidenceID: "tail-evidence", ChunkID: 9,
		Content: source.String(), Modality: model.ChunkModalityTranscript,
		StartMS: 0, EndMS: 305000, TimeRangeStatus: model.ChunkTimeRangeCoarse, SourceMappingStatus: model.ChunkSourceMapped,
		SourceRefs: []ChunkSourceRef{{StableID: "tail-atom", Content: tail, StartMS: 250000, EndMS: 260000, TimeRangeStatus: model.ChunkTimeRangeExact}}}
	candidates := buildCitations("应用层缓存", []RetrievedChunk{canonical})
	tailCandidate := candidates[len(candidates)-1]
	client := &scriptedChatClient{responses: []string{"可以取消应用层缓存 [" + tailCandidate.CitationID + "]。"}}
	result, _, err := NewVideoAgentTools(nil, nil, client).BuildCitedAnswer(context.Background(), BuildCitedAnswerInput{Question: "应用层缓存？", Citations: []RetrievedChunk{canonical}})
	if err != nil {
		t.Fatal(err)
	}
	if !messagesContain(client.messages[0], "["+tailCandidate.CitationID+"] (task_id=65") || !messagesContain(client.messages[0], tail) {
		t.Fatalf("writer lost tail after contextual 600-rune bound: %+v", client.messages)
	}
	raw, _ := json.Marshal(result)
	observed, err := (DefaultVideoAgentLoopObserver{}).Observe(VideoAgentLoopState{Goal: "应用层缓存？", Evidence: []RetrievedChunk{canonical}}, VideoAgentToolResult{Step: VideoAgentStep{Tool: VideoAgentToolBuildCitedAnswer}, Output: raw})
	if err != nil || len(observed.Citations) != 1 || observed.Citations[0].CitationID != tailCandidate.CitationID || observed.Citations[0].Content != tail || observed.Citations[0].StartMS != 250000 || observed.Citations[0].EndMS != 260000 {
		t.Fatalf("writer/public evidence drift: %+v %v", observed, err)
	}
}

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

func TestVideoAgentToolBuildCitedAnswerPreservesCitations(t *testing.T) {
	chatClient := &scriptedChatClient{responses: []string{"最终回答"}}
	tools := NewVideoAgentTools(nil, nil, chatClient)
	citations := []RetrievedChunk{
		{ChunkID: 10, ChunkIndex: 3, Content: "第一条唯一引用片段", Modality: model.ChunkModalityTranscript, StartMS: 1000, EndMS: 2000, TimeRangeStatus: model.ChunkTimeRangeCoarse, SourceMappingStatus: model.ChunkSourceMapped},
		{ChunkID: 20, ChunkIndex: 7, Content: "第二条唯一引用片段", Modality: model.ChunkModalityVisualOCR, StartMS: 3000, EndMS: 3001, TimeRangeStatus: model.ChunkTimeRangeExact, SourceMappingStatus: model.ChunkSourceMapped},
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
	for _, want := range []string{"行内引用链接", "[C1][C2]", "不要写成 [C1, C2]", "一句原文"} {
		if !strings.Contains(chatClient.messages[0][0].Content, want) {
			t.Fatalf("agent instruction prompt = %q, missing %q", chatClient.messages[0][0].Content, want)
		}
	}
	for _, wantMapping := range []string{
		"[C1] (task_id=0, chunk 3, modality=transcript, time=[1000,2000), time_status=coarse) 第一条唯一引用片段",
		"[C2] (task_id=0, chunk 7, modality=visual_ocr, time=[3000,3001), time_status=exact) 第二条唯一引用片段",
	} {
		if !strings.Contains(chatClient.messages[0][1].Content, wantMapping) {
			t.Fatalf("agent evidence prompt = %q, missing concrete mapping %q", chatClient.messages[0][1].Content, wantMapping)
		}
	}
	if step.Tool != VideoAgentToolBuildCitedAnswer || step.OutputRef == "" {
		t.Fatalf("step = %+v", step)
	}
}

func TestInspectVisualWindowEnforcesScopeRangeAndFrameBudget(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	refs, err := MarshalChunkSourceRefs([]ChunkSourceRef{{SourceType: model.ChunkModalityVisualOCR, StableID: "visual-frame:vf-1", SourceRowID: 10, StartMS: 10_000, EndMS: 10_001, TimeRangeStatus: model.ChunkTimeRangeExact}})
	if err != nil {
		t.Fatal(err)
	}
	rows := []model.VideoChunk{
		{UserID: 7, TaskID: 11, ChunkIndex: 1, Content: "[画面OCR 00:10] 字幕 A", ContentHash: "a", EmbeddingModel: "embed", EmbeddingDim: 3, VectorID: "visual-a", Modality: model.ChunkModalityVisualOCR, StartMS: 10_000, EndMS: 10_001, TimeRangeStatus: model.ChunkTimeRangeExact, SourceMappingStatus: model.ChunkSourceMapped, SourceRefs: refs},
		{UserID: 7, TaskID: 12, ChunkIndex: 2, Content: "其他视频", ContentHash: "b", EmbeddingModel: "embed", EmbeddingDim: 3, VectorID: "visual-other", Modality: model.ChunkModalityVisualOCR, StartMS: 10_000, EndMS: 10_001, TimeRangeStatus: model.ChunkTimeRangeExact, SourceMappingStatus: model.ChunkSourceMapped, SourceRefs: refs},
	}
	if err := repos.VideoChunk.ReplaceTaskChunks(11, "embed", rows[:1]); err != nil {
		t.Fatal(err)
	}
	if err := repos.VideoChunk.ReplaceTaskChunks(12, "embed", rows[1:]); err != nil {
		t.Fatal(err)
	}
	tools := NewVideoAgentTools(repos, nil, nil)
	result, _, err := tools.InspectVisualWindow(context.Background(), InspectVisualWindowInput{UserID: 7, TaskID: 11, EmbeddingModel: "embed", StartMS: 9_000, EndMS: 11_000, MaxFrames: 1})
	if err != nil || len(result.Evidence) != 1 || result.Evidence[0].EvidenceID != "visual-a" || result.Evidence[0].TaskID != 11 {
		t.Fatalf("inspection = %+v, err=%v", result, err)
	}
	if _, _, err := tools.InspectVisualWindow(context.Background(), InspectVisualWindowInput{UserID: 7, TaskID: 11, EmbeddingModel: "embed", StartMS: 0, EndMS: 11 * 60 * 1000}); err == nil {
		t.Fatal("oversized visual window was accepted")
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
