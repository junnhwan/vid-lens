package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

type pipelineTestRetriever struct {
	results  [][]RetrievedChunk
	requests []RetrievalRequest
}

func (r *pipelineTestRetriever) Search(_ context.Context, _ []float32, req RetrievalRequest) ([]RetrievedChunk, error) {
	r.requests = append(r.requests, req)
	if len(r.results) == 0 {
		return nil, nil
	}
	result := r.results[0]
	r.results = r.results[1:]
	return result, nil
}

type pipelineTestRewriter struct {
	result RewriteResult
	err    error
	input  RewriteInput
}

func (r *pipelineTestRewriter) Rewrite(_ context.Context, input RewriteInput) (RewriteResult, error) {
	r.input = input
	return r.result, r.err
}

func TestRetrievalPipelineNoRewriterUsesOriginalQueryOnce(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	embedding := &fakeEmbeddingClient{dim: 3}
	retriever := &pipelineTestRetriever{results: [][]RetrievedChunk{{
		{ChunkID: 1, ChunkIndex: 1, Content: "vector hit", RRFScore: 0.1},
	}}}
	pipeline := &RetrievalPipeline{repos: repos, retriever: retriever, CandidateK: 3}

	result, err := pipeline.Retrieve(context.Background(), RetrievalPipelineRequest{
		UserID:         7,
		TaskID:         1,
		Question:       "原始问题",
		TopK:           2,
		EmbeddingModel: "text-embedding-3-small",
		Embedding:      embedding,
	})
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}
	if got, want := strings.Join(embedding.inputs, "|"), "原始问题"; got != want {
		t.Fatalf("embedding inputs = %q, want %q", got, want)
	}
	if len(retriever.requests) != 1 || retriever.requests[0].TopK != 3 {
		t.Fatalf("retriever requests = %+v, want one candidateK=3 request", retriever.requests)
	}
	if len(result.Citations) != 1 || result.Rewrite.Fallback {
		t.Fatalf("result = %+v, want one non-fallback citation", result)
	}
}

func TestRetrievalPipelineMultiQueryCallsVectorAndKeywordSearchPerQuery(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	seedVideoChunks(t, repos, 7, 1, "text-embedding-3-small", []string{
		"Redis 分布式锁片段", "owner 校验片段",
	})
	embedding := &fakeEmbeddingClient{dim: 3}
	rewriter := &pipelineTestRewriter{result: RewriteResult{
		Original: "那这个风险呢",
		Queries:  []string{"Redis 分布式锁", "owner 校验", "那这个风险呢"},
		UsedLLM:  true,
	}}
	retriever := &pipelineTestRetriever{results: [][]RetrievedChunk{
		{{ChunkID: 10, ChunkIndex: 10, Content: "vector redis"}},
		{{ChunkID: 11, ChunkIndex: 11, Content: "vector owner"}},
		{{ChunkID: 12, ChunkIndex: 12, Content: "vector original"}},
	}}
	pipeline := &RetrievalPipeline{repos: repos, retriever: retriever, rewriter: rewriter, CandidateK: 5}

	result, err := pipeline.Retrieve(context.Background(), RetrievalPipelineRequest{
		UserID:         7,
		TaskID:         1,
		Question:       "那这个风险呢",
		Recent:         []model.ChatMessage{{Role: "assistant", Content: "刚才在讲 Redis 分布式锁"}},
		TopK:           5,
		EmbeddingModel: "text-embedding-3-small",
		Embedding:      embedding,
	})
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}
	if len(embedding.inputs) != 3 || len(retriever.requests) != 3 {
		t.Fatalf("embedding inputs=%+v retriever requests=%+v, want 3 each", embedding.inputs, retriever.requests)
	}
	if !containsContent(result.Citations, "Redis 分布式锁片段") || !containsContent(result.Citations, "owner 校验片段") {
		t.Fatalf("citations = %+v, want keyword hits from rewritten queries", result.Citations)
	}
	if strings.Join(result.Trace.RewrittenQueries, "|") != "Redis 分布式锁|owner 校验|那这个风险呢" {
		t.Fatalf("trace queries = %+v", result.Trace.RewrittenQueries)
	}
}

