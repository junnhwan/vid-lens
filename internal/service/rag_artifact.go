package service

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"vid-lens/internal/model"
)

// RAGVectorManifestEntry is the non-vector metadata frozen from a vector
// backend for strict evaluation. EvidenceID is the logical vector_id.
type RAGVectorManifestEntry struct {
	EvidenceID     string
	UserID         int64
	TaskID         int64
	ChunkID        int64
	ChunkIndex     int
	ContentHash    string
	EmbeddingModel string
}

// ComputeChunkManifestSHA256 freezes the indexed chunk identity and content.
// Auto-increment database IDs and timestamps are deliberately excluded.
func ComputeChunkManifestSHA256(chunks []model.VideoChunk) (string, error) {
	type entry struct {
		UserID         int64  `json:"user_id"`
		TaskID         int64  `json:"task_id"`
		EmbeddingModel string `json:"embedding_model"`
		ChunkIndex     int    `json:"chunk_index"`
		VectorID       string `json:"vector_id"`
		ContentHash    string `json:"content_hash"`
		Content        string `json:"content"`
	}
	manifest := make([]entry, 0, len(chunks))
	for _, chunk := range chunks {
		manifest = append(manifest, entry{
			UserID: chunk.UserID, TaskID: chunk.TaskID, EmbeddingModel: chunk.EmbeddingModel,
			ChunkIndex: chunk.ChunkIndex, VectorID: chunk.VectorID, ContentHash: chunk.ContentHash, Content: chunk.Content,
		})
	}
	sort.Slice(manifest, func(i, j int) bool {
		if manifest[i].UserID != manifest[j].UserID {
			return manifest[i].UserID < manifest[j].UserID
		}
		if manifest[i].TaskID != manifest[j].TaskID {
			return manifest[i].TaskID < manifest[j].TaskID
		}
		if manifest[i].EmbeddingModel != manifest[j].EmbeddingModel {
			return manifest[i].EmbeddingModel < manifest[j].EmbeddingModel
		}
		if manifest[i].ChunkIndex != manifest[j].ChunkIndex {
			return manifest[i].ChunkIndex < manifest[j].ChunkIndex
		}
		return manifest[i].VectorID < manifest[j].VectorID
	})
	raw, err := json.Marshal(manifest)
	if err != nil {
		return "", fmt.Errorf("marshal chunk manifest: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// ChunkEvidenceID is the stable evidence identity shared by relational chunks,
// vector backends, and strict evaluation. It is deterministic for a task/chunk/content
// tuple and deliberately does not use the auto-increment video_chunks.id.
func ChunkEvidenceID(taskID int64, chunkIndex int, contentHash string) string {
	contentHash = strings.ToLower(strings.TrimSpace(contentHash))
	if len(contentHash) > 12 {
		contentHash = contentHash[:12]
	}
	return fmt.Sprintf("task_%d_%s_%d", taskID, contentHash, chunkIndex)
}

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// attachStoredChunkIDs binds the persisted relational chunk IDs to the vector projection.
// It validates every mapping before mutating vectors so a partial binding cannot
// be passed to a vector backend.
func attachStoredChunkIDs(vectors []RAGVector, stored []model.VideoChunk) error {
	chunkIDsByVectorID := make(map[string]int64, len(stored))
	for _, chunk := range stored {
		if chunk.ID > 0 {
			chunkIDsByVectorID[chunk.VectorID] = chunk.ID
		}
	}

	chunkIDs := make([]int64, len(vectors))
	for i, vector := range vectors {
		chunkID, ok := chunkIDsByVectorID[vector.VectorID]
		if !ok {
			return fmt.Errorf("persisted chunk ID missing for vector %q", vector.VectorID)
		}
		chunkIDs[i] = chunkID
	}
	for i := range vectors {
		vectors[i].ChunkID = chunkIDs[i]
	}
	return nil
}
