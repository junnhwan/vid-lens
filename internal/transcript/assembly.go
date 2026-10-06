package transcript

import (
	"encoding/json"
	"strings"
	"unicode"

	"vid-lens/internal/model"
)

// Assemble is the only window-to-transcript operation used by the consumer,
// reader and retrieval projection. Contributions always retain a contiguous
// range of an immutable observation; no generated wording enters the corpus.
func Assemble(rows []model.VideoTranscriptionChunk) StitchResult {
	var result StitchResult
	ranges := make([]retainedRange, len(rows))
	for i, row := range rows {
		text := strings.TrimSpace(row.Content)
		start, end, aligned := ownedRange(row, text)
		aligned = aligned && row.Status == model.TranscriptionChunkStatusCompleted
		if !aligned {
			start, end = 0, len([]rune(text))
		}
		ranges[i] = retainedRange{start: start, end: end, aligned: aligned}
		if i > 0 && aligned && ranges[i-1].aligned && AdjacentOverlap(rows[i-1], row) {
			if leftEnd, rightStart, ok := sharedAcousticBoundary(rows[i-1], row); ok && leftEnd >= ranges[i-1].start && rightStart <= end {
				ranges[i-1].end, ranges[i].start = leftEnd, rightStart
				ranges[i].anchored = true
			}
		}
	}
	previous := -1
	for index, row := range rows {
		if row.Status != model.TranscriptionChunkStatusCompleted || strings.TrimSpace(row.Content) == "" {
			previous = -1 // silence, missing work and failures break text matching
			continue
		}
		text := strings.TrimSpace(row.Content)
		startRune, endRune, aligned := ranges[index].start, ranges[index].end, ranges[index].aligned
		if aligned {
			text = string([]rune(text)[startRune:endRune])
		}
		if text == "" {
			previous = -1
			continue
		}
		before := result.Content
		method, matched, dropped := "append", 0, 0
		if before == "" {
			result.Content = text
		} else if previous >= 0 && AdjacentOverlap(rows[previous], row) {
			previousAligned := ranges[previous].aligned
			if aligned && previousAligned {
				// Timestamp ownership already removed shared audio; a second text
				// match could erase a legitimately repeated word at the boundary.
				result.Content = joinPreservingWords(before, text)
				method = "time_ownership"
				if ranges[index].anchored {
					method = "aligned_overlap_anchor"
				}
			} else {
				result.Content, matched, dropped = stitchPair(before, text)
				if matched > 0 {
					method = "exact_normalized_overlap"
				}
			}
		} else {
			result.Content = before + "\n\n" + text
		}
		if retained := strings.TrimPrefix(result.Content, before); retained != "" {
			result.Contributions = append(result.Contributions, Contribution{PartIndex: index, Content: retained, StartRune: startRune + dropped, EndRune: endRune})
		}
		if previous >= 0 {
			result.Boundaries = append(result.Boundaries, Boundary{LeftPart: previous, RightPart: index, Method: method, MatchRunes: matched, PrefixRunes: dropped})
		}
		previous = index
	}
	return result
}

func AdjacentOverlap(left, right model.VideoTranscriptionChunk) bool {
	return right.ChunkIndex == left.ChunkIndex+1 && left.SegmentKey != "" && right.SegmentKey != "" &&
		left.SegmenterVersion != "" && left.SegmenterVersion == right.SegmenterVersion &&
		left.WindowStartMS >= 0 && right.WindowStartMS > left.WindowStartMS &&
		left.WindowEndMS > right.WindowStartMS && right.WindowEndMS > left.WindowEndMS
}

// ownedText only uses complete, validated forced-alignment word coverage.
// Character offsets select wording; observed timestamps establish ownership.
func ownedText(row model.VideoTranscriptionChunk, text string) (string, bool) {
	start, end, ok := ownedRange(row, text)
	if !ok {
		return text, false
	}
	return string([]rune(text)[start:end]), true
}

func ownedRange(row model.VideoTranscriptionChunk, text string) (int, int, bool) {
	if row.CoreEndMS <= row.CoreStartMS || row.WindowEndMS <= row.WindowStartMS || row.CoreStartMS < row.WindowStartMS || row.CoreEndMS > row.WindowEndMS {
		return 0, 0, false
	}
	var words []model.TranscriptionSegment
	if json.Unmarshal([]byte(row.TimedSegments), &words) != nil || len(words) == 0 {
		return 0, 0, false
	}
	runes := []rune(text)
	cursor, first, last := 0, -1, -1
	lastTime := int64(-1)
	for i, word := range words {
		if word.Method != "forced_alignment" || word.TextStart < cursor || word.TextEnd <= word.TextStart || word.TextEnd > len(runes) ||
			string(runes[word.TextStart:word.TextEnd]) != word.Text || word.StartMS < row.WindowStartMS || word.EndMS > row.WindowEndMS ||
			word.EndMS <= word.StartMS || word.StartMS < lastTime {
			return 0, 0, false
		}
		for _, r := range runes[cursor:word.TextStart] {
			if unicode.IsLetter(r) || unicode.IsNumber(r) {
				return 0, 0, false
			}
		}
		cursor, lastTime = word.TextEnd, word.StartMS
		midpoint := word.StartMS + (word.EndMS-word.StartMS)/2
		if midpoint >= row.CoreStartMS && midpoint < row.CoreEndMS {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	for _, r := range runes[cursor:] {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return 0, 0, false
		}
	}
	if first < 0 {
		return 0, 0, true
	}
	start, end := words[first].TextStart, len(runes)
	if first == 0 {
		start = 0
	}
	if last+1 < len(words) {
		end = words[last+1].TextStart
	}
	return start, end, true
}
