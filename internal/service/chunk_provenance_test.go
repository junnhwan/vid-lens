package service

import (
	"testing"

	"vid-lens/internal/model"
)

func TestSplitObservationsPreservesSentenceSourcesAndTime(t *testing.T) {
	chunks := SplitObservationsIntoChunks([]SourceTextObservation{
		{Content: "第一句完整。", Modality: model.ChunkModalityTranscript, Refs: []ChunkSourceRef{{SourceType: model.ChunkModalityTranscript, StableID: "segment-a", StartMS: 0, EndMS: 10000, TimeRangeStatus: model.ChunkTimeRangeCoarse}}},
		{Content: "第二句完整。", Modality: model.ChunkModalityTranscript, Refs: []ChunkSourceRef{{SourceType: model.ChunkModalityTranscript, StableID: "segment-b", StartMS: 9000, EndMS: 20000, TimeRangeStatus: model.ChunkTimeRangeCoarse}}},
	}, 21, 0)
	if len(chunks) != 2 || chunks[0].Content != "第一句完整。" || chunks[1].Content != "第二句完整。" {
		t.Fatalf("chunks = %+v", chunks)
	}
	for i, chunk := range chunks {
		if chunk.Modality != model.ChunkModalityTranscript || chunk.SourceMappingStatus != model.ChunkSourceMapped || chunk.TimeRangeStatus != model.ChunkTimeRangeCoarse || len(chunk.SourceRefs) != 1 || chunk.TokenCount > 21 {
			t.Fatalf("chunk[%d] provenance = %+v", i, chunk)
		}
	}
}

func TestSplitObservationsMarksHistoricalContentUnmapped(t *testing.T) {
	chunks := SplitObservationsIntoChunks([]SourceTextObservation{{Content: "历史转写没有可验证来源。", Modality: model.ChunkModalityTranscript}}, 100, 0)
	if len(chunks) != 1 || chunks[0].SourceMappingStatus != model.ChunkSourceUnmapped || chunks[0].TimeRangeStatus != model.ChunkTimeRangeUnknown || chunks[0].StartMS != 0 || len(chunks[0].SourceRefs) != 0 {
		t.Fatalf("historical chunk = %+v", chunks)
	}
}

func TestSplitObservationsDoesNotExposePartialMappingAsKnownTime(t *testing.T) {
	chunks := SplitObservationsIntoChunks([]SourceTextObservation{
		{Content: "可映射。", Modality: model.ChunkModalityTranscript, Refs: []ChunkSourceRef{{SourceType: model.ChunkModalityTranscript, StableID: "segment-a", StartMS: 1000, EndMS: 2000, TimeRangeStatus: model.ChunkTimeRangeCoarse}}},
		{Content: "历史未映射。", Modality: model.ChunkModalityTranscript},
	}, 100, 0)
	if len(chunks) != 1 || chunks[0].SourceMappingStatus != model.ChunkSourcePartial || chunks[0].TimeRangeStatus != model.ChunkTimeRangeUnknown || chunks[0].StartMS != 0 || chunks[0].EndMS != 0 {
		t.Fatalf("partial chunk = %+v", chunks)
	}
}

func TestSplitObservationsKeepsEveryChunkWithinConservativeTokenBudget(t *testing.T) {
	chunks := SplitObservationsIntoChunks([]SourceTextObservation{{
		Content:  "超长句子没有边界",
		Modality: model.ChunkModalityTranscript,
		Refs:     []ChunkSourceRef{{SourceType: model.ChunkModalityTranscript, StableID: "segment-long", StartMS: 0, EndMS: 1000, TimeRangeStatus: model.ChunkTimeRangeCoarse}},
	}}, 6, 0)
	if len(chunks) < 2 {
		t.Fatalf("chunks = %+v, want hard split for oversized semantic unit", chunks)
	}
	for i, chunk := range chunks {
		if chunk.TokenCount > 6 || EstimateChunkTokens(chunk.Content) > 6 {
			t.Fatalf("chunk[%d] exceeds token budget: %+v", i, chunk)
		}
	}
}

func TestFormatOCRChunksCarriesFrameObservation(t *testing.T) {
	chunks := FormatOCRChunksForIndex([]model.VideoVisualFrame{{ID: 9, TimeMs: 12345, OCRText: "屏幕上的标题", ObjectKey: "frames/9.jpg", CaptionMethod: "ocr", Status: model.VisualFrameStatusCompleted}})
	if len(chunks) != 1 || chunks[0].Modality != model.ChunkModalityVisualOCR || chunks[0].StartMS != 12345 || chunks[0].EndMS != 12346 || chunks[0].TimeRangeStatus != model.ChunkTimeRangeExact || chunks[0].SourceMappingStatus != model.ChunkSourceMapped {
		t.Fatalf("visual chunk = %+v", chunks)
	}
	if len(chunks[0].SourceRefs) != 1 || chunks[0].SourceRefs[0].StableID != "visual-frame:9" || chunks[0].SourceRefs[0].ObjectKey != "frames/9.jpg" {
		t.Fatalf("visual refs = %+v", chunks[0].SourceRefs)
	}
}

