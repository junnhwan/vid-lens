package service

import (
	"strings"
	"testing"

	"vid-lens/internal/model"
)

func continuityRows() []model.VideoTranscriptionChunk {
	return []model.VideoTranscriptionChunk{
		{ID: 1, ChunkIndex: 0, SegmentKey: "a", SegmenterVersion: "bounded_windows_v2", Status: model.TranscriptionChunkStatusCompleted,
			WindowStartMS: 0, WindowEndMS: 22000, CoreStartMS: 0, CoreEndMS: 20000, Content: "前文。这里是完整的一句话"},
		{ID: 2, ChunkIndex: 1, SegmentKey: "b", SegmenterVersion: "bounded_windows_v2", Status: model.TranscriptionChunkStatusCompleted,
			WindowStartMS: 18000, WindowEndMS: 42000, CoreStartMS: 20000, CoreEndMS: 40000, Content: "这里是完整的一句话。然后继续。"},
	}
}

func TestTranscriptContinuityTimelineAndIndexShareRetainedSource(t *testing.T) {
	rows := continuityRows()
	want := "前文。这里是完整的一句话。然后继续。"
	timeline := BuildVideoTimeline(1, rows, nil)
	var text strings.Builder
	for _, atom := range timeline.Atoms {
		text.WriteString(atom.Content)
		if len(atom.SourceRefs) == 0 {
			t.Fatal("reader lost playback provenance")
		}
	}
	if text.String() != want {
		t.Fatalf("reader repeats overlapping speech: got %q want %q", text.String(), want)
	}
	chunks := buildTranscriptIndexChunks(want, rows, 800, 0)
	if len(chunks) != 1 || chunks[0].Content != want || chunks[0].SourceMappingStatus != model.ChunkSourceMapped {
		t.Fatalf("reader and retrieval disagree: %+v", chunks)
	}
}

func TestTranscriptContinuityDoesNotDeduplicateAcrossMissingWindow(t *testing.T) {
	rows := continuityRows()
	rows[0].Content = "第一次。这里是重复的一句话。"
	rows[1].ChunkIndex = 2
	rows[1].Content = "这里是重复的一句话。另一次。"
	// Even accidentally overlapping historical ranges do not prove that the
	// two rows are consecutive observations of the same spoken occurrence.
	chunks := buildTranscriptIndexChunks(rows[0].Content+"\n\n"+rows[1].Content, rows, 800, 0)
	if len(chunks) != 1 || chunks[0].SourceMappingStatus != model.ChunkSourceMapped || strings.Count(chunks[0].Content, "这里是重复的一句话") != 2 {
		t.Fatalf("missing window lost speech or provenance: %+v", chunks)
	}
}
