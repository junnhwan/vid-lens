package eval

import (
	"crypto/md5"
	"fmt"
	"strings"
	"testing"
)

func TestFreezeEvidenceArtifactsBindsCanonicalLiveSnapshot(t *testing.T) {
	dataset, snapshot := validEvidenceSnapshot(t)
	bundle, err := FreezeEvidenceArtifacts(dataset, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for name, artifact := range map[string]FrozenArtifact{
		"corpus": bundle.Corpus, "chunks": bundle.Chunks, "vectors": bundle.Vectors,
	} {
		if err := ValidateSHA256Digest(name, artifact.SHA256); err != nil {
			t.Fatalf("%s digest: %v", name, err)
		}
		if len(artifact.CanonicalJSON) == 0 {
			t.Fatalf("%s canonical payload is empty", name)
		}
	}
	again, err := FreezeEvidenceArtifacts(dataset, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Corpus.SHA256 != again.Corpus.SHA256 || bundle.Chunks.SHA256 != again.Chunks.SHA256 || bundle.Vectors.SHA256 != again.Vectors.SHA256 {
		t.Fatal("same live snapshot did not produce deterministic artifact hashes")
	}
}

func TestFreezeEvidenceArtifactsBindsChunkWindowConfiguration(t *testing.T) {
	dataset, snapshot := validEvidenceSnapshot(t)
	baseline, err := FreezeEvidenceArtifacts(dataset, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.ChunkSize++
	candidate, err := FreezeEvidenceArtifacts(dataset, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Chunks.SHA256 == candidate.Chunks.SHA256 {
		t.Fatal("chunk artifact hash did not bind chunk size")
	}
}

func TestFreezeEvidenceArtifactsRejectsChunkContentHashDrift(t *testing.T) {
	dataset, snapshot := validEvidenceSnapshot(t)
	snapshot.Chunks[0].Content = "tampered after indexing"
	_, err := FreezeEvidenceArtifacts(dataset, snapshot)
	if err == nil || !strings.Contains(err.Error(), "content hash") {
		t.Fatalf("error = %v, want content hash drift", err)
	}
}

func TestFreezeEvidenceArtifactsRejectsTaskVideoAndVectorDrift(t *testing.T) {
	t.Run("case task video mismatch", func(t *testing.T) {
		dataset, snapshot := validEvidenceSnapshot(t)
		dataset.Cases[0].VideoID = "different-video"
		_, err := FreezeEvidenceArtifacts(dataset, snapshot)
		if err == nil || !strings.Contains(err.Error(), "task/video") {
			t.Fatalf("error = %v, want task/video mismatch", err)
		}
	})
	t.Run("vector projection missing relational evidence", func(t *testing.T) {
		dataset, snapshot := validEvidenceSnapshot(t)
		snapshot.Vectors = nil
		_, err := FreezeEvidenceArtifacts(dataset, snapshot)
		if err == nil || !strings.Contains(err.Error(), "vector manifest") {
			t.Fatalf("error = %v, want vector manifest drift", err)
		}
	})
	t.Run("dataset references unfrozen evidence", func(t *testing.T) {
		dataset, snapshot := validEvidenceSnapshot(t)
		dataset.Cases[0].EvidenceRanges[0].ContextIDs = []string{"unknown-evidence"}
		_, err := FreezeEvidenceArtifacts(dataset, snapshot)
		if err == nil || !strings.Contains(err.Error(), "unknown-evidence") {
			t.Fatalf("error = %v, want unknown evidence", err)
		}
	})
}

func validEvidenceSnapshot(t *testing.T) (Dataset, EvidenceSnapshot) {
	t.Helper()
	dataset := validStrictDataset(t)
	// Artifact freezing is split-scoped: a dev run must not load sealed test evidence.
	dataset.Cases = []Case{{
		CaseID: "rag-dev-001", VideoID: "video-1", SourceGroup: "series-dev", Split: SplitDev, TaskID: 1,
		Question: "What is frozen?", Answerable: true,
		EvidenceRanges: []EvidenceRange{{ID: "ev-1", GroupID: "g-1", ContextIDs: []string{"evidence-1"}, Source: EvidenceSourceASR, Relevance: 3}},
	}}
	return dataset, EvidenceSnapshot{
		SchemaVersion: "1", ChunkerStrategy: "semantic_boundary", ChunkerVersion: "semantic-v1", ChunkSize: 800, ChunkOverlap: 100,
		Corpus:  []CorpusSnapshotEntry{{VideoID: "video-1", TaskID: 1, Content: "frozen transcript"}},
		Chunks:  []ChunkSnapshotEntry{{VideoID: "video-1", UserID: 7, TaskID: 1, EmbeddingModel: "embed-v1", ChunkIndex: 0, EvidenceID: "evidence-1", ContentHash: md5Digest("frozen transcript"), Content: "frozen transcript"}},
		Vectors: []VectorSnapshotEntry{{VideoID: "video-1", UserID: 7, TaskID: 1, EmbeddingModel: "embed-v1", ChunkIndex: 0, EvidenceID: "evidence-1", ContentHash: md5Digest("frozen transcript")}},
	}
}

func md5Digest(value string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(value)))
}
