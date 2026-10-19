package investigator

import (
	"encoding/json"
	"strconv"
	"strings"

	"vidlens/internal/domain"
)

type evidenceReviewBatch struct {
	Mode   string
	Frames []domain.Asset
	Fields map[string]any
}

func evidenceReviewBatches(candidate any, catalog map[string]domain.Citation, frames []domain.Asset) []evidenceReviewBatch {
	out := []evidenceReviewBatch{{Mode: "source_text"}}
	for start := 0; start < len(frames); start += 3 {
		group := frames[start:min(start+3, len(frames))]
		fields := visualReviewFields(candidate, catalog, group)
		if len(fields) > 0 {
			out = append(out, evidenceReviewBatch{Mode: "visual", Frames: group, Fields: fields})
		}
	}
	return out
}

// Project existing canonical fields without reindexing or rewriting them. The
// full candidate remains the hash/validation boundary, while each visual call
// sees only claims linked to its actual pixels and their section headings.
func visualReviewFields(candidate any, catalog map[string]domain.Citation, frames []domain.Asset) map[string]any {
	allowed := map[string]bool{}
	for _, frame := range frames {
		allowed[frame.ID] = true
		for id, citation := range catalog {
			if citation.Kind == "video_frame" && citation.FrameID == frame.ID {
				allowed[id] = true
			}
		}
	}
	var doc map[string]any
	_ = json.Unmarshal(domain.JSON(candidate), &doc)
	out := map[string]any{}
	addBlocks := func(raw any, prefix string) bool {
		found := false
		blocks, _ := raw.([]any)
		for index, block := range blocks {
			if referencesReviewFrame(block, allowed) {
				out[prefix+"/"+strconv.Itoa(index)] = block
				found = true
			}
		}
		return found
	}
	sections, _ := doc["sections"].([]any)
	for index, raw := range sections {
		section, _ := raw.(map[string]any)
		prefix := "/sections/" + strconv.Itoa(index)
		if addBlocks(section["blocks"], prefix+"/blocks") {
			out[prefix+"/title"] = section["title"]
		}
	}
	addBlocks(doc["blocks"], "/blocks")
	limits, _ := doc["limitations"].([]any)
	for index, limit := range limits {
		if referencesReviewFrame(limit, allowed) {
			out["/limitations/"+strconv.Itoa(index)] = limit
		}
	}
	if summary, _ := doc["summary"].(string); summary != "" {
		out["/summary"] = summary
	}
	return out
}

func referencesReviewFrame(value any, allowed map[string]bool) bool {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if key == "citation_id" {
				if id, ok := child.(string); ok && allowed[id] {
					return true
				}
			}
			if key == "citation_ids" || key == "frame_ids" {
				ids, _ := child.([]any)
				for _, raw := range ids {
					if id, ok := raw.(string); ok && allowed[id] {
						return true
					}
				}
			}
			if referencesReviewFrame(child, allowed) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if referencesReviewFrame(child, allowed) {
				return true
			}
		}
	}
	return false
}

func (b evidenceReviewBatch) containsPath(path string) bool {
	if b.Mode == "source_text" && b.Fields == nil {
		return true
	}
	for prefix := range b.Fields {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}
