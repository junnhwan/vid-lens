package service

import (
	"testing"

	"vid-lens/internal/model"
)

func TestRankRetrievedModalitiesPrioritizesVisualQuestions(t *testing.T) {
	chunks := []RetrievedChunk{
		{EvidenceID: "speech", Modality: model.ChunkModalityTranscript, RRFScore: 0.020},
		{EvidenceID: "chart", Modality: model.ChunkModalityVisualCaption, RRFScore: 0.018},
	}
	got := rankRetrievedModalities("图表里展示的收入是多少？", chunks, 2)
	if len(got) != 2 || got[0].EvidenceID != "chart" || got[0].ModalityIntent != ModalityIntentVisual || got[0].ModalityScore <= got[1].ModalityScore {
		t.Fatalf("visual ranking = %+v", got)
	}
}

func TestRankRetrievedModalitiesKeepsBothSidesOfConflict(t *testing.T) {
	chunks := []RetrievedChunk{
		{EvidenceID: "ocr-1", Modality: model.ChunkModalityVisualOCR, RRFScore: 0.030},
		{EvidenceID: "caption-1", Modality: model.ChunkModalityVisualCaption, RRFScore: 0.025},
		{EvidenceID: "speech-1", Modality: model.ChunkModalityTranscript, RRFScore: 0.010},
	}
	got := rankRetrievedModalities("画面和解说是否一致？", chunks, 2)
	if len(got) != 2 {
		t.Fatalf("conflict ranking = %+v", got)
	}
	hasText, hasVisual := false, false
	for _, chunk := range got {
		hasText = hasText || chunk.Modality == model.ChunkModalityTranscript
		hasVisual = hasVisual || chunk.Modality == model.ChunkModalityVisualOCR || chunk.Modality == model.ChunkModalityVisualCaption
		if chunk.ModalityIntent != ModalityIntentConflict {
			t.Fatalf("intent = %q", chunk.ModalityIntent)
		}
	}
	if !hasText || !hasVisual {
		t.Fatalf("conflict lost a modality: %+v", got)
	}
}

func TestFilterChunksByModalitiesRejectsOtherEvidence(t *testing.T) {
	chunks := []RetrievedChunk{{EvidenceID: "t", Modality: model.ChunkModalityTranscript}, {EvidenceID: "v", Modality: model.ChunkModalityVisualOCR}}
	got := filterChunksByModalities(chunks, []string{model.ChunkModalityVisualOCR, model.ChunkModalityVisualCaption})
	if len(got) != 1 || got[0].EvidenceID != "v" {
		t.Fatalf("filtered = %+v", got)
	}
}
