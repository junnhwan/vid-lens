package investigator

import (
	"errors"

	"vidlens/internal/domain"
)

const authoringCatalogRule = "transcript_index is zero-based and identifies the exact element of the complete transcript already supplied above. Use the existing tN alias in evidence_ids and body citation_ids; the server resolves its immutable segment ID, text origin and real range. canonical_range is supplied only when the original cue must be intersected with actual media. ASR windows remain coarse, never sentence timestamps. fN entries retain their actual frame_id and time_ms."

// The full immutable transcript already contains IDs, source timing and text
// origin. Authoring emits aliases only, so repeating that metadata for every
// segment on every turn adds context without adding evidence.
func authoringCatalogHints(catalog map[string]domain.Citation, transcript []domain.Segment) (map[string]any, error) {
	indices := make(map[string]int, len(transcript))
	for i, segment := range transcript {
		indices[segment.ID] = i
	}
	out := make(map[string]any, len(catalog))
	for alias, citation := range catalog {
		if citation.Kind != "video_text" {
			out[alias] = map[string]any{"frame_id": citation.FrameID, "time_ms": citation.TimeMS}
			continue
		}
		if len(citation.SegmentIDs) != 1 {
			return nil, errors.New("authoring alias must identify one original transcript element")
		}
		index, ok := indices[citation.SegmentIDs[0]]
		if !ok || citation.Range == nil {
			return nil, errors.New("authoring alias is missing its original source range")
		}
		hint := map[string]any{"transcript_index": index}
		segment := transcript[index]
		if citation.Range.StartMS != segment.StartMS || citation.Range.EndMS != segment.EndMS {
			hint["canonical_range"] = citation.Range
		}
		out[alias] = hint
	}
	return out, nil
}
