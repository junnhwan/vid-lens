package eval

import (
	"context"
	"fmt"
	"strings"
)

// ChunkEvidence is the strict evaluator's backend-neutral view of one real
// retrieved chunk. ContextID must be the stable identity persisted with the
// chunk (VidLens uses video_chunks.vector_id), not a database row ID or a
// synthetic timestamp.
type ChunkEvidence struct {
	ContextID  string
	VideoID    string
	Text       string
	Source     EvidenceSource
	TokenCount int
}

type ChunkEvidenceRetriever interface {
	Retrieve(context.Context, Case) ([]ChunkEvidence, error)
}

type ChunkEvidenceExecutor struct {
	Retriever ChunkEvidenceRetriever
}

func (e ChunkEvidenceExecutor) Execute(ctx context.Context, c Case) (EvaluationCaseResult, error) {
	if e.Retriever == nil {
		return EvaluationCaseResult{}, &ExecutionError{Stage: "retrieval", Code: "retriever_missing", Err: fmt.Errorf("chunk evidence retriever is required")}
	}
	chunks, err := e.Retriever.Retrieve(ctx, c)
	if err != nil {
		return EvaluationCaseResult{}, &ExecutionError{Stage: "retrieval", Code: "retrieval_failed", Err: err}
	}
	contexts := make([]RetrievedContext, 0, len(chunks))
	for i, chunk := range chunks {
		contextID := strings.TrimSpace(chunk.ContextID)
		if contextID == "" {
			return EvaluationCaseResult{}, &ExecutionError{Stage: "evidence_mapping", Code: "stable_identity_missing", Err: fmt.Errorf("retrieved chunk %d has no stable context identity", i)}
		}
		videoID := strings.TrimSpace(chunk.VideoID)
		if videoID == "" {
			videoID = c.VideoID
		}
		source := chunk.Source
		if source == "" {
			source = EvidenceSourceASR
		}
		contexts = append(contexts, RetrievedContext{
			ContextID: contextID, VideoID: videoID, Source: source,
			Text: chunk.Text, TokenCount: chunk.TokenCount,
		})
	}
	return EvaluationCaseResult{Retrieved: contexts, PredictedAnswerable: len(contexts) > 0}, nil
}
