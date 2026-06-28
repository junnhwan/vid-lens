package service

import (
	"context"
	"sort"
	"strings"
)

type Reranker interface {
	Rerank(ctx context.Context, question string, chunks []RetrievedChunk, topK int) []RetrievedChunk
}

type DeterministicReranker struct{}

func (DeterministicReranker) Rerank(_ context.Context, question string, chunks []RetrievedChunk, topK int) []RetrievedChunk {
	if len(chunks) == 0 {
		return nil
	}
	type scoredChunk struct {
		chunk        RetrievedChunk
		originalRank int
	}

	terms := ExtractQueryTerms(question)
	adjacent := make(map[int]bool, len(chunks))
	indices := make(map[int]bool, len(chunks))
	for _, chunk := range chunks {
		indices[chunk.ChunkIndex] = true
	}
	for _, chunk := range chunks {
		if indices[chunk.ChunkIndex-1] || indices[chunk.ChunkIndex+1] {
			adjacent[chunk.ChunkIndex] = true
		}
	}

	scored := make([]scoredChunk, 0, len(chunks))
	for i, chunk := range chunks {
		chunk.RerankScore = deterministicRerankScore(chunk, terms, adjacent[chunk.ChunkIndex])
		scored = append(scored, scoredChunk{chunk: chunk, originalRank: i + 1})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		left, right := scored[i], scored[j]
		if left.chunk.RerankScore != right.chunk.RerankScore {
			return left.chunk.RerankScore > right.chunk.RerankScore
		}
		if left.chunk.RRFScore != right.chunk.RRFScore {
			return left.chunk.RRFScore > right.chunk.RRFScore
		}
		if left.originalRank != right.originalRank {
			return left.originalRank < right.originalRank
		}
		return left.chunk.ChunkIndex < right.chunk.ChunkIndex
	})

	limit := len(scored)
	if topK > 0 && topK < limit {
		limit = topK
	}
	reranked := make([]RetrievedChunk, 0, limit)
	for i := 0; i < limit; i++ {
		chunk := scored[i].chunk
		chunk.FinalRank = i + 1
		reranked = append(reranked, chunk)
	}
	return reranked
}

func deterministicRerankScore(chunk RetrievedChunk, terms []string, adjacent bool) float64 {
	score := chunk.RRFScore
	if score == 0 && chunk.Score > 0 {
		score = float64(chunk.Score)
	}
	if len(terms) > 0 {
		score += queryTermCoverage(chunk.Content, terms) * 0.20
	}
	switch chunk.Source {
	case RetrievalSourceHybrid:
		score += 0.04
	case RetrievalSourceKeyword:
		score += 0.03
	case RetrievalSourceVector:
		score += 0.01
	}
	if adjacent {
		score += 0.012
	}
	return score
}

func queryTermCoverage(content string, terms []string) float64 {
	if len(terms) == 0 {
		return 0
	}
	content = strings.ToLower(content)
	var hits int
	for _, term := range terms {
		if strings.Contains(content, strings.ToLower(term)) {
			hits++
		}
	}
	return float64(hits) / float64(len(terms))
}
