package service

import (
	"strings"
	"unicode"
)

type TextChunk struct {
	Index               int
	Content             string
	TokenCount          int
	Modality            string
	StartMS             int64
	EndMS               int64
	TimeRangeStatus     string
	SourceMappingStatus string
	SourceRefs          []ChunkSourceRef
}

// SplitTextIntoChunks splits text at the strongest available semantic boundary.
// Sentence and clause units are kept intact whenever they fit in chunkSize. Only
// text with no usable punctuation or whitespace falls back to rune-level units.
func SplitTextIntoChunks(text string, chunkSize, overlap int) []TextChunk {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if chunkSize <= 0 {
		chunkSize = 800
	}
	if overlap < 0 || overlap >= chunkSize {
		overlap = 0
	}

	// This compatibility helper is also used for bounded display excerpts, whose
	// historical budget is character based. The provenance-aware RAG path uses
	// SplitObservationsIntoChunks and its conservative token estimator.
	units := splitSemanticUnitsBy(text, chunkSize, 0, runeCount)
	return packSemanticUnitsBy(units, chunkSize, overlap, runeCount)
}

const (
	primaryBoundaryLevel = iota
	secondaryBoundaryLevel
	whitespaceBoundaryLevel
	hardBoundaryLevel
)

func splitSemanticUnits(text string, chunkSize, level int) []string {
	return splitSemanticUnitsBy(text, chunkSize, level, EstimateChunkTokens)
}

func splitSemanticUnitsBy(text string, chunkSize, level int, measure func(string) int) []string {
	if text == "" {
		return nil
	}
	if measure(text) <= chunkSize {
		return []string{text}
	}
	if level >= hardBoundaryLevel {
		runes := []rune(text)
		units := make([]string, 0, len(runes))
		for _, r := range runes {
			units = append(units, string(r))
		}
		return units
	}

	parts := splitAtBoundaryLevel(text, level)
	if len(parts) <= 1 {
		return splitSemanticUnitsBy(text, chunkSize, level+1, measure)
	}

	units := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		if measure(part) <= chunkSize {
			units = append(units, part)
			continue
		}
		units = append(units, splitSemanticUnitsBy(part, chunkSize, level+1, measure)...)
	}
	return units
}

func splitAtBoundaryLevel(text string, level int) []string {
	runes := []rune(text)
	parts := make([]string, 0)
	start := 0

	for i := 0; i < len(runes); {
		if !isBoundaryAtLevel(runes[i], level) {
			i++
			continue
		}

		end := i + 1
		switch level {
		case primaryBoundaryLevel:
			for end < len(runes) && (isPrimaryBoundary(runes[end]) || isClosingPunctuation(runes[end])) {
				end++
			}
			for end < len(runes) && unicode.IsSpace(runes[end]) {
				end++
			}
		case secondaryBoundaryLevel, whitespaceBoundaryLevel:
			for end < len(runes) && isBoundaryAtLevel(runes[end], level) {
				end++
			}
			for end < len(runes) && unicode.IsSpace(runes[end]) {
				end++
			}
		}

		parts = append(parts, string(runes[start:end]))
		start = end
		i = end
	}
	if start < len(runes) {
		parts = append(parts, string(runes[start:]))
	}
	return parts
}

func isBoundaryAtLevel(r rune, level int) bool {
	switch level {
	case primaryBoundaryLevel:
		return isPrimaryBoundary(r)
	case secondaryBoundaryLevel:
		return isSecondaryBoundary(r)
	case whitespaceBoundaryLevel:
		return unicode.IsSpace(r)
	default:
		return false
	}
}

func isPrimaryBoundary(r rune) bool {
	switch r {
	case '。', '！', '？', '；', '.', '!', '?', ';', '\n', '\r':
		return true
	default:
		return false
	}
}

func isSecondaryBoundary(r rune) bool {
	switch r {
	case '，', ',', '、', '：', ':':
		return true
	default:
		return false
	}
}

func isClosingPunctuation(r rune) bool {
	switch r {
	case '”', '’', '」', '』', '》', '）', ')', '】', ']', '"', '\'':
		return true
	default:
		return false
	}
}

func packSemanticUnits(units []string, chunkSize, overlap int) []TextChunk {
	return packSemanticUnitsBy(units, chunkSize, overlap, EstimateChunkTokens)
}

func packSemanticUnitsBy(units []string, chunkSize, overlap int, measure func(string) int) []TextChunk {
	chunks := make([]TextChunk, 0, len(units))
	for start := 0; start < len(units); {
		end := start
		contentTokens := 0
		for end < len(units) {
			unitTokens := measure(units[end])
			if end > start && contentTokens+unitTokens > chunkSize {
				break
			}
			contentTokens += unitTokens
			end++
			if contentTokens >= chunkSize {
				break
			}
		}

		content := strings.TrimSpace(strings.Join(units[start:end], ""))
		if content != "" {
			chunks = append(chunks, TextChunk{Index: len(chunks), Content: content, TokenCount: measure(content)})
		}
		if end >= len(units) {
			break
		}

		next := semanticOverlapStartBy(units, start, end, chunkSize, overlap, measure)
		if next <= start || next > end {
			next = end
		}
		start = next
	}
	return chunks
}

// semanticOverlapStart returns a suffix made only of complete units. It also
// reserves room for the first unseen unit so a chunk can never contain overlap
// alone. Rune-level fallback units naturally preserve character overlap for
// text that has no semantic boundaries.
func semanticOverlapStart(units []string, start, end, chunkSize, overlap int) int {
	return semanticOverlapStartBy(units, start, end, chunkSize, overlap, EstimateChunkTokens)
}

func semanticOverlapStartBy(units []string, start, end, chunkSize, overlap int, measure func(string) int) int {
	if overlap <= 0 || end >= len(units) {
		return end
	}

	nextUnitTokens := measure(units[end])
	suffixTokens := 0
	candidate := end
	for i := end - 1; i > start; i-- {
		unitTokens := measure(units[i])
		if suffixTokens+unitTokens > overlap || suffixTokens+unitTokens+nextUnitTokens > chunkSize {
			break
		}
		suffixTokens += unitTokens
		candidate = i
	}
	return candidate
}

func runeCount(text string) int {
	return len([]rune(text))
}

// EstimateChunkTokens is a deterministic provider-independent upper budget:
// valid UTF-8 text cannot produce more byte-level tokenizer pieces than input
// bytes. It deliberately overbudgets common Latin and CJK text; provider usage
// remains authoritative for billing and evaluation.
func EstimateChunkTokens(text string) int {
	return len([]byte(text))
}
