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
		citationTaskID := citation.TaskID
		if citationTaskID <= 0 {
			citationTaskID = taskID
		}
		window, err := e.repos.VideoChunk.ListByIndexRange(userID, citationTaskID, embeddingModel, start, end)
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
		content, anchor, selectedStart, selectedEnd, truncated, ok := joinChunkWindowPreservingAnchor(window, citation.ChunkIndex, e.MaxCharsPerCitation)
		if !ok {
			fallback := citation
			fallback.Fallbacks = appendFallback(fallback.Fallbacks, "window_anchor_missing")
			expanded = append(expanded, fallback)
			continue
		}
		next.AnchorContent = anchor
		next.Content = content
		next.ExpandedFromChunkIndex = citation.ChunkIndex
		next.ExpandedWindowStart = selectedStart
		next.ExpandedWindowEnd = selectedEnd
		next.WindowTruncated = truncated
		if next.WindowTruncated {
			next.Fallbacks = appendFallback(next.Fallbacks, "window_truncated")
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

func joinChunkWindowPreservingAnchor(chunks []model.VideoChunk, anchorIndex, maxRunes int) (content, anchor string, start, end int, truncated, ok bool) {
	anchorPos := -1
	for i := range chunks {
		if chunks[i].ChunkIndex == anchorIndex {
			anchorPos = i
			break
		}
	}
	if anchorPos < 0 {
		return "", "", 0, 0, false, false
	}
	anchor = strings.TrimSpace(chunks[anchorPos].Content)
	if anchor == "" {
		return "", "", 0, 0, false, false
	}
	joined := joinChunkWindow(chunks)
	if maxRunes <= 0 || len([]rune(joined)) <= maxRunes {
		return joined, anchor, chunks[0].ChunkIndex, chunks[len(chunks)-1].ChunkIndex, false, true
	}

	// The citation identity points at the anchor chunk, so the anchor is a hard
	// constraint while the configured character budget is soft. If the anchor
	// itself exceeds the budget, keep it intact and omit all neighbors.
	selected := map[int]bool{anchorPos: true}
	used := len([]rune(anchor))
	for distance := 1; anchorPos-distance >= 0 || anchorPos+distance < len(chunks); distance++ {
		for _, pos := range []int{anchorPos - distance, anchorPos + distance} {
			if pos < 0 || pos >= len(chunks) {
				continue
			}
			part := strings.TrimSpace(chunks[pos].Content)
			if part == "" {
				continue
			}
			cost := len([]rune(part))
			if used > 0 {
				cost++ // newline separator
			}
			if used+cost <= maxRunes {
				selected[pos] = true
				used += cost
			}
		}
	}
	parts := make([]string, 0, len(selected))
	start, end = anchorIndex, anchorIndex
	for i, chunk := range chunks {
		if !selected[i] {
			continue
		}
		part := strings.TrimSpace(chunk.Content)
		if part == "" {
			continue
		}
		parts = append(parts, part)
		if chunk.ChunkIndex < start {
			start = chunk.ChunkIndex
		}
		if chunk.ChunkIndex > end {
			end = chunk.ChunkIndex
		}
	}
	return strings.Join(parts, "\n"), anchor, start, end, true, true
}

func appendFallback(fallbacks []string, fallback string) []string {
	for _, existing := range fallbacks {
		if existing == fallback {
			return fallbacks
		}
	}
	return append(fallbacks, fallback)
}
