package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"vid-lens/internal/repository"
)

var errRetrievalScope = errors.New("retriever returned evidence outside the authorized scope")

func containsTaskID(ids []int64, id int64) bool {
	for _, v := range ids {
		if id == v {
			return true
		}
	}
	return false
}
func isComparisonQuestion(q string) bool {
	return containsAny(strings.ToLower(q), "比较", "对比", "区别", "compare", "versus", " vs ")
}

func (p *RetrievalPipeline) comparisonTargets(userID int64, ids []int64, q string) ([]int64, error) {
	if p.repos == nil || p.repos.Task == nil {
		return nil, nil
	}
	tasks, err := p.repos.Task.ListByIDsForUser(userID, ids)
	if err != nil {
		return nil, err
	}
	var targets []int64
	for _, task := range tasks {
		title := strings.TrimSpace(task.Title)
		if title == "" {
			title = task.Filename
		}
		if title != "" && strings.Contains(strings.ToLower(q), strings.ToLower(title)) {
			targets = append(targets, task.ID)
		}
	}
	// An unspecified comparison of a two-member collection has an unambiguous pair.
	if len(targets) == 0 && len(ids) == 2 {
		return ids, nil
	}
	return targets, nil
}

// Independent channels run concurrently. One embedding is reused for all targets;
// target vector searches have a hard two-worker limit. Scope and cancellation never degrade.
func (p *RetrievalPipeline) retrieveChannels(ctx context.Context, req RetrievalPipelineRequest, ids, targets []int64, query string, k int, minScore float32, vector, keyword bool, corpus *repository.BM25Corpus, corpusErr error) ([]RetrievedChunk, []RetrievedChunk, []string, error) {
	var vc, kc []RetrievedChunk
	var ve, ke error
	var wg sync.WaitGroup
	if vector {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if p.retriever == nil || req.Embedding == nil {
				ve = errors.New("vector channel unavailable")
				return
			}
			embedding, err := p.embedRequestQuery(ctx, req, query)
			if err != nil {
				ve = err
				return
			}
			scopes := [][]int64{ids}
			for _, id := range targets {
				if len(ids) > 1 {
					scopes = append(scopes, []int64{id})
				}
			}
			results := make([][]RetrievedChunk, len(scopes))
			errs := make([]error, len(scopes))
			jobs := make(chan int, len(scopes))
			for i := range scopes {
				jobs <- i
			}
			close(jobs)
			var workers sync.WaitGroup
			for worker := 0; worker < min(2, len(scopes)); worker++ {
				workers.Add(1)
				go func() {
					defer workers.Done()
					for i := range jobs {
						searchK := k
						if len(req.TimeRanges) > 0 {
							searchK = max(50, searchK)
						}
						r := RetrievalRequest{UserID: req.UserID, TaskIDs: scopes[i], EmbeddingModel: req.EmbeddingModel, TopK: searchK, MinScore: minScore, TimeRanges: req.TimeRanges, Modalities: req.Modalities}
						if len(scopes[i]) == 1 {
							r.TaskID = scopes[i][0]
						}
						results[i], errs[i] = p.retriever.Search(ctx, embedding, r)
						for j := range results[i] {
							c := &results[i][j]
							if c.TaskID == 0 && len(scopes[i]) == 1 {
								c.TaskID = scopes[i][0]
							}
							if !containsTaskID(scopes[i], c.TaskID) {
								errs[i] = errRetrievalScope
							}
						}
					}
				}()
			}
			workers.Wait()
			for i := range results {
				if errs[i] != nil {
					ve = errs[i]
					if errors.Is(ve, errRetrievalScope) {
						return
					}
					continue
				}
				vc = append(vc, results[i]...)
			}
		}()
	}
	if keyword {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if corpusErr != nil {
				ke = corpusErr
				return
			}
			if corpus == nil {
				ke = errors.New("keyword channel unavailable")
				return
			}
			scopes := [][]int64{ids}
			for _, id := range targets {
				if len(ids) > 1 {
					scopes = append(scopes, []int64{id})
				}
			}
			for _, scope := range scopes {
				rows, err := corpus.Search(ctx, ExtractQueryTerms(query), scope, k)
				if err != nil {
					ke = err
					return
				}
				for _, row := range rows {
					c := RetrievedChunk{TaskID: row.Chunk.TaskID, EvidenceID: row.Chunk.VectorID, ChunkID: row.Chunk.ID, ChunkIndex: row.Chunk.ChunkIndex, Score: float32(row.Score), Content: row.Chunk.Content, Source: RetrievalSourceKeyword, KeywordRank: row.Rank}
					applyChunkProvenance(&c, row.Chunk)
					kc = append(kc, c)
				}
			}
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}
	if errors.Is(ve, errRetrievalScope) || errors.Is(ve, context.Canceled) || errors.Is(ve, context.DeadlineExceeded) {
		return nil, nil, nil, ve
	}
	var fallbacks []string
	if ve != nil {
		fallbacks = append(fallbacks, "vector_channel_failed")
	}
	if ke != nil {
		fallbacks = append(fallbacks, "keyword_channel_failed")
	}
	if (len(vc) == 0 && len(kc) == 0 && (ve != nil || ke != nil)) || ((!vector || (ve != nil && len(vc) == 0)) && (!keyword || ke != nil)) {
		return nil, nil, fallbacks, fmt.Errorf("all retrieval channels failed: vector=%v keyword=%v", ve, ke)
	}
	return vc, kc, fallbacks, nil
}

