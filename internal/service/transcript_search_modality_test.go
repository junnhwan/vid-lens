package service

import (
	"context"
	"testing"
	"vid-lens/internal/model"
)

func TestTranscriptSearchDoesNotReturnVisualHits(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	retriever := &pipelineTestRetriever{results: [][]RetrievedChunk{{
		{TaskID: 1, ChunkID: 1, Content: "视觉结果", Modality: model.ChunkModalityVisualOCR},
		{TaskID: 1, ChunkID: 2, Content: "原文转写", Modality: model.ChunkModalityTranscript},
		{TaskID: 1, ChunkID: 3, Content: "旧版未知来源", Modality: model.ChunkModalityUnknown},
	}}}
	tools := NewVideoAgentTools(repos, &RetrievalPipeline{retriever: retriever, rewriter: NoopQueryRewriter{}, CandidateK: 5}, &recordingChatClient{})
	result, _, err := tools.SearchTranscript(context.Background(), SearchTranscriptInput{UserID: 7, TaskID: 1, Question: "框架", TopK: 5, EmbeddingModel: "embed", Embedding: &fakeEmbeddingClient{dim: 3}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Citations) != 2 {
		t.Fatalf("citations=%+v", result.Citations)
	}
	for _, c := range result.Citations {
		if c.Modality == model.ChunkModalityVisualOCR {
			t.Fatal("visual hit escaped transcript tool")
		}
		if c.ChunkID == 3 && c.Modality != model.ChunkModalityUnknown {
			t.Fatal("legacy source relabeled transcript")
		}
	}
}
