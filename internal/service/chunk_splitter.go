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
	step := chunkSize - overlap
	for start, index := 0, 0; start < len(runes); start, index = start+step, index+1 {
		end := start + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		content := strings.TrimSpace(string(runes[start:end]))
		if content != "" {
			chunks = append(chunks, TextChunk{Index: index, Content: content})
		}
	}
	return chunks
}
