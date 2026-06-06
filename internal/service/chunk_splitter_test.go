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
	if len(chunks) != 4 {
		t.Fatalf("len(chunks) = %d, want 4: %+v", len(chunks), chunks)
	}

	want := []string{"abcdefghij", "ijklmnopqr", "qrstuvwxyz", "yz"}
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
