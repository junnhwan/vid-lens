package service

import (
	"context"
	"sort"
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

	byTask := map[int64][]model.VideoChunk{}
	expanded := make([]RetrievedChunk, 0, len(citations))
	seen := make(map[string]bool, len(citations))
	for _, citation := range citations {
		key := retrievalChunkKey(citation)
		if seen[key] {
			continue
		}
		seen[key] = true

		if err := ctx.Err(); err != nil {
			return nil, err
		}
		citationTaskID := citation.TaskID
		if citationTaskID <= 0 {
			citationTaskID = taskID
		}
		window, loaded := byTask[citationTaskID]
		var err error
		if !loaded {
			window, err = e.repos.VideoChunk.ListByTaskID(userID, citationTaskID, embeddingModel)
			byTask[citationTaskID] = window
		}
		window = continuousChunkWindow(window, citation.ChunkIndex, e.Radius)
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
		next.ContextTimeStatus = model.ChunkTimeRangeExact
		next.ContextStartMS, next.ContextEndMS = -1, 0
		for _, row := range window {
			if row.ChunkIndex < selectedStart || row.ChunkIndex > selectedEnd {
				continue
			}
			next.ContextSourceRefs = append(next.ContextSourceRefs, sourceRefsForModelChunk(row)...)
			if normalizedTimeRangeStatus(row.TimeRangeStatus, row.StartMS, row.EndMS) == model.ChunkTimeRangeUnknown {
				next.ContextTimeStatus = model.ChunkTimeRangeUnknown
			} else if next.ContextTimeStatus != model.ChunkTimeRangeUnknown && row.TimeRangeStatus != model.ChunkTimeRangeExact {
				next.ContextTimeStatus = model.ChunkTimeRangeCoarse
			}
			if next.ContextStartMS < 0 || row.StartMS < next.ContextStartMS {
				next.ContextStartMS = row.StartMS
			}
			if row.EndMS > next.ContextEndMS {
				next.ContextEndMS = row.EndMS
			}
		}
		if next.ContextTimeStatus == model.ChunkTimeRangeUnknown || next.ContextStartMS < 0 {
			next.ContextTimeStatus = model.ChunkTimeRangeUnknown
			next.ContextStartMS = 0
			next.ContextEndMS = 0
		}
		if next.WindowTruncated {
			next.Fallbacks = appendFallback(next.Fallbacks, "window_truncated")
		}
		expanded = append(expanded, next)
	}
	return mergeExpandedContexts(expanded, byTask, e.MaxCharsPerCitation), nil
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
	return joinVerbatimParts(parts)
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
	blocked := map[int]bool{}
	for distance := 1; anchorPos-distance >= 0 || anchorPos+distance < len(chunks); distance++ {
		for _, pos := range []int{anchorPos - distance, anchorPos + distance} {
			direction := 1
			if pos < anchorPos {
				direction = -1
			}
			if blocked[direction] || pos < 0 || pos >= len(chunks) {
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
			} else {
				blocked[direction] = true
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
	return joinVerbatimParts(parts), anchor, start, end, true, true
}

func appendFallback(fallbacks []string, fallback string) []string {
	for _, existing := range fallbacks {
		if existing == fallback {
			return fallbacks
		}
	}
	return append(fallbacks, fallback)
}

// Global chunk indices mix modalities. Select neighbors by measured source time,
// never by index alone, and stop at a discontinuity or an unknown mapping.
func continuousChunkWindow(rows []model.VideoChunk, anchorIndex, radius int) []model.VideoChunk {
	var anchor *model.VideoChunk
	for i := range rows {
		if rows[i].ChunkIndex == anchorIndex {
			anchor = &rows[i]
			break
		}
	}
	if anchor == nil {
		return nil
	}
	if anchor.Modality == model.ChunkModalityUnknown || anchor.SourceMappingStatus != model.ChunkSourceMapped || normalizedTimeRangeStatus(anchor.TimeRangeStatus, anchor.StartMS, anchor.EndMS) == model.ChunkTimeRangeUnknown {
		return []model.VideoChunk{*anchor}
	}
	var same []model.VideoChunk
	for _, row := range rows {
		if row.Modality == anchor.Modality && row.TaskID == anchor.TaskID && row.SourceMappingStatus == model.ChunkSourceMapped && normalizedTimeRangeStatus(row.TimeRangeStatus, row.StartMS, row.EndMS) != model.ChunkTimeRangeUnknown {
			same = append(same, row)
		}
	}
	sort.SliceStable(same, func(i, j int) bool {
		if same[i].StartMS != same[j].StartMS {
			return same[i].StartMS < same[j].StartMS
		}
		return same[i].ChunkIndex < same[j].ChunkIndex
	})
	pos := 0
	for i := range same {
		if same[i].ChunkIndex == anchorIndex {
			pos = i
			break
		}
	}
	left, right := pos, pos
	for n := 0; n < radius && left > 0; n++ {
		if same[left].StartMS > same[left-1].EndMS+250 {
			break
		}
		left--
	}
	for n := 0; n < radius && right+1 < len(same); n++ {
		if same[right+1].StartMS > same[right].EndMS+250 {
			break
		}
		right++
	}
	return same[left : right+1]
}

func joinVerbatimParts(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for _, part := range parts[1:] {
		a, b := []rune(result), []rune(part)
		overlap := 0
		for n := min(len(a), len(b)); n >= 8; n-- {
			if string(a[len(a)-n:]) == string(b[:n]) {
				overlap = n
				break
			}
		}
		if overlap > 0 {
			result += string(b[overlap:])
		} else {
			result += "\n" + part
		}
	}
	return result
}

// Overlapping windows share one canonical verbatim context while retaining each
// anchor identity/quote and its own source range. Budget overflow keeps them separate.
func mergeExpandedContexts(chunks []RetrievedChunk, rowsByTask map[int64][]model.VideoChunk, budget int) []RetrievedChunk {
	for i := range chunks {
		for j := i + 1; j < len(chunks); j++ {
			a, b := &chunks[i], &chunks[j]
			if a.TaskID != b.TaskID || a.Modality != b.Modality || a.AnchorContent == "" || b.AnchorContent == "" || a.ContextTimeStatus == model.ChunkTimeRangeUnknown || b.ContextTimeStatus == model.ChunkTimeRangeUnknown || a.ExpandedWindowStart > b.ExpandedWindowEnd || b.ExpandedWindowStart > a.ExpandedWindowEnd {
				continue
			}
			start, end := min(a.ExpandedWindowStart, b.ExpandedWindowStart), max(a.ExpandedWindowEnd, b.ExpandedWindowEnd)
			var rows []model.VideoChunk
			for _, row := range rowsByTask[a.TaskID] {
				if row.ChunkIndex >= start && row.ChunkIndex <= end && row.Modality == a.Modality {
					rows = append(rows, row)
				}
			}
			joined := joinChunkWindow(rows)
			if len(rows) == 0 || (budget > 0 && runeCount(joined) > budget) || !strings.Contains(joined, a.AnchorContent) || !strings.Contains(joined, b.AnchorContent) {
				continue
			}
			valid := true
			for n := 1; n < len(rows); n++ {
				if rows[n].StartMS > rows[n-1].EndMS+250 || rows[n].TimeRangeStatus == model.ChunkTimeRangeUnknown {
					valid = false
				}
			}
			if !valid {
				continue
			}
			var refs []ChunkSourceRef
			seen := map[string]bool{}
			for _, row := range rows {
				for _, ref := range sourceRefsForModelChunk(row) {
					key := ref.SourceType + ":" + ref.StableID
					if !seen[key] {
						seen[key] = true
						refs = append(refs, ref)
					}
				}
			}
			for _, c := range []*RetrievedChunk{a, b} {
				c.Content = joined
				c.ExpandedWindowStart, c.ExpandedWindowEnd = start, end
				c.ContextStartMS = rows[0].StartMS
				c.ContextEndMS = rows[len(rows)-1].EndMS
				c.ContextSourceRefs = refs
			}
		}
	}
	return chunks
}
