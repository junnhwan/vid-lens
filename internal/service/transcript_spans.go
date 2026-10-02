package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

// transcriptObservations maps only exact retained source text to provider
// timestamps. Gaps, edited text and legacy rows retain the measured ASR window;
// no character-position estimate is used to manufacture a sentence time.
func transcriptObservations(row model.VideoTranscriptionChunk, retained string) ([]SourceTextObservation, bool) {
	coarse := func(content, spanID string) SourceTextObservation {
		ref := transcriptSourceRef(row)
		if spanID != "" {
			ref.StableID += ":untimed:" + spanID
			ref.ContentHash = artifact.Hash(content)
		}
		ref.Content = content
		return SourceTextObservation{Content: content, Modality: model.ChunkModalityTranscript, Refs: []ChunkSourceRef{ref}}
	}
	fallback := []SourceTextObservation{coarse(retained, "")}
	var segments []model.TranscriptionSegment
	if json.Unmarshal([]byte(row.TimedSegments), &segments) != nil || len(segments) == 0 {
		return fallback, false
	}
	raw := strings.TrimSpace(row.Content)
	text := strings.TrimLeftFunc(retained, unicode.IsSpace)
	// Stitch only drops an observation's prefix, inserting at most a separator.
	if text == "" || !strings.HasSuffix(raw, text) {
		return fallback, false
	}
	retainedStart := len(raw) - len(text)
	separator := retained[:len(retained)-len(text)]
	type span struct {
		start, end int
		index      int
		segment    model.TranscriptionSegment
	}
	spans := make([]span, 0, len(segments))
	cursor := 0
	window := transcriptSourceRef(row)
	if window.TimeRangeStatus == model.ChunkTimeRangeUnknown {
		return fallback, false
	}
	lastStart := int64(-1)
	for index, segment := range segments {
		quote := strings.TrimSpace(segment.Text)
		if quote == "" {
			continue
		}
		// Match verbatim and in source order; fuzzy matching could attach a
		// quote to a different spoken occurrence.
		position := strings.Index(raw[cursor:], quote)
		if position < 0 {
			continue
		}
		start := cursor + position
		end := start + len(quote)
		cursor = end
		// Some providers omit invalid segments. With repeated text we cannot
		// prove which occurrence the remaining timestamp belongs to.
		if strings.Count(raw, quote) != 1 {
			continue
		}
		// Even an invalid timed span consumes its matched text, so a later
		// repeated sentence cannot inherit that earlier occurrence by accident.
		if segment.StartMS < window.StartMS || segment.EndMS > window.EndMS || segment.EndMS <= segment.StartMS || segment.StartMS < lastStart {
			continue
		}
		lastStart = segment.StartMS
		spans = append(spans, span{start: start, end: end, index: index, segment: segment})
	}
	if len(spans) == 0 {
		return fallback, false
	}
	observations := make([]SourceTextObservation, 0, len(spans)*2+1)
	cursor = 0
	for _, item := range spans {
		if item.start > cursor {
			observations = append(observations, coarse(raw[cursor:item.start], fmt.Sprintf("%d:%d", cursor, item.start)))
		}
		content := raw[item.start:item.end]
		ref := transcriptSourceRef(row)
		ref.StableID = fmt.Sprintf("%s:asr:%d", ref.StableID, item.index)
		ref.Content, ref.ContentHash = content, artifact.Hash(content)
		ref.StartMS, ref.EndMS, ref.TimeRangeStatus = item.segment.StartMS, item.segment.EndMS, model.ChunkTimeRangeExact
		observations = append(observations, SourceTextObservation{Content: content, Modality: model.ChunkModalityTranscript, Refs: []ChunkSourceRef{ref}})
		cursor = item.end
	}
	if cursor < len(raw) {
		observations = append(observations, coarse(raw[cursor:], fmt.Sprintf("%d:%d", cursor, len(raw))))
	}
	// Source identities and hashes describe the full canonical observation.
	// Only Content is sliced by stitching, so answer imports can still locate
	// that observation in an artifact frozen from the same transcript.
	visible := make([]SourceTextObservation, 0, len(observations))
	cursor = 0
	timed := false
	for _, observation := range observations {
		end := cursor + len(observation.Content)
		if end > retainedStart {
			observation.Content = observation.Content[max(0, retainedStart-cursor):]
			observation.Refs[0].Content = observation.Content
			visible = append(visible, observation)
			timed = timed || observation.Refs[0].TimeRangeStatus == model.ChunkTimeRangeExact
		}
		cursor = end
	}
	observations = visible
	if separator != "" {
		observations[0].Content = separator + observations[0].Content
		observations[0].Refs[0].Content = observations[0].Content
	}
	return observations, timed
}
