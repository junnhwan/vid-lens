package service

import "testing"

func TestSplitTextIntoChunksReturnsSingleChunkForShortText(t *testing.T) {
	chunks := SplitTextIntoChunks("这是一段短文本", 100, 20)
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want 1", len(chunks))
	}
	if chunks[0].Index != 0 || chunks[0].Content != "这是一段短文本" {
		t.Fatalf("unexpected chunk: %+v", chunks[0])
	}
}

func TestSplitTextIntoChunksUsesOverlap(t *testing.T) {
	chunks := SplitTextIntoChunks("abcdefghijklmnopqrstuvwxyz", 10, 2)
	if len(chunks) != 3 {
		t.Fatalf("len(chunks) = %d, want 3 without a duplicate overlap-only tail: %+v", len(chunks), chunks)
	}

	want := []string{"abcdefghij", "ijklmnopqr", "qrstuvwxyz"}
	for i, chunk := range chunks {
		if chunk.Index != i {
			t.Fatalf("chunk[%d].Index = %d", i, chunk.Index)
		}
		if chunk.Content != want[i] {
			t.Fatalf("chunk[%d].Content = %q, want %q", i, chunk.Content, want[i])
		}
	}
}

func TestSplitTextIntoChunksReturnsEmptyForBlankText(t *testing.T) {
	chunks := SplitTextIntoChunks("  \n\t  ", 10, 2)
	if len(chunks) != 0 {
		t.Fatalf("len(chunks) = %d, want 0", len(chunks))
	}
}

func TestSplitTextIntoChunksRejectsInvalidOverlap(t *testing.T) {
	chunks := SplitTextIntoChunks("abcdefghij", 5, 5)
	if len(chunks) != 2 {
		t.Fatalf("len(chunks) = %d, want 2", len(chunks))
	}
	if chunks[0].Content != "abcde" || chunks[1].Content != "fghij" {
		t.Fatalf("unexpected chunks: %+v", chunks)
	}
}

func TestSplitTextIntoChunksPrefersSemanticBoundary(t *testing.T) {
	text := "第一句很短。第二句也不长。第三句结束。"
	chunks := SplitTextIntoChunks(text, 12, 0)
	if len(chunks) < 3 {
		t.Fatalf("len(chunks) = %d, want sentence-aligned chunks: %+v", len(chunks), chunks)
	}
	for i := 0; i < len(chunks)-1; i++ {
		last := []rune(chunks[i].Content)
		if len(last) == 0 || last[len(last)-1] != '。' {
			t.Fatalf("chunk[%d] = %q, want semantic sentence boundary", i, chunks[i].Content)
		}
	}
	joined := ""
	for _, chunk := range chunks {
		joined += chunk.Content
	}
	if joined != text {
		t.Fatalf("joined chunks = %q, want %q", joined, text)
	}
}

func TestSplitTextIntoChunksOverlapsWholeSentencesWithoutFragmentedStarts(t *testing.T) {
	chunks := SplitTextIntoChunks("甲甲甲。乙乙乙。丙丙丙。丁丁丁。", 8, 4)
	want := []string{
		"甲甲甲。乙乙乙。",
		"乙乙乙。丙丙丙。",
		"丙丙丙。丁丁丁。",
	}
	if len(chunks) != len(want) {
		t.Fatalf("len(chunks) = %d, want %d: %+v", len(chunks), len(want), chunks)
	}
	for i, chunk := range chunks {
		if chunk.Index != i {
			t.Fatalf("chunk[%d].Index = %d, want %d", i, chunk.Index, i)
		}
		if chunk.Content != want[i] {
			t.Fatalf("chunk[%d].Content = %q, want %q", i, chunk.Content, want[i])
		}
	}
}

func TestSplitTextIntoChunksRecursivelySplitsOversizedSentenceAtClauseBoundaries(t *testing.T) {
	chunks := SplitTextIntoChunks("甲甲甲甲，乙乙乙乙，丙丙丙丙。", 6, 0)
	want := []string{"甲甲甲甲，", "乙乙乙乙，", "丙丙丙丙。"}
	if len(chunks) != len(want) {
		t.Fatalf("len(chunks) = %d, want %d: %+v", len(chunks), len(want), chunks)
	}
	for i, chunk := range chunks {
		if chunk.Content != want[i] {
			t.Fatalf("chunk[%d].Content = %q, want %q", i, chunk.Content, want[i])
		}
	}
}
