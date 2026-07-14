package eval

import (
	"context"
	"testing"
)

type fakeChunkRetriever struct {
	results []ChunkEvidence
	err     error
}

func (f fakeChunkRetriever) Retrieve(_ context.Context, _ Case) ([]ChunkEvidence, error) {
	return f.results, f.err
}

func TestChunkEvidenceExecutorMapsStableIdentityWithoutFabricatingTime(t *testing.T) {
	executor := ChunkEvidenceExecutor{Retriever: fakeChunkRetriever{results: []ChunkEvidence{{
		ContextID: "task_1_hash_3", Text: "证据", Source: EvidenceSourceASR, TokenCount: 2,
	}}}}
	result, err := executor.Execute(t.Context(), Case{CaseID: "c1", VideoID: "v1", TaskID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Retrieved) != 1 || result.Retrieved[0].ContextID != "task_1_hash_3" {
		t.Fatalf("result = %+v", result)
	}
	if result.Retrieved[0].StartMS != 0 || result.Retrieved[0].EndMS != 0 {
		t.Fatalf("executor fabricated timestamps: %+v", result.Retrieved[0])
	}
	if !result.PredictedAnswerable {
		t.Fatal("non-empty evidence should be predicted answerable")
	}
}

func TestChunkEvidenceExecutorRejectsMissingStableIdentity(t *testing.T) {
	executor := ChunkEvidenceExecutor{Retriever: fakeChunkRetriever{results: []ChunkEvidence{{Text: "no identity", Source: EvidenceSourceASR}}}}
	_, err := executor.Execute(t.Context(), Case{CaseID: "c1", VideoID: "v1", TaskID: 1})
	if err == nil {
		t.Fatal("missing ContextID must be rejected")
	}
}
