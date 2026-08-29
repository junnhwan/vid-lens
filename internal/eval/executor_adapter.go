package eval

import (
	"context"
	"fmt"
	"strings"
	"time"
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
	start := time.Now()
	chunks, err := e.Retriever.Retrieve(ctx, c)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		// Failed retrievals record zero latency: the executor never completed, and
		// the failure-as-zero convention keeps the P95 denominator honest.
		return EvaluationCaseResult{RetrieveLatencyMS: 0}, &ExecutionError{Stage: "retrieval", Code: "retrieval_failed", Err: err}
	}
	contexts := make([]RetrievedContext, 0, len(chunks))
	for i, chunk := range chunks {
		contextID := strings.TrimSpace(chunk.ContextID)
		if contextID == "" {
			// This is a failed case (returns ExecutionError); per the failure-as-zero
			// convention (docs/eval/README.md 的失败样本计时约定), failed cases record 0 latency so the
			// success-subset P95 cannot be polluted by a failed case's retrieval time.
			return EvaluationCaseResult{RetrieveLatencyMS: 0}, &ExecutionError{Stage: "evidence_mapping", Code: "stable_identity_missing", Err: fmt.Errorf("retrieved chunk %d has no stable context identity", i)}
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
	return EvaluationCaseResult{Retrieved: contexts, PredictedAnswerable: len(contexts) > 0, RetrieveLatencyMS: latency}, nil
}
