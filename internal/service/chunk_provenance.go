package service

import (
	"encoding/json"
	"sort"
	"strings"

	"vid-lens/internal/model"
)

type ChunkSourceRef struct {
	SourceType      string `json:"source_type"`
	StableID        string `json:"stable_id"`
	ArtifactKind    string `json:"artifact_kind,omitempty"`
	SegmentKey      string `json:"segment_key,omitempty"`
	SourceRowID     int64  `json:"source_row_id,omitempty"`
	StartMS         int64  `json:"start_ms"`
	EndMS           int64  `json:"end_ms"`
	TimeRangeStatus string `json:"time_range_status"`
	ObjectKey       string `json:"object_key,omitempty"`
	CaptionMethod   string `json:"caption_method,omitempty"`
}

type SourceTextObservation struct {
	Content  string
	Modality string
	Refs     []ChunkSourceRef
}

func MarshalChunkSourceRefs(refs []ChunkSourceRef) (string, error) {
	if len(refs) == 0 {
		return "[]", nil
	}
	raw, err := json.Marshal(refs)
	return string(raw), err
}

func ParseChunkSourceRefs(raw string) ([]ChunkSourceRef, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var refs []ChunkSourceRef
	if err := json.Unmarshal([]byte(raw), &refs); err != nil {
		return nil, err
	}
	return refs, nil
}

type annotatedUnit struct {
	content  string
	refs     []ChunkSourceRef
	modality string
	mapped   bool
}

// SplitObservationsIntoChunks preserves source spans while using the same
// semantic boundary and whole-unit overlap policy as SplitTextIntoChunks.
func SplitObservationsIntoChunks(observations []SourceTextObservation, chunkSize, overlap int) []TextChunk {
	if chunkSize <= 0 {
		chunkSize = 800
	}
	if overlap < 0 || overlap >= chunkSize {
		overlap = 0
	}
	var text strings.Builder
	type observationSpan struct {
		start, end  int
		observation SourceTextObservation
	}
	spans := make([]observationSpan, 0, len(observations))
	cursor := 0
	for _, observation := range observations {
		if observation.Content == "" {
			continue
		}
		start := cursor
		text.WriteString(observation.Content)
		cursor += runeCount(observation.Content)
		spans = append(spans, observationSpan{start: start, end: cursor, observation: observation})
	}
	fullText := text.String()
	if strings.TrimSpace(fullText) == "" {
		return nil
	}
	units := splitSemanticUnits(fullText, chunkSize, 0)
	annotated := make([]annotatedUnit, 0, len(units))
	unitStart := 0
	for _, content := range units {
		unitEnd := unitStart + runeCount(content)
		refs := make([]ChunkSourceRef, 0)
		modalities := make(map[string]struct{})
		mapped := true
		covered := false
		for _, span := range spans {
			if span.end <= unitStart || span.start >= unitEnd {
				continue
			}
			covered = true
			if len(span.observation.Refs) == 0 {
				mapped = false
			}
			refs = append(refs, span.observation.Refs...)
			if modality := strings.TrimSpace(span.observation.Modality); modality != "" {
				modalities[modality] = struct{}{}
			}
		}
		if !covered {
			mapped = false
		}
		annotated = append(annotated, annotatedUnit{content: content, refs: dedupeSourceRefs(refs), modality: collapseModalities(modalities), mapped: mapped})
		unitStart = unitEnd
	}
	return packAnnotatedUnits(annotated, chunkSize, overlap)
}

func packAnnotatedUnits(units []annotatedUnit, chunkSize, overlap int) []TextChunk {
	chunks := make([]TextChunk, 0, len(units))
	for start := 0; start < len(units); {
		end, tokens := start, 0
		for end < len(units) {
			unitTokens := EstimateChunkTokens(units[end].content)
			if end > start && tokens+unitTokens > chunkSize {
				break
			}
			tokens += unitTokens
			end++
			if tokens >= chunkSize {
				break
			}
		}
		contentParts := make([]string, 0, end-start)
		refs := make([]ChunkSourceRef, 0)
		modalities := make(map[string]struct{})
		mappedUnits := 0
		for _, unit := range units[start:end] {
			contentParts = append(contentParts, unit.content)
			refs = append(refs, unit.refs...)
			if unit.modality != "" {
				modalities[unit.modality] = struct{}{}
			}
			if unit.mapped {
				mappedUnits++
			}
		}
		content := strings.TrimSpace(strings.Join(contentParts, ""))
		if content != "" {
			refs = dedupeSourceRefs(refs)
			mappingStatus := model.ChunkSourceUnmapped
			switch {
			case mappedUnits == end-start && len(refs) > 0:
				mappingStatus = model.ChunkSourceMapped
			case mappedUnits > 0 || len(refs) > 0:
				mappingStatus = model.ChunkSourcePartial
			}
			startMS, endMS, timeStatus := int64(0), int64(0), model.ChunkTimeRangeUnknown
			if mappingStatus == model.ChunkSourceMapped {
				startMS, endMS, timeStatus = aggregateSourceTime(refs)
			}
			chunks = append(chunks, TextChunk{
				Index: len(chunks), Content: content, TokenCount: EstimateChunkTokens(content),
				Modality: collapseModalities(modalities), StartMS: startMS, EndMS: endMS,
				TimeRangeStatus: timeStatus, SourceMappingStatus: mappingStatus, SourceRefs: refs,
			})
		}
		if end >= len(units) {
			break
		}
		rawUnits := make([]string, len(units))
		for i := range units {
			rawUnits[i] = units[i].content
		}
		next := semanticOverlapStartBy(rawUnits, start, end, chunkSize, overlap, EstimateChunkTokens)
		if next <= start || next > end {
			next = end
		}
		start = next
	}
	return chunks
}

func dedupeSourceRefs(refs []ChunkSourceRef) []ChunkSourceRef {
	seen := make(map[string]struct{}, len(refs))
	out := make([]ChunkSourceRef, 0, len(refs))
	for _, ref := range refs {
		key := ref.SourceType + "\x00" + ref.StableID
		if strings.TrimSpace(ref.StableID) == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].StartMS != out[j].StartMS {
			return out[i].StartMS < out[j].StartMS
		}
		if out[i].SourceType != out[j].SourceType {
			return out[i].SourceType < out[j].SourceType
		}
		return out[i].StableID < out[j].StableID
	})
	return out
}

func aggregateSourceTime(refs []ChunkSourceRef) (int64, int64, string) {
	if len(refs) == 0 {
		return 0, 0, model.ChunkTimeRangeUnknown
	}
	status := model.ChunkTimeRangeExact
	var start, end int64
	for i, ref := range refs {
		if ref.TimeRangeStatus == model.ChunkTimeRangeUnknown || ref.EndMS <= ref.StartMS || ref.StartMS < 0 {
			return 0, 0, model.ChunkTimeRangeUnknown
		}
		if i == 0 || ref.StartMS < start {
			start = ref.StartMS
		}
		if ref.EndMS > end {
			end = ref.EndMS
		}
		if ref.TimeRangeStatus != model.ChunkTimeRangeExact {
			status = model.ChunkTimeRangeCoarse
		}
	}
	return start, end, status
}

func collapseModalities(values map[string]struct{}) string {
	if len(values) != 1 {
		return model.ChunkModalityUnknown
	}
	for value := range values {
		return value
	}
	return model.ChunkModalityUnknown
}

func sourceRefsForModelChunk(chunk model.VideoChunk) []ChunkSourceRef {
	refs, err := ParseChunkSourceRefs(chunk.SourceRefs)
	if err != nil {
		return nil
	}
	return refs
}
