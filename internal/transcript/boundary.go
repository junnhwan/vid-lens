package transcript

import (
	"encoding/json"
	"strings"
	"unicode"

	"vid-lens/internal/model"
)

type retainedRange struct {
	start, end        int
	aligned, anchored bool
}

type acousticCharacter struct {
	rune           rune
	startMS, endMS int64
	cut            int // end of word plus its following punctuation
	wordEnd        bool
}

// sharedAcousticBoundary chooses one common cut for both observations. A
// fixed core cut alone can lose or duplicate a character when two timestamp
// predictions differ by one quantization step. Matching source characters
// AND acoustic occurrence removes that ambiguity without rewriting speech.
func sharedAcousticBoundary(left, right model.VideoTranscriptionChunk) (int, int, bool) {
	start, end := right.WindowStartMS, left.WindowEndMS
	characters := func(row model.VideoTranscriptionChunk) []acousticCharacter {
		var words []model.TranscriptionSegment
		_ = json.Unmarshal([]byte(row.TimedSegments), &words)
		text := []rune(strings.TrimSpace(row.Content))
		var result []acousticCharacter
		for i, word := range words {
			if word.EndMS < start || word.StartMS > end {
				continue
			}
			cut := len(text)
			if i+1 < len(words) {
				cut = words[i+1].TextStart
			}
			last := -1
			for _, r := range text[word.TextStart:word.TextEnd] {
				if unicode.IsLetter(r) || unicode.IsNumber(r) {
					result = append(result, acousticCharacter{rune: unicode.ToLower(r), startMS: word.StartMS, endMS: word.EndMS, cut: cut})
					last = len(result) - 1
				}
			}
			if last >= 0 {
				result[last].wordEnd = true
			}
		}
		return result
	}
	a, b := characters(left), characters(right)
	compatible := func(i, j int) bool {
		return a[i].rune == b[j].rune && absTime((a[i].startMS+a[i].endMS)/2-(b[j].startMS+b[j].endMS)/2) <= 600
	}
	// Exact source runs are bounded by the small audio overlap, not video
	// length. Punctuation/case differences do not alter the source offsets.
	back := make([][]int, len(a)+1)
	for i := range back {
		back[i] = make([]int, len(b)+1)
	}
	for i := range a {
		for j := range b {
			if compatible(i, j) {
				back[i+1][j+1] = back[i][j] + 1
			}
		}
	}
	bestDistance, bestDisagreement := int64(1501), int64(1<<60)
	leftCut, rightCut, found := 0, 0, false
	for i := range a {
		for j := range b {
			if !a[i].wordEnd || !b[j].wordEnd || !compatible(i, j) || absTime(a[i].endMS-b[j].endMS) > 240 {
				continue
			}
			run := back[i+1][j+1]
			for k := 1; i+k < len(a) && j+k < len(b) && compatible(i+k, j+k); k++ {
				run++
			}
			if run < 4 {
				continue
			}
			cutTime := (a[i].endMS + b[j].endMS) / 2
			distance, disagreement := absTime(cutTime-right.CoreStartMS), absTime(a[i].endMS-b[j].endMS)
			if cutTime < start || cutTime > end || distance > 1500 {
				continue
			}
			if distance < bestDistance || distance == bestDistance && disagreement < bestDisagreement {
				leftCut, rightCut, found = a[i].cut, b[j].cut, true
				bestDistance, bestDisagreement = distance, disagreement
			}
		}
	}
	return leftCut, rightCut, found
}

func absTime(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