func TestFilterChunksByTimeRangesDropsUnknownAndNonOverlapping(t *testing.T) {
	chunks := []RetrievedChunk{{EvidenceID: "hit", StartMS: 10000, EndMS: 20000, TimeRangeStatus: model.ChunkTimeRangeCoarse}, {EvidenceID: "miss", StartMS: 30000, EndMS: 40000, TimeRangeStatus: model.ChunkTimeRangeCoarse}, {EvidenceID: "unknown", TimeRangeStatus: model.ChunkTimeRangeUnknown}}
	got := filterChunksByTimeRanges(chunks, []TimestampRange{{StartMS: 15000, EndMS: 15000}})
	if len(got) != 1 || got[0].EvidenceID != "hit" {
		t.Fatalf("filtered = %+v", got)
	}
}

func TestRetrievalHydratesCitationProvenanceFromRelationalChunk(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	refsJSON, _ := MarshalChunkSourceRefs([]ChunkSourceRef{{SourceType: model.ChunkModalityTranscript, StableID: "segment-a", StartMS: 1000, EndMS: 2000, TimeRangeStatus: model.ChunkTimeRangeCoarse}})
	if err := repos.VideoChunk.ReplaceTaskChunks(1, "embed", []model.VideoChunk{{UserID: 7, TaskID: 1, ChunkIndex: 3, Content: "可引用的 owner 证据。", ContentHash: "hash", EmbeddingModel: "embed", EmbeddingDim: 3, VectorID: "ev-1", Modality: model.ChunkModalityTranscript, StartMS: 1000, EndMS: 2000, TimeRangeStatus: model.ChunkTimeRangeCoarse, SourceMappingStatus: model.ChunkSourceMapped, SourceRefs: refsJSON}}); err != nil {
		t.Fatal(err)
	}
	stored, _ := repos.VideoChunk.ListByTaskID(7, 1, "embed")
	chunks := []RetrievedChunk{{TaskID: 1, EvidenceID: "ev-1", ChunkID: stored[0].ID, ChunkIndex: 3, Content: stored[0].Content}}
	pipeline := &RetrievalPipeline{repos: repos}
	if err := pipeline.hydrateChunkProvenance(7, []int64{1}, "embed", chunks); err != nil {
		t.Fatal(err)
	}
	_, citations := buildCitationSet("owner", chunks)
	if len(citations) != 1 || citations[0].Modality != model.ChunkModalityTranscript || citations[0].StartMS != 1000 || citations[0].EndMS != 2000 || citations[0].SourceMappingStatus != model.ChunkSourceMapped || len(citations[0].SourceRefs) != 1 {
		t.Fatalf("citations=%+v", citations)
	}
}

func TestKeywordRetrievalCarriesRelationalProvenance(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	refsJSON, _ := MarshalChunkSourceRefs([]ChunkSourceRef{{SourceType: model.ChunkModalityVisualOCR, StableID: "visual-frame:8", StartMS: 8000, EndMS: 8001, TimeRangeStatus: model.ChunkTimeRangeExact}})
	if err := repos.VideoChunk.ReplaceTaskChunks(1, "embed", []model.VideoChunk{{
		UserID: 7, TaskID: 1, ChunkIndex: 4, Content: "画面显示稳定的来源标识", ContentHash: "hash-keyword", EmbeddingModel: "embed", EmbeddingDim: 3, VectorID: "ev-keyword",
		Modality: model.ChunkModalityVisualOCR, StartMS: 8000, EndMS: 8001, TimeRangeStatus: model.ChunkTimeRangeExact, SourceMappingStatus: model.ChunkSourceMapped, SourceRefs: refsJSON,
	}}); err != nil {
		t.Fatal(err)
	}
	chunks, err := (&RetrievalPipeline{repos: repos}).keywordChunks(7, 1, "embed", "来源标识", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 || chunks[0].Source != RetrievalSourceKeyword || chunks[0].Modality != model.ChunkModalityVisualOCR || chunks[0].StartMS != 8000 || chunks[0].TimeRangeStatus != model.ChunkTimeRangeExact || len(chunks[0].SourceRefs) != 1 {
		t.Fatalf("keyword chunks = %+v", chunks)
	}
}
