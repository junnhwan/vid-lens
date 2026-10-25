package investigator

import "vidlens/internal/domain"

// Adjacent paraphrase references carry the same immutable source identities in
// one binding. No source, quote, prose, block identity or time bound is dropped.
func compactDeliveryCitations(a *domain.Article, transcript []domain.Segment) {
	ordinals := map[string]int{}
	for i, original := range transcript {
		ordinals[original.ID] = i
	}
	refs := func(ids []string) []string {
		out := []string{}
		for i := 0; i < len(ids); {
			first, ok := a.Citations[ids[i]]
			eligible := ok && first.Kind == "video_text" && first.Quote == nil && first.Range != nil && len(first.SegmentIDs) > 0
			lastOrdinal := -1
			if eligible {
				for j, id := range first.SegmentIDs {
					ordinal, exists := ordinals[id]
					if !exists || j > 0 && ordinal != lastOrdinal+1 {
						eligible = false
						break
					}
					lastOrdinal = ordinal
				}
			}
			if !eligible {
				out = append(out, ids[i])
				i++
				continue
			}
			group := first
			group.SegmentIDs = append([]string{}, first.SegmentIDs...)
			rangeValue := *first.Range
			group.Range = &rangeValue
			end := i + 1
			for end < len(ids) {
				next, exists := a.Citations[ids[end]]
				if !exists || next.Kind != first.Kind || next.Quote != nil || next.Range == nil || next.SnapshotID != first.SnapshotID || next.TextOrigin != first.TextOrigin || next.Label != first.Label || len(next.SegmentIDs) == 0 {
					break
				}
				ordinal := lastOrdinal
				continuous := true
				for _, id := range next.SegmentIDs {
					position, exists := ordinals[id]
					if !exists || position != ordinal+1 {
						continuous = false
						break
					}
					ordinal = position
				}
				if !continuous {
					break
				}
				group.SegmentIDs = append(group.SegmentIDs, next.SegmentIDs...)
				group.Range.StartMS = min(group.Range.StartMS, next.Range.StartMS)
				group.Range.EndMS = max(group.Range.EndMS, next.Range.EndMS)
				lastOrdinal = ordinal
				end++
			}
			if end > i+1 {
				group.ID = ""
				group.ID = "sources_" + domain.ContentHash(group)[:24]
				a.Citations[group.ID] = group
				out = append(out, group.ID)
			} else {
				out = append(out, ids[i])
			}
			i = end
		}
		return out
	}
	inlines := func(items []domain.Inline) []domain.Inline {
		out := []domain.Inline{}
		for i := 0; i < len(items); {
			if items[i].Type != "citation" {
				out = append(out, items[i])
				i++
				continue
			}
			ids := []string{}
			for i < len(items) && items[i].Type == "citation" {
				ids = append(ids, items[i].CitationID)
				i++
			}
			for _, id := range refs(ids) {
				out = append(out, domain.Inline{Type: "citation", CitationID: id})
			}
		}
		return out
	}
	for si := range a.Sections {
		for bi := range a.Sections[si].Blocks {
			b := &a.Sections[si].Blocks[bi]
			b.Content = inlines(b.Content)
			b.CitationIDs = refs(b.CitationIDs)
			for i := range b.Items {
				b.Items[i].Content = inlines(b.Items[i].Content)
			}
			for i := range b.Rows {
				b.Rows[i].CitationIDs = refs(b.Rows[i].CitationIDs)
			}
		}
	}
	for i := range a.Limitations {
		a.Limitations[i].CitationIDs = refs(a.Limitations[i].CitationIDs)
	}
}
