package service

import "testing"

func TestFuseRetrievedChunksUsesRRFAndMarksSources(t *testing.T) {
	vectorChunks := []RetrievedChunk{
		{ChunkID: 1, ChunkIndex: 0, Score: 0.91, Content: "vector only"},
		{ChunkID: 2, ChunkIndex: 1, Score: 0.72, Content: "both"},
	}
	keywordChunks := []RetrievedChunk{
		{ChunkID: 2, ChunkIndex: 1, Score: 4.2, Content: "both"},
		{ChunkID: 3, ChunkIndex: 2, Score: 3.8, Content: "keyword only"},
	}

	fused := FuseRetrievedChunks(vectorChunks, keywordChunks, 3, 60)
	if len(fused) != 3 {
		t.Fatalf("len(fused) = %d, want 3: %+v", len(fused), fused)
	}
	if fused[0].ChunkID != 2 {
		t.Fatalf("first chunk id = %d, want hybrid chunk 2: %+v", fused[0].ChunkID, fused)
	}
	if fused[0].Source != RetrievalSourceHybrid {
		t.Fatalf("source = %q, want hybrid", fused[0].Source)
	}
	if fused[0].VectorRank != 2 || fused[0].KeywordRank != 1 {
		t.Fatalf("ranks = vector %d keyword %d, want 2/1", fused[0].VectorRank, fused[0].KeywordRank)
	}
	if fused[0].RRFScore <= fused[1].RRFScore {
		t.Fatalf("hybrid rrf score should outrank single-source chunk: %+v", fused)
	}
	if fused[1].Source == "" || fused[2].Source == "" {
		t.Fatalf("all fused chunks should carry retrieval source: %+v", fused)
	}
}

func TestExtractQueryTermsPreservesEnglishNumbersAndChineseNgrams(t *testing.T) {
	terms := ExtractQueryTerms("为什么分布式锁要校验 owner？2026 年")
	termSet := make(map[string]bool, len(terms))
	for _, term := range terms {
		termSet[term] = true
	}

	for _, want := range []string{"owner", "2026", "分布式锁", "校验"} {
		if !termSet[want] {
			t.Fatalf("terms = %#v, missing %q", terms, want)
		}
	}
}

func TestFuseRetrievedChunksDeduplicatesByStableEvidenceID(t *testing.T) {
	vectorChunks := []RetrievedChunk{{EvidenceID: "task_9_semantic-v1_deadbeef_3", ChunkID: 10, ChunkIndex: 3, Content: "vector copy"}}
	keywordChunks := []RetrievedChunk{{EvidenceID: "task_9_semantic-v1_deadbeef_3", ChunkID: 99, ChunkIndex: 3, Content: "keyword copy"}}

	fused := FuseRetrievedChunks(vectorChunks, keywordChunks, 5, 60)
	if len(fused) != 1 {
		t.Fatalf("len(fused) = %d, want stable evidence merged once: %+v", len(fused), fused)
	}
	if fused[0].Source != RetrievalSourceHybrid || fused[0].VectorRank != 1 || fused[0].KeywordRank != 1 {
		t.Fatalf("fused evidence = %+v, want both retrieval ranks", fused[0])
	}
}