func TestRetrievalPipelineCrossQueryRRFDedupesChunks(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	embedding := &fakeEmbeddingClient{dim: 3}
	rewriter := &pipelineTestRewriter{result: RewriteResult{
		Original: "q",
		Queries:  []string{"q1", "q2"},
	}}
	retriever := &pipelineTestRetriever{results: [][]RetrievedChunk{
		{{ChunkID: 1, ChunkIndex: 1, Content: "same chunk"}},
		{{ChunkID: 1, ChunkIndex: 1, Content: "same chunk"}},
	}}
	pipeline := &RetrievalPipeline{repos: repos, retriever: retriever, rewriter: rewriter, CandidateK: 2}

	result, err := pipeline.Retrieve(context.Background(), RetrievalPipelineRequest{
		UserID:         7,
		TaskID:         1,
		Question:       "q",
		TopK:           5,
		EmbeddingModel: "text-embedding-3-small",
		Embedding:      embedding,
	})
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}
	if len(result.Citations) != 1 {
		t.Fatalf("citations = %+v, want deduped single chunk", result.Citations)
	}
	if result.Citations[0].CrossQueryRank != 1 {
		t.Fatalf("cross query rank = %d, want 1", result.Citations[0].CrossQueryRank)
	}
}

func TestRetrievalPipelineRewriteFailureFallsBackToOriginalQuery(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	embedding := &fakeEmbeddingClient{dim: 3}
	rewriter := &pipelineTestRewriter{
		result: RewriteResult{Original: "原始问题", Queries: []string{"原始问题"}, Fallback: true},
		err:    errors.New("bad rewrite json"),
	}
	retriever := &pipelineTestRetriever{results: [][]RetrievedChunk{{
		{ChunkID: 1, ChunkIndex: 1, Content: "fallback hit"},
	}}}
	pipeline := &RetrievalPipeline{repos: repos, retriever: retriever, rewriter: rewriter, CandidateK: 2}

	result, err := pipeline.Retrieve(context.Background(), RetrievalPipelineRequest{
		UserID:         7,
		TaskID:         1,
		Question:       "原始问题",
		TopK:           2,
		EmbeddingModel: "text-embedding-3-small",
		Embedding:      embedding,
	})
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}
	if len(embedding.inputs) != 1 || embedding.inputs[0] != "原始问题" {
		t.Fatalf("embedding inputs = %+v, want original fallback query", embedding.inputs)
	}
	if !result.Rewrite.Fallback || !containsFallback(result.Trace.Fallbacks, "rewrite_failed") {
		t.Fatalf("result trace = %+v, want rewrite_failed fallback", result)
	}
}

func TestRetrievalPipelineInvokesExpanderAndRerankerWhenConfigured(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	seedVideoChunks(t, repos, 7, 1, "text-embedding-3-small", []string{
		"chunk-0 Redis", "chunk-1 Redis owner", "chunk-2 Redis",
	})
	embedding := &fakeEmbeddingClient{dim: 3}
	retriever := &pipelineTestRetriever{results: [][]RetrievedChunk{{
		{ChunkID: 2, ChunkIndex: 1, Content: "chunk-1 Redis owner", RRFScore: 0.01},
	}}}
	pipeline := &RetrievalPipeline{
		repos:      repos,
		retriever:  retriever,
		expander:   &ContextExpander{repos: repos, Radius: 1, MaxCharsPerCitation: 200},
		reranker:   DeterministicReranker{},
		CandidateK: 2,
	}

	result, err := pipeline.Retrieve(context.Background(), RetrievalPipelineRequest{
		UserID:         7,
		TaskID:         1,
		Question:       "Redis owner",
		TopK:           1,
		EmbeddingModel: "text-embedding-3-small",
		Embedding:      embedding,
	})
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}
	if len(result.Citations) != 1 {
		t.Fatalf("citations = %+v, want one", result.Citations)
	}
	if !strings.Contains(result.Citations[0].Content, "chunk-0 Redis") || result.Citations[0].FinalRank != 1 {
		t.Fatalf("citation = %+v, want expanded and reranked citation", result.Citations[0])
	}
}

func containsContent(chunks []RetrievedChunk, content string) bool {
	for _, chunk := range chunks {
		if strings.Contains(chunk.Content, content) {
			return true
		}
	}
	return false
}

var _ ai.EmbeddingClient = (*fakeEmbeddingClient)(nil)