// Only already relevant candidates enter this stage. Preserve independent sources,
// negations and quantities; exact same-source duplicates cannot consume the budget.
func selectDiverseEvidence(chunks []RetrievedChunk, limit int, required []int64) []RetrievedChunk {
	var unique []RetrievedChunk
	seen := map[string]bool{}
	for _, c := range chunks {
		key := fmt.Sprintf("%d:%s:%s", c.TaskID, c.Modality, strings.Join(strings.Fields(strings.ToLower(c.Content)), " "))
		if seen[key] {
			continue
		}
		seen[key] = true
		unique = append(unique, c)
	}
	var result []RetrievedChunk
	used := map[int]bool{}
	for _, id := range required {
		for i, c := range unique {
			if c.TaskID == id && len(result) < limit {
				result = append(result, c)
				used[i] = true
				break
			}
		}
	}
	for len(result) < limit {
		best := -1
		bestScore := -1e9
		for i, c := range unique {
			if used[i] {
				continue
			}
			score := 1.0 / float64(i+1)
			for _, chosen := range result {
				if c.TaskID == chosen.TaskID {
					score -= .12
					if c.Modality == chosen.Modality && strings.Contains(chosen.Content, c.Content) {
						score -= 1
					}
				}
			}
			if score > bestScore {
				best, bestScore = i, score
			}
		}
		if best < 0 {
			break
		}
		used[best] = true
		result = append(result, unique[best])
	}
	for i := range result {
		result[i].FinalRank = i + 1
	}
	return result
}

func selectEvidenceDimensions(chunks []RetrievedChunk, limit int, required []int64, dimensions []string) []RetrievedChunk {
	if len(required) == 0 || len(dimensions) == 0 {
		return selectDiverseEvidence(chunks, limit, required)
	}
	selected := selectDiverseEvidence(chunks, min(limit, len(required)), required)
	used := map[string]bool{}
	for _, c := range selected {
		used[retrievalChunkKey(c)] = true
	}
	for _, dimension := range dimensions {
		for _, id := range required {
			for _, c := range chunks {
				if len(selected) >= limit {
					break
				}
				if c.TaskID != id || used[retrievalChunkKey(c)] || !strings.Contains(strings.ToLower(c.Content), strings.ToLower(dimension)) {
					continue
				}
				selected = append(selected, c)
				used[retrievalChunkKey(c)] = true
				break
			}
		}
	}
	remaining := make([]RetrievedChunk, 0)
	for _, c := range chunks {
		if !used[retrievalChunkKey(c)] {
			remaining = append(remaining, c)
		}
	}
	selected = append(selected, selectDiverseEvidence(remaining, limit-len(selected), nil)...)
	for i := range selected {
		selected[i].FinalRank = i + 1
	}
	return selected
}

// No global cache: authorization, model and scope are frozen for this request only.
type retrievalRequestCache struct {
	mu         sync.Mutex
	corpora    map[string]*repository.BM25Corpus
	embeddings map[string][]float32
}

func newRetrievalRequestCache() *retrievalRequestCache {
	return &retrievalRequestCache{corpora: map[string]*repository.BM25Corpus{}, embeddings: map[string][]float32{}}
}
func (p *RetrievalPipeline) loadRequestCorpus(ctx context.Context, userID int64, ids []int64, model string) (*repository.BM25Corpus, error) {
	if p.RequestCache == nil {
		return p.repos.VideoChunk.LoadBM25Corpus(ctx, userID, ids, model)
	}
	key := fmt.Sprintf("%d:%v:%s", userID, normalizeTaskIDs(ids), model)
	p.RequestCache.mu.Lock()
	defer p.RequestCache.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if corpus, ok := p.RequestCache.corpora[key]; ok {
		return corpus, nil
	}
	corpus, err := p.repos.VideoChunk.LoadBM25Corpus(ctx, userID, ids, model)
	if err == nil {
		p.RequestCache.corpora[key] = corpus
	}
	return corpus, err
}
func (p *RetrievalPipeline) embedRequestQuery(ctx context.Context, req RetrievalPipelineRequest, query string) ([]float32, error) {
	if p.RequestCache == nil {
		return embedQueryWithAdmissionWait(ctx, req.Embedding, query)
	}
	key := fmt.Sprintf("%d:%s:%s", req.UserID, req.EmbeddingModel, query)
	p.RequestCache.mu.Lock()
	defer p.RequestCache.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if vector, ok := p.RequestCache.embeddings[key]; ok {
		return append([]float32(nil), vector...), nil
	}
	vector, err := embedQueryWithAdmissionWait(ctx, req.Embedding, query)
	if err == nil {
		p.RequestCache.embeddings[key] = append([]float32(nil), vector...)
	}
	return vector, err
}
