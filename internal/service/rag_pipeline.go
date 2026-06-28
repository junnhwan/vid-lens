package service

import (
	"context"
	"fmt"
	"sort"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

type RetrievalPipeline struct {
	repos     *repository.Repositories
	retriever RAGRetriever
	rewriter  QueryRewriter
	expander  *ContextExpander
	reranker  Reranker

	CandidateK int
	MinScore   float32
	RRFK       float64
}

type RetrievalPipelineRequest struct {
	UserID         int64
	TaskID         int64
	Question       string
	Recent         []model.ChatMessage
	TopK           int
	EmbeddingModel string
	Embedding      ai.EmbeddingClient
}

type RetrievalPipelineResult struct {
	Citations []RetrievedChunk
	Rewrite   RewriteResult
	Trace     RetrievalTrace
}

type RetrievalTrace struct {
	OriginalQuery    string   `json:"original_query,omitempty"`
	RewrittenQueries []string `json:"rewritten_queries,omitempty"`
	Fallbacks        []string `json:"fallbacks,omitempty"`
}

func (p *RetrievalPipeline) Retrieve(ctx context.Context, req RetrievalPipelineRequest) (RetrievalPipelineResult, error) {
	if p == nil || p.retriever == nil {
		return RetrievalPipelineResult{}, fmt.Errorf("当前视频尚未构建 RAG 索引")
	}
	if req.Embedding == nil {
		return RetrievalPipelineResult{}, fmt.Errorf("embedding client 不能为空")
	}
	topK := req.TopK
	if topK <= 0 {
		topK = 5
	}
	candidateK := p.CandidateK
	if candidateK <= 0 {
		candidateK = topK
	}
	if candidateK < topK {
		candidateK = topK
	}

	rewrite, rewriteErr := p.rewrite(ctx, req)
	trace := RetrievalTrace{
		OriginalQuery:    rewrite.Original,
		RewrittenQueries: append([]string(nil), rewrite.Queries...),
	}
	if rewriteErr != nil {
		trace.Fallbacks = appendFallback(trace.Fallbacks, "rewrite_failed")
	}

	perQuery := make([][]RetrievedChunk, 0, len(rewrite.Queries))
	for _, query := range rewrite.Queries {
		queryVector, err := req.Embedding.Embed(ctx, query)
		if err != nil {
			return RetrievalPipelineResult{}, err
		}
		vectorChunks, err := p.retriever.Search(ctx, queryVector, RetrievalRequest{
			UserID:         req.UserID,
			TaskID:         req.TaskID,
			EmbeddingModel: req.EmbeddingModel,
			TopK:           candidateK,
			MinScore:       p.MinScore,
		})
		if err != nil {
			return RetrievalPipelineResult{}, err
		}
		keywordChunks, err := p.keywordChunks(req.UserID, req.TaskID, req.EmbeddingModel, query, candidateK)
		if err != nil {
			return RetrievalPipelineResult{}, err
		}
		fused := FuseRetrievedChunks(vectorChunks, keywordChunks, candidateK, p.RRFK)
		for i := range fused {
			fused[i].MatchedQuery = query
		}
		perQuery = append(perQuery, fused)
	}

	citations := fuseCrossQueryChunks(perQuery, topK, p.RRFK)
	var err error
	if p.expander != nil {
		citations, err = p.expander.Expand(ctx, req.UserID, req.TaskID, req.EmbeddingModel, citations)
		if err != nil {
			return RetrievalPipelineResult{}, err
		}
	}
	if p.reranker != nil {
		citations = p.reranker.Rerank(ctx, req.Question, citations, topK)
	}
	return RetrievalPipelineResult{
		Citations: citations,
		Rewrite:   rewrite,
		Trace:     trace,
	}, nil
}

func (p *RetrievalPipeline) rewrite(ctx context.Context, req RetrievalPipelineRequest) (RewriteResult, error) {
	rewriter := p.rewriter
	if rewriter == nil {
		rewriter = NoopQueryRewriter{}
	}
	rewrite, err := rewriter.Rewrite(ctx, RewriteInput{
		Question:   req.Question,
		Recent:     req.Recent,
		NumQueries: 3,
	})
	if rewrite.Original == "" {
		rewrite.Original = req.Question
	}
	if len(rewrite.Queries) == 0 {
		rewrite.Queries = []string{req.Question}
		rewrite.Fallback = true
	}
	return rewrite, err
}

func (p *RetrievalPipeline) keywordChunks(userID, taskID int64, embeddingModel, query string, limit int) ([]RetrievedChunk, error) {
	if p.repos == nil || p.repos.VideoChunk == nil {
		return nil, nil
	}
	keywordResults, err := p.repos.VideoChunk.SearchByBM25(userID, taskID, embeddingModel, ExtractQueryTerms(query), limit)
	if err != nil {
		return nil, err
	}
	chunks := make([]RetrievedChunk, 0, len(keywordResults))
	for _, result := range keywordResults {
		chunks = append(chunks, RetrievedChunk{
			ChunkID:     result.Chunk.ID,
			ChunkIndex:  result.Chunk.ChunkIndex,
			Score:       float32(result.Score),
			Content:     result.Chunk.Content,
			Source:      RetrievalSourceKeyword,
			KeywordRank: result.Rank,
		})
	}
	return chunks, nil
}

func fuseCrossQueryChunks(rankLists [][]RetrievedChunk, topK int, k float64) []RetrievedChunk {
	if k <= 0 {
		k = defaultRRFK
	}
	type state struct {
		chunk      RetrievedChunk
		score      float64
		firstRank  int
		firstOrder int
	}
	states := make(map[string]*state)
	order := 0
	for _, chunks := range rankLists {
		for rank, chunk := range chunks {
			key := retrievalChunkKey(chunk)
			current := states[key]
			if current == nil {
				order++
				copied := chunk
				current = &state{chunk: copied, firstRank: rank + 1, firstOrder: order}
				states[key] = current
			}
			current.score += 1.0 / (k + float64(rank+1))
		}
	}
	fused := make([]state, 0, len(states))
	for _, current := range states {
		current.chunk.RRFScore = current.score
		current.chunk.Score = float32(current.score)
		fused = append(fused, *current)
	}
	sort.SliceStable(fused, func(i, j int) bool {
		if fused[i].score != fused[j].score {
			return fused[i].score > fused[j].score
		}
		if fused[i].firstRank != fused[j].firstRank {
			return fused[i].firstRank < fused[j].firstRank
		}
		return fused[i].firstOrder < fused[j].firstOrder
	})
	if topK > 0 && len(fused) > topK {
		fused = fused[:topK]
	}
	result := make([]RetrievedChunk, 0, len(fused))
	for i := range fused {
		chunk := fused[i].chunk
		chunk.CrossQueryRank = i + 1
		result = append(result, chunk)
	}
	return result
}
