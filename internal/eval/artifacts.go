package eval

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// EvidenceSnapshot is the live evidence state used by one strict evaluation run.
// It is built from the transcription corpus, MySQL chunk rows, and Milvus rows.
type EvidenceSnapshot struct {
	SchemaVersion   string                `json:"schema_version"`
	ChunkerStrategy string                `json:"chunker_strategy"`
	ChunkerVersion  string                `json:"chunker_version"`
	ChunkSize       int                   `json:"chunk_size"`
	ChunkOverlap    int                   `json:"chunk_overlap"`
	Corpus          []CorpusSnapshotEntry `json:"corpus"`
	Chunks          []ChunkSnapshotEntry  `json:"chunks"`
	Vectors         []VectorSnapshotEntry `json:"vectors"`
}

type CorpusSnapshotEntry struct {
	VideoID string `json:"video_id"`
	TaskID  int64  `json:"task_id"`
	Content string `json:"content"`
}

type ChunkSnapshotEntry struct {
	VideoID        string `json:"video_id"`
	UserID         int64  `json:"user_id"`
	TaskID         int64  `json:"task_id"`
	EmbeddingModel string `json:"embedding_model"`
	ChunkIndex     int    `json:"chunk_index"`
	EvidenceID     string `json:"evidence_id"`
	ContentHash    string `json:"content_hash"`
	Content        string `json:"content"`
}

type VectorSnapshotEntry struct {
	VideoID        string `json:"video_id"`
	UserID         int64  `json:"user_id"`
	TaskID         int64  `json:"task_id"`
	EmbeddingModel string `json:"embedding_model"`
	ChunkIndex     int    `json:"chunk_index"`
	EvidenceID     string `json:"evidence_id"`
	ContentHash    string `json:"content_hash"`
}

type FrozenArtifact struct {
	SHA256        string `json:"sha256"`
	CanonicalJSON []byte `json:"-"`
}

type EvidenceArtifactBundle struct {
	Corpus  FrozenArtifact
	Chunks  FrozenArtifact
	Vectors FrozenArtifact
}

type corpusArtifactPayload struct {
	SchemaVersion string                `json:"schema_version"`
	Corpus        []CorpusSnapshotEntry `json:"corpus"`
}

type chunkArtifactPayload struct {
	SchemaVersion   string               `json:"schema_version"`
	ChunkerStrategy string               `json:"chunker_strategy"`
	ChunkerVersion  string               `json:"chunker_version"`
	ChunkSize       int                  `json:"chunk_size"`
	ChunkOverlap    int                  `json:"chunk_overlap"`
	Chunks          []ChunkSnapshotEntry `json:"chunks"`
}

type vectorArtifactPayload struct {
	SchemaVersion string                `json:"schema_version"`
	Vectors       []VectorSnapshotEntry `json:"vectors"`
}

// FreezeEvidenceArtifacts validates cross-store identity before hashing sorted,
// canonical JSON payloads. Callers must pass only the cases in the run split so
// a dev run never needs to read a sealed test split's live evidence.
func FreezeEvidenceArtifacts(dataset Dataset, snapshot EvidenceSnapshot) (EvidenceArtifactBundle, error) {
	normalizeEvidenceSnapshot(&snapshot)
	if err := validateEvidenceSnapshot(dataset, snapshot); err != nil {
		return EvidenceArtifactBundle{}, err
	}
	corpus, err := freezeCanonical(corpusArtifactPayload{SchemaVersion: snapshot.SchemaVersion, Corpus: snapshot.Corpus})
	if err != nil {
		return EvidenceArtifactBundle{}, fmt.Errorf("freeze corpus artifact: %w", err)
	}
	chunks, err := freezeCanonical(chunkArtifactPayload{
		SchemaVersion: snapshot.SchemaVersion, ChunkerStrategy: snapshot.ChunkerStrategy,
		ChunkerVersion: snapshot.ChunkerVersion, ChunkSize: snapshot.ChunkSize, ChunkOverlap: snapshot.ChunkOverlap, Chunks: snapshot.Chunks,
	})
	if err != nil {
		return EvidenceArtifactBundle{}, fmt.Errorf("freeze chunk manifest: %w", err)
	}
	vectors, err := freezeCanonical(vectorArtifactPayload{SchemaVersion: snapshot.SchemaVersion, Vectors: snapshot.Vectors})
	if err != nil {
		return EvidenceArtifactBundle{}, fmt.Errorf("freeze vector artifact: %w", err)
	}
	return EvidenceArtifactBundle{Corpus: corpus, Chunks: chunks, Vectors: vectors}, nil
}

