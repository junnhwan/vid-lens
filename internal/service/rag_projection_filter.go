package service

import (
	"context"
	"vid-lens/internal/model"
)

// A source can refresh after vector/keyword recall took its snapshot. Native
// stores filter before TopK; this second check drops retired relational IDs
// before they can enter generation context. Legacy unmapped fixtures/readers
// retain their existing compatibility behavior, while explicit source workflows
// must always resolve a current relational chunk identity.
func (p *RetrievalPipeline) filterCurrentSourceProjection(ctx context.Context, userID int64, taskIDs []int64, embeddingModel string, chunks []RetrievedChunk) ([]RetrievedChunk, error) {
	if len(chunks) == 0 || p == nil || p.repos == nil || p.repos.Task == nil || p.repos.VideoChunk == nil {
		return chunks, nil
	}
	tasks, err := p.repos.Task.ListByIDsForUser(userID, taskIDs)
	if err != nil {
		return nil, err
	}
	strict := map[int64]bool{}
	for _, task := range tasks {
		if task.ActiveTextSourceID != "" || task.ProcessingIntentJSON != "" {
			strict[task.ID] = true
		}
	}
	if len(strict) == 0 {
		return chunks, nil
	}
	ids := make([]int64, 0, len(chunks))
	for _, chunk := range chunks {
		if strict[chunk.TaskID] && chunk.ChunkID > 0 {
			ids = append(ids, chunk.ChunkID)
		}
	}
	stored, err := p.repos.VideoChunk.ListByIDs(userID, taskIDs, embeddingModel, ids)
	if err != nil {
		return nil, err
	}
	current := map[int64]model.VideoChunk{}
	for _, chunk := range stored {
		current[chunk.ID] = chunk
	}
	filtered := make([]RetrievedChunk, 0, len(chunks))
	for _, chunk := range chunks {
		if strict[chunk.TaskID] {
			row, ok := current[chunk.ChunkID]
			if !ok || row.TaskID != chunk.TaskID || chunk.EvidenceID != "" && row.VectorID != chunk.EvidenceID {
				continue
			}
		}
		filtered = append(filtered, chunk)
	}
	return filtered, nil
}
