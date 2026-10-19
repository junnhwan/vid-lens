package investigator

import (
	"bytes"
	"testing"

	"vidlens/internal/domain"
)

func TestVisualReviewScopesKeepOriginalPointersAndNestedReferences(t *testing.T) {
	candidate := map[string]any{"sections": []any{
		map[string]any{"id": "opening", "title": "Opening", "blocks": []any{
			map[string]any{"type": "figure", "frame_ids": []string{"frame1"}, "caption": "First test image."},
			map[string]any{"type": "paragraph", "text": "The literal word f5 is not a reference."},
			map[string]any{"type": "table", "rows": []any{map[string]any{"cells": []string{"value"}, "citation_ids": []string{"f2"}}}},
		}},
		map[string]any{"id": "finish", "title": "Finish", "blocks": []any{
			map[string]any{"type": "list", "items": []any{map[string]any{"content": []any{map[string]any{"type": "citation", "citation_id": "f4"}}}}},
		}},
	}}
	frames := []domain.Asset{{ID: "frame1"}, {ID: "frame2"}, {ID: "frame3"}, {ID: "frame4"}, {ID: "frame5"}}
	catalog := map[string]domain.Citation{"f1": {Kind: "video_frame", FrameID: "frame1"}, "f2": {Kind: "video_frame", FrameID: "frame2"}, "f4": {Kind: "video_frame", FrameID: "frame4"}, "f5": {Kind: "video_frame", FrameID: "frame5"}}
	before := domain.JSON(candidate)
	batches := evidenceReviewBatches(candidate, catalog, frames)
	if len(batches) != 3 || batches[0].Mode != "source_text" || len(batches[0].Frames) != 0 {
		t.Fatal("full source facts were not reviewed separately", batches)
	}
	for _, batch := range batches[1:] {
		if len(batch.Frames) > 3 || batch.Mode != "visual" {
			t.Fatal("a visual call received an unbounded mixed task", batch)
		}
	}
	first, second := batches[1], batches[2]
	if !first.containsPath("/sections/0/blocks/0/caption") || !first.containsPath("/sections/0/blocks/2/rows/0") || first.containsPath("/sections/0/blocks/1") || first.containsPath("/sections/1/blocks/0") {
		t.Fatal("visual fields lost their original offsets or included unrelated claims", first)
	}
	if !second.containsPath("/sections/1/blocks/0/items/0") || second.containsPath("/sections/0/blocks/0") {
		t.Fatal("nested inline references or chapter identity were lost", second)
	}
	if !bytes.Equal(before, domain.JSON(candidate)) {
		t.Fatal("scoping changed the candidate or its publication hash")
	}
}