func normalizeEvidenceSnapshot(snapshot *EvidenceSnapshot) {
	snapshot.SchemaVersion = strings.TrimSpace(snapshot.SchemaVersion)
	snapshot.ChunkerStrategy = strings.TrimSpace(snapshot.ChunkerStrategy)
	snapshot.ChunkerVersion = strings.TrimSpace(snapshot.ChunkerVersion)
	for i := range snapshot.Corpus {
		snapshot.Corpus[i].VideoID = normalizeVideoID(snapshot.Corpus[i].VideoID)
	}
	for i := range snapshot.Chunks {
		snapshot.Chunks[i].VideoID = normalizeVideoID(snapshot.Chunks[i].VideoID)
		snapshot.Chunks[i].EmbeddingModel = strings.TrimSpace(snapshot.Chunks[i].EmbeddingModel)
		snapshot.Chunks[i].EvidenceID = strings.TrimSpace(snapshot.Chunks[i].EvidenceID)
		snapshot.Chunks[i].ContentHash = strings.TrimSpace(snapshot.Chunks[i].ContentHash)
	}
	for i := range snapshot.Vectors {
		snapshot.Vectors[i].VideoID = normalizeVideoID(snapshot.Vectors[i].VideoID)
		snapshot.Vectors[i].EmbeddingModel = strings.TrimSpace(snapshot.Vectors[i].EmbeddingModel)
		snapshot.Vectors[i].EvidenceID = strings.TrimSpace(snapshot.Vectors[i].EvidenceID)
		snapshot.Vectors[i].ContentHash = strings.TrimSpace(snapshot.Vectors[i].ContentHash)
	}
	sort.Slice(snapshot.Corpus, func(i, j int) bool {
		if snapshot.Corpus[i].TaskID != snapshot.Corpus[j].TaskID {
			return snapshot.Corpus[i].TaskID < snapshot.Corpus[j].TaskID
		}
		return snapshot.Corpus[i].VideoID < snapshot.Corpus[j].VideoID
	})
	sort.Slice(snapshot.Chunks, func(i, j int) bool { return chunkEntryLess(snapshot.Chunks[i], snapshot.Chunks[j]) })
	sort.Slice(snapshot.Vectors, func(i, j int) bool { return vectorEntryLess(snapshot.Vectors[i], snapshot.Vectors[j]) })
}

func validateEvidenceSnapshot(dataset Dataset, snapshot EvidenceSnapshot) error {
	if snapshot.SchemaVersion == "" {
		return fmt.Errorf("evidence snapshot missing schema version")
	}
	if snapshot.ChunkerStrategy == "" || snapshot.ChunkerVersion == "" {
		return fmt.Errorf("evidence snapshot must freeze chunker strategy and version")
	}
	if snapshot.ChunkSize <= 0 || snapshot.ChunkOverlap < 0 || snapshot.ChunkOverlap >= snapshot.ChunkSize {
		return fmt.Errorf("evidence snapshot must freeze valid chunk size and overlap")
	}
	if len(snapshot.Corpus) == 0 {
		return fmt.Errorf("corpus artifact is empty")
	}
	if len(snapshot.Chunks) == 0 {
		return fmt.Errorf("chunk manifest is empty")
	}
	if len(snapshot.Vectors) == 0 {
		return fmt.Errorf("vector manifest is empty")
	}

	taskVideos := make(map[int64]string, len(snapshot.Corpus))
	for i, entry := range snapshot.Corpus {
		if entry.TaskID <= 0 || entry.VideoID == "" {
			return fmt.Errorf("corpus[%d] missing task/video identity", i)
		}
		if previous, ok := taskVideos[entry.TaskID]; ok {
			return fmt.Errorf("corpus task/video identity duplicated for task %d (%s, %s)", entry.TaskID, previous, entry.VideoID)
		}
		taskVideos[entry.TaskID] = entry.VideoID
	}

	chunksByEvidence := make(map[string]ChunkSnapshotEntry, len(snapshot.Chunks))
	chunkCoordinates := make(map[string]string, len(snapshot.Chunks))
	for i, chunk := range snapshot.Chunks {
		if err := validateChunkSnapshotEntry(i, chunk, taskVideos); err != nil {
			return err
		}
		if _, exists := chunksByEvidence[chunk.EvidenceID]; exists {
			return fmt.Errorf("duplicate chunk evidence_id %q", chunk.EvidenceID)
		}
		coordinate := evidenceCoordinate(chunk.UserID, chunk.TaskID, chunk.EmbeddingModel, chunk.ChunkIndex)
		if previous, exists := chunkCoordinates[coordinate]; exists {
			return fmt.Errorf("duplicate chunk coordinate %s for evidence %q and %q", coordinate, previous, chunk.EvidenceID)
		}
		chunksByEvidence[chunk.EvidenceID] = chunk
		chunkCoordinates[coordinate] = chunk.EvidenceID
	}

	seenVectors := make(map[string]bool, len(snapshot.Vectors))
	for i, vector := range snapshot.Vectors {
		if vector.EvidenceID == "" {
			return fmt.Errorf("vector manifest[%d] missing evidence_id", i)
		}
		if seenVectors[vector.EvidenceID] {
			return fmt.Errorf("duplicate vector evidence_id %q", vector.EvidenceID)
		}
		seenVectors[vector.EvidenceID] = true
		chunk, exists := chunksByEvidence[vector.EvidenceID]
		if !exists {
			return fmt.Errorf("vector manifest contains unknown evidence %q", vector.EvidenceID)
		}
		if vector.VideoID != chunk.VideoID || vector.UserID != chunk.UserID || vector.TaskID != chunk.TaskID ||
			vector.EmbeddingModel != chunk.EmbeddingModel || vector.ChunkIndex != chunk.ChunkIndex || vector.ContentHash != chunk.ContentHash {
			return fmt.Errorf("vector manifest drift for evidence %q", vector.EvidenceID)
		}
	}
	for evidenceID := range chunksByEvidence {
		if !seenVectors[evidenceID] {
			return fmt.Errorf("vector manifest missing MySQL evidence %q", evidenceID)
		}
	}

	matchedTasks := make(map[int64]bool)
	for _, c := range dataset.Cases {
		if c.TaskID <= 0 {
			continue
		}
		videoID, exists := taskVideos[c.TaskID]
		if !exists || normalizeVideoID(c.VideoID) != videoID {
			return fmt.Errorf("case %q task/video mismatch: task_id=%d video_id=%q", c.CaseID, c.TaskID, c.VideoID)
		}
		matchedTasks[c.TaskID] = true
		for _, evidence := range append(append([]EvidenceRange(nil), c.EvidenceRanges...), c.NegativeConfusers...) {
			for _, contextID := range evidence.ContextIDs {
				chunk, ok := chunksByEvidence[strings.TrimSpace(contextID)]
				if !ok || chunk.TaskID != c.TaskID || chunk.VideoID != videoID {
					return fmt.Errorf("case %q references unfrozen evidence %q", c.CaseID, contextID)
				}
			}
		}
	}
	for taskID := range taskVideos {
		if !matchedTasks[taskID] {
			return fmt.Errorf("frozen task/video %d/%s has no dataset case", taskID, taskVideos[taskID])
		}
	}
	return nil
}

