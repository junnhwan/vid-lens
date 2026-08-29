package service

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"vid-lens/internal/model"
)

func TestRetrievalPipelineNormalizesSameTaskIDsForEveryRewrite(t *testing.T) {
	embedding := &fakeEmbeddingClient{dim: 3}
	retriever := &pipelineTestRetriever{results: [][]RetrievedChunk{
		{{TaskID: 2, ChunkID: 20, ChunkIndex: 0, Content: "a"}},
		{{TaskID: 3, ChunkID: 30, ChunkIndex: 0, Content: "b"}},
	}}
	cfg := DefaultRAGRetrievalConfig()
	cfg.EnableBM25 = false
	cfg.QueryMode = QueryModeRewrite
	cfg.RewriteQueries = 2
	cfg.NeighborRadius = 0
	cfg.RerankerMode = RerankerModeNone
	cfg.RerankerVersion = ""
	pipeline := &RetrievalPipeline{retriever: retriever, rewriter: &pipelineTestRewriter{result: RewriteResult{Original: "q", Queries: []string{"q1", "q2"}}}, CandidateK: 5, Config: &cfg}

	_, err := pipeline.Retrieve(context.Background(), RetrievalPipelineRequest{UserID: 7, TaskIDs: []int64{3, 2, 3, -1}, Question: "q", EmbeddingModel: "embed", Embedding: embedding})
	if err != nil {
		t.Fatal(err)
	}
	if len(retriever.requests) != 2 {
		t.Fatalf("requests=%+v", retriever.requests)
	}
	for _, req := range retriever.requests {
		if !reflect.DeepEqual(req.TaskIDs, []int64{2, 3}) {
			t.Fatalf("task ids=%v", req.TaskIDs)
		}
	}
}

func TestRetrievalPipelineRejectsEmptyTaskIDs(t *testing.T) {
	pipeline := &RetrievalPipeline{retriever: &pipelineTestRetriever{}, CandidateK: 5}
	_, err := pipeline.Retrieve(context.Background(), RetrievalPipelineRequest{UserID: 7, Question: "q", EmbeddingModel: "embed", Embedding: &fakeEmbeddingClient{dim: 3}})
	if err == nil || !strings.Contains(err.Error(), "task_ids") {
		t.Fatalf("err=%v", err)
	}
}

func TestContextExpanderUsesEachCitationTaskID(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	chunks := []model.VideoChunk{
		{UserID: 7, TaskID: 1, ChunkIndex: 0, Content: "video-1 neighbor", EmbeddingModel: "embed", VectorID: "1-0", ContentHash: "h10"},
		{UserID: 7, TaskID: 1, ChunkIndex: 1, Content: "video-1 anchor", EmbeddingModel: "embed", VectorID: "1-1", ContentHash: "h11"},
		{UserID: 7, TaskID: 2, ChunkIndex: 0, Content: "video-2 neighbor", EmbeddingModel: "embed", VectorID: "2-0", ContentHash: "h20"},
		{UserID: 7, TaskID: 2, ChunkIndex: 1, Content: "video-2 anchor", EmbeddingModel: "embed", VectorID: "2-1", ContentHash: "h21"},
	}
	if err := repos.VideoChunk.ReplaceTaskChunks(1, "embed", chunks[:2]); err != nil {
		t.Fatal(err)
	}
	if err := repos.VideoChunk.ReplaceTaskChunks(2, "embed", chunks[2:]); err != nil {
		t.Fatal(err)
	}

	got, err := NewContextExpander(repos, 1, 1000).Expand(context.Background(), 7, 0, "embed", []RetrievedChunk{{TaskID: 2, ChunkIndex: 1, Content: "video-2 anchor"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !strings.Contains(got[0].Content, "video-2 neighbor") || strings.Contains(got[0].Content, "video-1") {
		t.Fatalf("expanded=%+v", got)
	}
}

func TestCitationAndPromptIncludeVideoSource(t *testing.T) {
	contexts, citations := buildCitationSet("owner", []RetrievedChunk{{TaskID: 12, VideoTitle: "并发控制课", EvidenceID: "e", ChunkID: 3, ChunkIndex: 4, Content: "owner 校验"}})
	if len(citations) != 1 || citations[0].TaskID != 12 || citations[0].VideoTitle != "并发控制课" {
		t.Fatalf("citations=%+v", citations)
	}
	messages := buildRAGMessages(contexts, nil, "q")
	if !strings.Contains(messages[1].Content, "并发控制课") || !strings.Contains(messages[1].Content, "task_id=12") {
		t.Fatalf("prompt=%s", messages[1].Content)
	}
}
