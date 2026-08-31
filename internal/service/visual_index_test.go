package service

import (
	"strings"
	"testing"

	"vid-lens/internal/model"
)

func TestFormatOCRChunksForIndexPrefixesTimestamp(t *testing.T) {
	frames := []model.VideoVisualFrame{
		{OCRText: "  前序遍历  ", TimeMs: 125_000, Status: model.VisualFrameStatusCompleted},
		{OCRText: "", TimeMs: 1},
		{OCRText: "根左右", TimeMs: 30_000},
	}
	got := FormatOCRChunksForIndex(frames)
	if len(got) != 2 {
		t.Fatalf("len=%d want 2: %#v", len(got), got)
	}
	if !strings.Contains(got[0].Content, "[画面 02:05]") && !strings.Contains(got[0].Content, "[画面OCR 02:05]") && !strings.Contains(got[0].Content, "[画面理解 02:05]") {
		t.Fatalf("first chunk missing timestamp prefix: %q", got[0].Content)
	}
	if !strings.Contains(got[0].Content, "前序遍历") {
		t.Fatalf("first chunk missing ocr text: %q", got[0].Content)
	}
	if !strings.Contains(got[1].Content, "00:30") {
		t.Fatalf("second chunk missing timestamp: %q", got[1].Content)
	}
}

func TestFormatVisualChunksKeepsOCRAndCaptionAsSeparateStableEvidence(t *testing.T) {
	frames := []model.VideoVisualFrame{{
		ID: 19, FrameKey: "vf_stable", TimeMs: 42_000, StartMS: 42_000, EndMS: 42_001,
		TimeStatus: model.ChunkTimeRangeExact, OCRText: "季度收入 120 万", VisionCaption: "柱状图显示收入增长",
		ObjectKey: "visual/task/frame.jpg", Status: model.VisualFrameStatusCompleted,
	}}
	chunks := FormatOCRChunksForIndex(frames)
	if len(chunks) != 2 {
		t.Fatalf("visual chunks = %+v, want OCR and caption", chunks)
	}
	if chunks[0].Modality != model.ChunkModalityVisualOCR || chunks[1].Modality != model.ChunkModalityVisualCaption {
		t.Fatalf("modalities = %q/%q", chunks[0].Modality, chunks[1].Modality)
	}
	for _, chunk := range chunks {
		if chunk.StartMS != 42_000 || chunk.EndMS != 42_001 || chunk.TimeRangeStatus != model.ChunkTimeRangeExact || len(chunk.SourceRefs) != 1 {
			t.Fatalf("unstable visual time mapping: %+v", chunk)
		}
		if chunk.SourceRefs[0].StableID != "visual-frame:vf_stable" || chunk.SourceRefs[0].SourceRowID != 19 {
			t.Fatalf("source ref = %+v", chunk.SourceRefs[0])
		}
	}
}

func TestFormatVisualChunksConvertsLongLegacyVisionRowsWithoutLeakingOCRModality(t *testing.T) {
	chunks := formatOCRChunksForIndex([]model.VideoVisualFrame{{
		ID: 21, TimeMs: 10_000, OCRText: strings.Repeat("视觉描述。", 40),
		CaptionMethod: "vision", Status: model.VisualFrameStatusCompleted,
	}}, 32)
	if len(chunks) < 2 {
		t.Fatalf("legacy chunks = %+v", chunks)
	}
	for _, chunk := range chunks {
		if chunk.Modality != model.ChunkModalityVisualCaption {
			t.Fatalf("legacy vision row leaked modality %q", chunk.Modality)
		}
	}
}