func validateChunkSnapshotEntry(index int, chunk ChunkSnapshotEntry, taskVideos map[int64]string) error {
	if chunk.UserID <= 0 || chunk.TaskID <= 0 || chunk.ChunkIndex < 0 || chunk.EmbeddingModel == "" || chunk.EvidenceID == "" || chunk.ContentHash == "" {
		return fmt.Errorf("chunk manifest[%d] missing identity field", index)
	}
	if videoID, ok := taskVideos[chunk.TaskID]; !ok || chunk.VideoID != videoID {
		return fmt.Errorf("chunk manifest[%d] task/video mismatch", index)
	}
	contentSum := md5.Sum([]byte(chunk.Content))
	if chunk.ContentHash != hex.EncodeToString(contentSum[:]) {
		return fmt.Errorf("chunk manifest[%d] content hash does not match frozen content", index)
	}
	return nil
}

func freezeCanonical(value any) (FrozenArtifact, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return FrozenArtifact{}, err
	}
	sum := sha256.Sum256(raw)
	return FrozenArtifact{SHA256: hex.EncodeToString(sum[:]), CanonicalJSON: raw}, nil
}

func evidenceCoordinate(userID, taskID int64, model string, index int) string {
	return fmt.Sprintf("%d/%d/%s/%d", userID, taskID, model, index)
}

func normalizeVideoID(value string) string { return strings.ToLower(strings.TrimSpace(value)) }

func chunkEntryLess(a, b ChunkSnapshotEntry) bool {
	if a.TaskID != b.TaskID {
		return a.TaskID < b.TaskID
	}
	if a.UserID != b.UserID {
		return a.UserID < b.UserID
	}
	if a.EmbeddingModel != b.EmbeddingModel {
		return a.EmbeddingModel < b.EmbeddingModel
	}
	if a.ChunkIndex != b.ChunkIndex {
		return a.ChunkIndex < b.ChunkIndex
	}
	return a.EvidenceID < b.EvidenceID
}

func vectorEntryLess(a, b VectorSnapshotEntry) bool {
	if a.TaskID != b.TaskID {
		return a.TaskID < b.TaskID
	}
	if a.UserID != b.UserID {
		return a.UserID < b.UserID
	}
	if a.EmbeddingModel != b.EmbeddingModel {
		return a.EmbeddingModel < b.EmbeddingModel
	}
	if a.ChunkIndex != b.ChunkIndex {
		return a.ChunkIndex < b.ChunkIndex
	}
	return a.EvidenceID < b.EvidenceID
}
