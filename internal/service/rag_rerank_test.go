package service

import (
	"context"
	"errors"
	"testing"

	"vid-lens/internal/ai"
)

func TestDeterministicRerankerKeywordHitOutranksWeakVectorOnlyHit(t *testing.T) {
	reranker := DeterministicReranker{}
	chunks := []RetrievedChunk{
		{ChunkID: 1, ChunkIndex: 1, Content: "这里主要讨论视频上传流程", Source: RetrievalSourceVector, VectorRank: 1, RRFScore: 0.05},
		{ChunkID: 2, ChunkIndex: 2, Content: "Redis 分布式锁释放必须校验 owner，否则有并发风险", Source: RetrievalSourceKeyword, KeywordRank: 1, RRFScore: 0.02},
	}

	reranked := reranker.Rerank(context.Background(), "Redis owner 风险", chunks, 2)

	if reranked[0].ChunkID != 2 {
		t.Fatalf("top chunk = %+v, want keyword hit chunk first", reranked[0])
	}
	if reranked[0].RerankScore <= reranked[1].RerankScore {
		t.Fatalf("rerank scores = %+v, want top score greater", reranked)
	}
	if reranked[0].FinalRank != 1 || reranked[1].FinalRank != 2 {
		t.Fatalf("final ranks = %+v, want 1 and 2", reranked)
	}
}

func TestDeterministicRerankerAdjacentChunkContinuityGivesSmallBoost(t *testing.T) {
	reranker := DeterministicReranker{}
	chunks := []RetrievedChunk{
		{ChunkID: 5, ChunkIndex: 5, Content: "Redis 锁需要 owner 标识", Source: RetrievalSourceHybrid, VectorRank: 1, KeywordRank: 2, RRFScore: 0.03},
		{ChunkID: 20, ChunkIndex: 20, Content: "Redis 锁需要 owner 标识", Source: RetrievalSourceHybrid, VectorRank: 2, KeywordRank: 3, RRFScore: 0.031},
		{ChunkID: 6, ChunkIndex: 6, Content: "Redis 锁需要 owner 标识", Source: RetrievalSourceHybrid, VectorRank: 3, KeywordRank: 1, RRFScore: 0.03},
	}

	reranked := reranker.Rerank(context.Background(), "Redis owner", chunks, 3)

	if reranked[0].ChunkIndex != 5 || reranked[1].ChunkIndex != 6 {
		t.Fatalf("reranked order = %+v, want adjacent chunks 5 and 6 first", reranked)
	}
}

func TestDeterministicRerankerNoQueryTermsKeepsRRFOrder(t *testing.T) {
	reranker := DeterministicReranker{}
	chunks := []RetrievedChunk{
		{ChunkID: 1, ChunkIndex: 2, Content: "chunk-2", RRFScore: 0.02},
		{ChunkID: 2, ChunkIndex: 1, Content: "chunk-1", RRFScore: 0.03},
	}

	reranked := reranker.Rerank(context.Background(), "?", chunks, 2)

	if reranked[0].ChunkID != 2 || reranked[1].ChunkID != 1 {
		t.Fatalf("reranked order = %+v, want original RRF order", reranked)
	}
	if reranked[0].FinalRank != 1 || reranked[1].FinalRank != 2 {
		t.Fatalf("final ranks = %+v, want populated ranks", reranked)
	}
}

func TestDeterministicRerankerCapsTopK(t *testing.T) {
	reranker := DeterministicReranker{}
	chunks := []RetrievedChunk{
		{ChunkID: 1, ChunkIndex: 1, Content: "Redis owner", RRFScore: 0.01},
		{ChunkID: 2, ChunkIndex: 2, Content: "Redis owner", RRFScore: 0.02},
	}

	reranked := reranker.Rerank(context.Background(), "Redis owner", chunks, 1)

	if len(reranked) != 1 {
		t.Fatalf("len(reranked) = %d, want 1", len(reranked))
	}
	if reranked[0].FinalRank != 1 || reranked[0].RerankScore == 0 {
		t.Fatalf("rerank trace not populated: %+v", reranked[0])
	}
}

type fakeRerankClient struct {
	results []ai.RerankResult
	err     error
}

func (c *fakeRerankClient) Rerank(_ context.Context, _ string, _ []string, _ int) ([]ai.RerankResult, error) {
	return c.results, c.err
}

func TestModelRerankerSortsByModelScores(t *testing.T) {
	reranker := NewModelReranker(&fakeRerankClient{results: []ai.RerankResult{
		{Index: 2, Score: 0.95},
		{Index: 0, Score: 0.40},
		{Index: 1, Score: 0.10},
	}})
	chunks := []RetrievedChunk{
		{ChunkID: 1, ChunkIndex: 1, Content: "weak"},
		{ChunkID: 2, ChunkIndex: 2, Content: "least relevant"},
		{ChunkID: 3, ChunkIndex: 3, Content: "best"},
	}

	reranked := reranker.Rerank(context.Background(), "question", chunks, 2)

	if len(reranked) != 2 {
		t.Fatalf("len(reranked) = %d, want 2", len(reranked))
	}
	if reranked[0].ChunkID != 3 || reranked[0].RerankScore != 0.95 || reranked[0].FinalRank != 1 {
		t.Fatalf("top reranked chunk = %+v, want chunk 3 score 0.95 rank 1", reranked[0])
	}
	if reranked[1].ChunkID != 1 || reranked[1].RerankScore != 0.40 || reranked[1].FinalRank != 2 {
		t.Fatalf("second reranked chunk = %+v, want chunk 1 score 0.40 rank 2", reranked[1])
	}
}

func TestModelRerankerFallsBackToOriginalOrderOnClientError(t *testing.T) {
	reranker := NewModelReranker(&fakeRerankClient{err: errors.New("rerank unavailable")})
	chunks := []RetrievedChunk{
		{ChunkID: 1, ChunkIndex: 1, Content: "first", CrossQueryRank: 1},
		{ChunkID: 2, ChunkIndex: 2, Content: "second", CrossQueryRank: 2},
	}

	reranked := reranker.Rerank(context.Background(), "question", chunks, 2)

	if len(reranked) != 2 || reranked[0].ChunkID != 1 || reranked[1].ChunkID != 2 {
		t.Fatalf("reranked = %+v, want original order preserved", reranked)
	}
	if len(reranked[0].Fallbacks) == 0 || reranked[0].Fallbacks[0] != "model_rerank_failed" {
		t.Fatalf("fallbacks = %+v, want model_rerank_failed", reranked[0].Fallbacks)
	}
}
