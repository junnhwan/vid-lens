package service

import (
	"sort"
	"strings"

	"vid-lens/internal/model"
)

const (
	ModalityIntentText     = "text"
	ModalityIntentVisual   = "visual"
	ModalityIntentMixed    = "mixed"
	ModalityIntentConflict = "conflict"
)

var visualQuestionSignals = []string{
	"画面", "屏幕", "字幕", "图表", "图中", "幻灯片", "ppt", "演示", "展示", "写着", "颜色", "外观", "布局", "镜头", "看起来", "出现",
}

var conflictQuestionSignals = []string{
	"冲突", "矛盾", "不一致", "是否一致", "有没有说错", "画面与解说", "画面和解说", "字幕与解说", "字幕和解说",
}

func classifyModalityIntent(question string) string {
	question = strings.ToLower(strings.TrimSpace(question))
	for _, signal := range conflictQuestionSignals {
		if strings.Contains(question, signal) {
			return ModalityIntentConflict
		}
	}
	for _, signal := range visualQuestionSignals {
		if strings.Contains(question, signal) {
			return ModalityIntentVisual
		}
	}
	if strings.Contains(question, "解说") || strings.Contains(question, "讲了") || strings.Contains(question, "说了") {
		return ModalityIntentText
	}
	return ModalityIntentMixed
}

func filterChunksByModalities(chunks []RetrievedChunk, allowed []string) []RetrievedChunk {
	if len(allowed) == 0 {
		return chunks
	}
	set := make(map[string]struct{}, len(allowed))
	for _, modality := range allowed {
		set[normalizedChunkModality(strings.TrimSpace(modality))] = struct{}{}
	}
	filtered := make([]RetrievedChunk, 0, len(chunks))
	for _, chunk := range chunks {
		if _, ok := set[normalizedChunkModality(chunk.Modality)]; ok {
			filtered = append(filtered, chunk)
		}
	}
	return filtered
}

func rankRetrievedModalities(question string, chunks []RetrievedChunk, topK int) []RetrievedChunk {
	if len(chunks) == 0 {
		return chunks
	}
	intent := classifyModalityIntent(question)
	ranks := make(map[string]int)
	for i := range chunks {
		modality := normalizedChunkModality(chunks[i].Modality)
		ranks[modality]++
		chunks[i].ModalityRank = ranks[modality]
		chunks[i].ModalityIntent = intent
		base := chunks[i].RerankScore
		if base == 0 {
			base = chunks[i].RRFScore
		}
		if base == 0 {
			base = float64(chunks[i].Score)
		}
		chunks[i].ModalityScore = base * modalityWeight(intent, modality)
	}
	sort.SliceStable(chunks, func(i, j int) bool {
		if chunks[i].ModalityScore != chunks[j].ModalityScore {
			return chunks[i].ModalityScore > chunks[j].ModalityScore
		}
		return chunks[i].FinalRank < chunks[j].FinalRank
	})

	limit := topK
	if limit <= 0 || limit > len(chunks) {
		limit = len(chunks)
	}
	selected := append([]RetrievedChunk(nil), chunks[:limit]...)
	if intent == ModalityIntentConflict && limit >= 2 {
		selected = ensureModalityDiversity(selected, chunks, limit)
	}
	for i := range selected {
		selected[i].FinalRank = i + 1
	}
	return selected
}

func modalityWeight(intent, modality string) float64 {
	visual := modality == model.ChunkModalityVisualOCR || modality == model.ChunkModalityVisualCaption
	switch intent {
	case ModalityIntentVisual:
		if visual {
			return 1.35
		}
		return 0.85
	case ModalityIntentText:
		if modality == model.ChunkModalityTranscript {
			return 1.15
		}
		return 0.90
	case ModalityIntentConflict:
		if visual || modality == model.ChunkModalityTranscript {
			return 1.15
		}
	}
	return 1
}

func ensureModalityDiversity(selected, candidates []RetrievedChunk, limit int) []RetrievedChunk {
	hasText, hasVisual := false, false
	for _, chunk := range selected {
		hasText = hasText || chunk.Modality == model.ChunkModalityTranscript
		hasVisual = hasVisual || chunk.Modality == model.ChunkModalityVisualOCR || chunk.Modality == model.ChunkModalityVisualCaption
	}
	find := func(wantVisual bool) (RetrievedChunk, bool) {
		for _, chunk := range candidates {
			visual := chunk.Modality == model.ChunkModalityVisualOCR || chunk.Modality == model.ChunkModalityVisualCaption
			if visual == wantVisual && (visual || chunk.Modality == model.ChunkModalityTranscript) {
				return chunk, true
			}
		}
		return RetrievedChunk{}, false
	}
	if !hasText {
		if chunk, ok := find(false); ok {
			selected[limit-1] = chunk
			hasText = true
		}
	}
	if !hasVisual {
		if chunk, ok := find(true); ok {
			selected[limit-1] = chunk
		}
	}
	return selected
}
