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
