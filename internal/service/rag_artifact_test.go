package service

import (
	"strings"
	"testing"

	"vid-lens/internal/model"
)

func TestAttachStoredChunkIDsRejectsMissingPersistedChunk(t *testing.T) {
	vectors := []RAGVector{{VectorID: "vector-1"}}

	err := attachStoredChunkIDs(vectors, []model.VideoChunk{{ID: 7, VectorID: "other-vector"}})
	if err == nil {
		t.Fatal("attachStoredChunkIDs() succeeded, want missing chunk error")
	}
	if !strings.Contains(err.Error(), "vector-1") {
		t.Fatalf("attachStoredChunkIDs() error = %q, want missing vector ID", err)
	}
	if vectors[0].ChunkID != 0 {
		t.Fatalf("vector chunk ID = %d, want unchanged zero value", vectors[0].ChunkID)
	}
}
