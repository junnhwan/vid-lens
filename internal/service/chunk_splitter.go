package service

import "strings"

type TextChunk struct {
	Index   int
	Content string
}

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

	runes := []rune(text)
	chunks := make([]TextChunk, 0, len(runes)/chunkSize+1)
	for start, index := 0, 0; start < len(runes); index++ {
		end := start + chunkSize
		if end > len(runes) {
			end = len(runes)
		} else if semanticEnd := findSemanticChunkEnd(runes, start, end, chunkSize); semanticEnd > start {
			end = semanticEnd
		}
		content := strings.TrimSpace(string(runes[start:end]))
		if content != "" {
			chunks = append(chunks, TextChunk{Index: index, Content: content})
		}
		if end >= len(runes) {
			if overlap > 0 && len(runes)-start > overlap {
				start = len(runes) - overlap
				continue
			}
			break
		}
		next := end - overlap
		if next <= start {
			next = end
		}
		start = next
	}
	return chunks
}

func findSemanticChunkEnd(runes []rune, start, hardEnd, chunkSize int) int {
	minimum := start + chunkSize/2
	if minimum <= start {
		minimum = start + 1
	}
	for i := hardEnd - 1; i >= minimum-1; i-- {
		if isSemanticBoundary(runes[i]) {
			return i + 1
		}
	}
	return hardEnd
}

func isSemanticBoundary(r rune) bool {
	switch r {
	case '。', '！', '？', '；', '.', '!', '?', ';', '\n':
		return true
	default:
		return false
	}
}
