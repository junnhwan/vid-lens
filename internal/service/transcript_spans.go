package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/transcript"
)

// transcriptObservations maps only exact retained source text to provider
// timestamps. Gaps, edited text and legacy rows retain the measured ASR window;
// no character-position estimate is used to manufacture a sentence time.
func transcriptObservations(row model.VideoTranscriptionChunk, retained string, runeRange ...int) ([]SourceTextObservation, bool) {
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
	if len(runeRange) == 2 && (runeRange[0] != 0 || runeRange[1] != len([]rune(strings.TrimSpace(row.Content)))) {
		fallback[0].Refs[0].StableID += fmt.Sprintf(":retained:%d:%d", runeRange[0], runeRange[1])
		fallback[0].Refs[0].ContentHash = artifact.Hash(retained)
	}
	var segments []model.TranscriptionSegment
	if json.Unmarshal([]byte(row.TimedSegments), &segments) != nil || len(segments) == 0 {
		return fallback, false
	}
	raw := strings.TrimSpace(row.Content)
	text := strings.TrimLeftFunc(retained, unicode.IsSpace)
	if text == "" {
		return fallback, false
	}
	retainedStart := len(raw) - len(text)
	retainedEnd := len(raw)
	if len(runeRange) == 2 {
		runes := []rune(raw)
		start, end := runeRange[0], runeRange[1]
		if start < 0 || end <= start || end > len(runes) || strings.TrimSpace(string(runes[start:end])) != strings.TrimSpace(text) {
			return fallback, false
		}
		retainedStart, retainedEnd = len(string(runes[:start])), len(string(runes[:end]))
	} else if !strings.HasSuffix(raw, text) {
		return fallback, false
	}
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
		start, end := 0, 0
		positioned := segment.TextEnd > segment.TextStart
		if positioned {
			runes := []rune(raw)
			if segment.TextStart < 0 || segment.TextEnd > len(runes) || string(runes[segment.TextStart:segment.TextEnd]) != segment.Text {
				continue
			}
			start, end = len(string(runes[:segment.TextStart])), len(string(runes[:segment.TextEnd]))
			if start < cursor {
				continue
			}
			if segment.Method == "forced_alignment" {
				// Punctuation has no acoustic interval of its own. Keep it with
				// its observed word so it does not create coarse punctuation rows.
				if strings.IndexFunc(raw[cursor:start], func(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) }) < 0 {
					start = cursor
				}
				next := len(raw)
				if index+1 < len(segments) && segments[index+1].TextStart >= segment.TextEnd && segments[index+1].TextStart <= len(runes) {
					next = len(string(runes[:segments[index+1].TextStart]))
				}
				if next >= end && strings.IndexFunc(raw[end:next], func(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) }) < 0 {
					end = next
				}
			}
		} else {
			position := strings.Index(raw[cursor:], quote)
			if position < 0 {
				continue
			}
			start = cursor + position
			end = start + len(quote)
		}
		cursor = end
		// Some providers omit invalid segments. With repeated text we cannot
		// prove which occurrence the remaining timestamp belongs to.
		if !positioned && strings.Count(raw, quote) != 1 {
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
		if end > retainedStart && cursor < retainedEnd {
			observation.Content = observation.Content[max(0, retainedStart-cursor):min(len(observation.Content), retainedEnd-cursor)]
			observation.Refs[0].Content = observation.Content
			visible = append(visible, observation)
			timed = timed || observation.Refs[0].TimeRangeStatus == model.ChunkTimeRangeExact
		}
		cursor = end
	}
	observations = visible
	if transcript.ValidateAlignedWords(row, segments) == nil {
		observations = alignedSentenceObservations(observations)
	}
	if separator != "" {
		observations[0].Content = separator + observations[0].Content
		observations[0].Refs[0].Content = observations[0].Content
	}
	return observations, timed
}

// Words are acoustic coordinates, while sentences are useful canonical
// evidence. Keeping every character as an artifact source would fragment the
// model's reading context and hit source-count limits on ordinary long videos.
// Group only verified words within one raw observation, preserving every
// character and the union of their actual intervals. Cross-window sentence
// pieces remain separate provenance, and the reader joins them for display.
func alignedSentenceObservations(words []SourceTextObservation) []SourceTextObservation {
	var out []SourceTextObservation
	lastWordID := ""
	flush := func() {
		if len(out) == 0 {
			return
		}
		last := &out[len(out)-1]
		ref := &last.Refs[0]
		ref.StableID += ":to:" + lastWordID[strings.LastIndex(lastWordID, ":")+1:]
		ref.Content, ref.ContentHash = last.Content, artifact.Hash(last.Content)
	}
	for _, word := range words {
		ref := word.Refs[0]
		join := false
		if len(out) > 0 {
			last := &out[len(out)-1]
			previous := last.Refs[0]
			ending := strings.TrimRight(last.Content, " \t\r\n\"'”’）)")
			endingRunes := []rune(ending)
			ended := len(endingRunes) > 0 && strings.ContainsRune("。！？!?；;.", endingRunes[len(endingRunes)-1])
			join = ref.TimeRangeStatus == model.ChunkTimeRangeExact && previous.TimeRangeStatus == model.ChunkTimeRangeExact &&
				ref.StartMS >= previous.StartMS && ref.StartMS-previous.EndMS <= 2000 && !ended
			if join {
				last.Content += word.Content
				last.Refs[0].EndMS = max(previous.EndMS, ref.EndMS)
			}
		}
		if !join {
			flush()
			copyWord := word
			copyWord.Refs = append([]ChunkSourceRef(nil), word.Refs...)
			out = append(out, copyWord)
		}
		lastWordID = ref.StableID
	}
	flush()
	return out
}
