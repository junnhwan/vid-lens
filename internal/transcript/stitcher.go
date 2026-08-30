// Package transcript assembles independently generated ASR observations into
// one deterministic transcript. It does not call a model and never rewrites
// source wording.
package transcript

import (
	"strings"
	"unicode"
)

const (
	minimumOverlapRunes = 4
	maximumOverlapRunes = 256
)

type Boundary struct {
	LeftPart    int
	RightPart   int
	Method      string
	MatchRunes  int
	PrefixRunes int
}

type StitchResult struct {
	Content    string
	Boundaries []Boundary
}

// Stitch removes normalized suffix/prefix duplication created by overlapping
// audio windows. Punctuation and whitespace do not participate in matching,
// but original text is preserved in the output.
func Stitch(parts []string) StitchResult {
	nonEmpty := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			nonEmpty = append(nonEmpty, part)
		}
	}
	if len(nonEmpty) == 0 {
		return StitchResult{}
	}

	result := StitchResult{Content: nonEmpty[0], Boundaries: make([]Boundary, 0, len(nonEmpty)-1)}
	for i := 1; i < len(nonEmpty); i++ {
		merged, matchRunes, prefixRunes := stitchPair(result.Content, nonEmpty[i])
		method := "append"
		if matchRunes > 0 {
			method = "exact_normalized_overlap"
		}
		result.Content = merged
		result.Boundaries = append(result.Boundaries, Boundary{
			LeftPart: i - 1, RightPart: i, Method: method,
			MatchRunes: matchRunes, PrefixRunes: prefixRunes,
		})
	}
	return result
}

type comparableText struct {
	runes       []rune
	originalEnd []int
}

func stitchPair(left, right string) (string, int, int) {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" {
		return right, 0, 0
	}
	if right == "" {
		return left, 0, 0
	}

	leftComparable := comparable(left)
	rightComparable := comparable(right)
	maxMatch := len(leftComparable.runes)
	if len(rightComparable.runes) < maxMatch {
		maxMatch = len(rightComparable.runes)
	}
	if maxMatch > maximumOverlapRunes {
		maxMatch = maximumOverlapRunes
	}
	for size := maxMatch; size >= minimumOverlapRunes; size-- {
		if !equalRunes(leftComparable.runes[len(leftComparable.runes)-size:], rightComparable.runes[:size]) {
			continue
		}
		prefixEnd := rightComparable.originalEnd[size-1]
		remainder := strings.TrimLeftFunc(string([]rune(right)[prefixEnd:]), unicode.IsSpace)
		return joinPreservingWords(left, remainder), size, prefixEnd
	}
	return joinPreservingWords(left, right), 0, 0
}

func comparable(text string) comparableText {
	original := []rune(text)
	result := comparableText{runes: make([]rune, 0, len(original)), originalEnd: make([]int, 0, len(original))}
	for i, r := range original {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) {
			continue
		}
		result.runes = append(result.runes, unicode.ToLower(r))
		result.originalEnd = append(result.originalEnd, i+1)
	}
	return result
}

func equalRunes(left, right []rune) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func joinPreservingWords(left, right string) string {
	if right == "" {
		return left
	}
	leftRunes, rightRunes := []rune(left), []rune(right)
	if len(leftRunes) == 0 {
		return right
	}
	last, first := leftRunes[len(leftRunes)-1], rightRunes[0]
	if unicode.IsSpace(last) || unicode.IsSpace(first) || !needsWordSeparator(last, first) {
		return left + right
	}
	return left + " " + right
}

func needsWordSeparator(left, right rune) bool {
	return isLatinWordRune(left) && isLatinWordRune(right)
}

func isLatinWordRune(r rune) bool {
	return r <= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsNumber(r))
}
