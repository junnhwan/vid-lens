package service

import (
	"context"
	"sort"
	"strings"

	"vid-lens/internal/ai"
)

type Reranker interface {
	Rerank(ctx context.Context, question string, chunks []RetrievedChunk, topK int) []RetrievedChunk
}

type DeterministicReranker struct{}

type ModelReranker struct {
	client ai.RerankClient
}

func NewModelReranker(client ai.RerankClient) *ModelReranker {
	return &ModelReranker{client: client}
}

func (r *ModelReranker) Rerank(ctx context.Context, question string, chunks []RetrievedChunk, topK int) []RetrievedChunk {
	if len(chunks) == 0 {
		return nil
	}
	if r == nil || r.client == nil {
		return fallbackRerankOrder(chunks, topK, "model_rerank_unavailable")
	}
	documents := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		documents = append(documents, chunk.Content)
	}
	results, err := r.client.Rerank(ctx, question, documents, len(chunks))
	if err != nil || len(results) == 0 {
		return fallbackRerankOrder(chunks, topK, "model_rerank_failed")
	}

	type scoredChunk struct {
		chunk        RetrievedChunk
		modelRank    int
		originalRank int
		scored       bool
	}
	scored := make([]scoredChunk, 0, len(chunks))
	used := make(map[int]bool, len(results))
	for i, result := range results {
		if result.Index < 0 || result.Index >= len(chunks) || used[result.Index] {
			continue
		}
		used[result.Index] = true
		chunk := chunks[result.Index]
		chunk.RerankScore = result.Score
		scored = append(scored, scoredChunk{
			chunk:        chunk,
			modelRank:    i + 1,
			originalRank: result.Index + 1,
			scored:       true,
		})
	}
	if len(scored) == 0 {
		return fallbackRerankOrder(chunks, topK, "model_rerank_failed")
	}
	for i, chunk := range chunks {
		if used[i] {
			continue
		}
		scored = append(scored, scoredChunk{chunk: chunk, originalRank: i + 1})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		left, right := scored[i], scored[j]
		if left.scored != right.scored {
			return left.scored
		}
		if left.chunk.RerankScore != right.chunk.RerankScore {
			return left.chunk.RerankScore > right.chunk.RerankScore
		}
		if left.modelRank != right.modelRank {
			return left.modelRank < right.modelRank
		}
		return left.originalRank < right.originalRank
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

func (DeterministicReranker) Rerank(_ context.Context, question string, chunks []RetrievedChunk, topK int) []RetrievedChunk {
	if len(chunks) == 0 {
		return nil
	}
	type scoredChunk struct {
		chunk        RetrievedChunk
		originalRank int
	}

	terms := ExtractQueryTerms(question)
	type chunkPosition struct {
		taskID int64
		index  int
	}
	adjacent := make(map[chunkPosition]bool, len(chunks))
	indices := make(map[chunkPosition]bool, len(chunks))
	for _, chunk := range chunks {
		indices[chunkPosition{chunk.TaskID, chunk.ChunkIndex}] = true
	}
	for _, chunk := range chunks {
		key := chunkPosition{chunk.TaskID, chunk.ChunkIndex}
		if indices[chunkPosition{chunk.TaskID, chunk.ChunkIndex - 1}] || indices[chunkPosition{chunk.TaskID, chunk.ChunkIndex + 1}] {
			adjacent[key] = true
		}
	}

	scored := make([]scoredChunk, 0, len(chunks))
	for i, chunk := range chunks {
		chunk.RerankScore = deterministicRerankScore(chunk, terms, adjacent[chunkPosition{chunk.TaskID, chunk.ChunkIndex}])
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

func fallbackRerankOrder(chunks []RetrievedChunk, topK int, reason string) []RetrievedChunk {
	limit := len(chunks)
	if topK > 0 && topK < limit {
		limit = topK
	}
	fallback := make([]RetrievedChunk, 0, limit)
	for i := 0; i < limit; i++ {
		chunk := chunks[i]
		chunk.FinalRank = i + 1
		chunk.Fallbacks = appendFallback(chunk.Fallbacks, reason)
		fallback = append(fallback, chunk)
	}
	return fallback
}
