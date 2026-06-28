package service

import (
	"context"
	"strings"

	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

type ContextExpander struct {
	repos               *repository.Repositories
	Radius              int
	MaxCharsPerCitation int
}

func NewContextExpander(repos *repository.Repositories, radius, maxCharsPerCitation int) *ContextExpander {
	return &ContextExpander{
		repos:               repos,
		Radius:              radius,
		MaxCharsPerCitation: maxCharsPerCitation,
	}
}

func (e *ContextExpander) Expand(ctx context.Context, userID, taskID int64, embeddingModel string, citations []RetrievedChunk) ([]RetrievedChunk, error) {
	if len(citations) == 0 {
		return nil, nil
	}
	if e == nil || e.repos == nil || e.repos.VideoChunk == nil {
		return markWindowExpansionFallback(citations), nil
	}

	expanded := make([]RetrievedChunk, 0, len(citations))
	seen := make(map[string]bool, len(citations))
	for _, citation := range citations {
		key := retrievalChunkKey(citation)
		if seen[key] {
			continue
		}
		seen[key] = true

		start := citation.ChunkIndex - e.Radius
		if start < 0 {
			start = 0
		}
		end := citation.ChunkIndex + e.Radius
		window, err := e.repos.VideoChunk.ListByIndexRange(userID, taskID, embeddingModel, start, end)
		if err != nil {
			fallback := citation
			fallback.Fallbacks = appendFallback(fallback.Fallbacks, "window_expansion_failed")
			expanded = append(expanded, fallback)
			continue
		}
		if len(window) == 0 {
			fallback := citation
			fallback.Fallbacks = appendFallback(fallback.Fallbacks, "window_expansion_empty")
			expanded = append(expanded, fallback)
			continue
		}

		next := citation
		if next.AnchorContent == "" {
			next.AnchorContent = citation.Content
		}
		next.Content = joinChunkWindow(window)
		next.ExpandedFromChunkIndex = citation.ChunkIndex
		next.ExpandedWindowStart = window[0].ChunkIndex
		next.ExpandedWindowEnd = window[len(window)-1].ChunkIndex
		if e.MaxCharsPerCitation > 0 {
			next.Content, next.WindowTruncated = truncateRunes(next.Content, e.MaxCharsPerCitation)
			if next.WindowTruncated {
				next.Fallbacks = appendFallback(next.Fallbacks, "window_truncated")
			}
		}
		expanded = append(expanded, next)
	}
	return expanded, nil
}

func markWindowExpansionFallback(citations []RetrievedChunk) []RetrievedChunk {
	fallback := make([]RetrievedChunk, len(citations))
	for i, citation := range citations {
		citation.Fallbacks = appendFallback(citation.Fallbacks, "window_expansion_failed")
		fallback[i] = citation
	}
	return fallback
}

func joinChunkWindow(chunks []model.VideoChunk) string {
	parts := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		content := strings.TrimSpace(chunk.Content)
		if content != "" {
			parts = append(parts, content)
		}
	}
	return strings.Join(parts, "\n")
}

func truncateRunes(text string, maxRunes int) (string, bool) {
	runes := []rune(text)
	if maxRunes <= 0 || len(runes) <= maxRunes {
		return text, false
	}
	return string(runes[:maxRunes]), true
}

func appendFallback(fallbacks []string, fallback string) []string {
	for _, existing := range fallbacks {
		if existing == fallback {
			return fallbacks
		}
	}
	return append(fallbacks, fallback)
}
