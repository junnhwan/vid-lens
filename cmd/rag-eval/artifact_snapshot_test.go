package main

import (
	"context"
	"crypto/md5"
	"fmt"
	"strings"
	"testing"

	rageval "vid-lens/internal/eval"
	"vid-lens/internal/model"
	"vid-lens/internal/service"
)

type fakeSnapshotTasks struct{ task *model.VideoTask }

func (f fakeSnapshotTasks) FindByID(id int64) (*model.VideoTask, error) { return f.task, nil }

type fakeSnapshotTranscriptions struct{ transcription *model.VideoTranscription }

func (f fakeSnapshotTranscriptions) FindByTaskID(taskID int64) (*model.VideoTranscription, error) {
	return f.transcription, nil
}

type fakeSnapshotChunks struct{ chunks []model.VideoChunk }

func (f fakeSnapshotChunks) ListAllByTaskID(taskID int64) ([]model.VideoChunk, error) {
	return f.chunks, nil
}

type fakeSnapshotIndexes struct{ index *model.VideoRAGIndex }

func (f fakeSnapshotIndexes) FindByTaskAndModel(userID, taskID int64, embeddingModel string) (*model.VideoRAGIndex, error) {
	return f.index, nil
}

type fakeSnapshotVectors struct {
	entries []service.RAGVectorManifestEntry
}

func (f fakeSnapshotVectors) ListTaskVectorManifest(context.Context, int64, int64, string) ([]service.RAGVectorManifestEntry, error) {
	return f.entries, nil
}

func TestBuildLiveEvidenceSnapshotBindsTaskMD5AndStorageRows(t *testing.T) {
	content := "frozen transcript"
	hash := md5DigestForSnapshotTest(content)
	dataset := rageval.Dataset{Cases: []rageval.Case{
		{CaseID: "dev-1", Split: rageval.SplitDev, TaskID: 9, VideoID: "abcdefabcdefabcdefabcdefabcdefab"},
		{CaseID: "test-1", Split: rageval.SplitTest, TaskID: 10, VideoID: "sealed"},
	}}
	sources := liveEvidenceSources{
		tasks:          fakeSnapshotTasks{task: &model.VideoTask{ID: 9, UserID: 7, FileMD5: "ABCDEFABCDEFABCDEFABCDEFABCDEFAB"}},
		transcriptions: fakeSnapshotTranscriptions{transcription: &model.VideoTranscription{TaskID: 9, Content: content}},
		chunks:         fakeSnapshotChunks{chunks: []model.VideoChunk{{UserID: 7, TaskID: 9, ChunkIndex: 0, Content: content, ContentHash: hash, EmbeddingModel: "embed-v1", VectorID: "evidence-1"}}},
		indexes:        fakeSnapshotIndexes{index: &model.VideoRAGIndex{UserID: 7, TaskID: 9, EmbeddingModel: "embed-v1", Status: model.RAGIndexStatusIndexed, ChunkCount: 1, ChunkerStrategy: service.ChunkerStrategyFixedWindow, ChunkerVersion: service.FixedWindowChunkerVersion, ChunkSize: 800, ChunkOverlap: 100}},
		vectors:        fakeSnapshotVectors{entries: []service.RAGVectorManifestEntry{{UserID: 7, TaskID: 9, ChunkIndex: 0, ContentHash: hash, EmbeddingModel: "embed-v1", EvidenceID: "evidence-1"}}},
	}
	manifest, manifestErr := service.ComputeChunkManifestSHA256(sources.chunks.(fakeSnapshotChunks).chunks)
	if manifestErr != nil {
		t.Fatal(manifestErr)
	}
	sources.indexes.(fakeSnapshotIndexes).index.ChunkManifestSHA256 = manifest
	cfg := service.DefaultRAGRetrievalConfig()
	cfg.ChunkerStrategy = service.ChunkerStrategyFixedWindow
	cfg.ChunkerVersion = service.FixedWindowChunkerVersion

	splitDataset, snapshot, err := buildLiveEvidenceSnapshot(t.Context(), dataset, rageval.SplitDev, cfg, sources)
	if err != nil {
		t.Fatal(err)
	}
	if len(splitDataset.Cases) != 1 || splitDataset.Cases[0].CaseID != "dev-1" {
		t.Fatalf("split cases = %+v", splitDataset.Cases)
	}
	if len(snapshot.Corpus) != 1 || snapshot.Corpus[0].VideoID != "abcdefabcdefabcdefabcdefabcdefab" || snapshot.Corpus[0].Content != content {
		t.Fatalf("corpus = %+v", snapshot.Corpus)
	}
	if snapshot.ChunkerStrategy != service.ChunkerStrategyFixedWindow || snapshot.ChunkerVersion != service.FixedWindowChunkerVersion || snapshot.ChunkSize != 800 || snapshot.ChunkOverlap != 100 {
		t.Fatalf("snapshot chunker provenance = %+v", snapshot)
	}
	if len(snapshot.Chunks) != 1 || len(snapshot.Vectors) != 1 || snapshot.Chunks[0].EvidenceID != snapshot.Vectors[0].EvidenceID {
		t.Fatalf("snapshot chunks=%+v vectors=%+v", snapshot.Chunks, snapshot.Vectors)
	}
}

func TestBuildLiveEvidenceSnapshotRejectsUnprovenancedIndex(t *testing.T) {
	content := "frozen transcript"
	hash := md5DigestForSnapshotTest(content)
	dataset := rageval.Dataset{Cases: []rageval.Case{{CaseID: "dev-1", Split: rageval.SplitDev, TaskID: 9, VideoID: "abcdefabcdefabcdefabcdefabcdefab"}}}
	sources := liveEvidenceSources{
		tasks:          fakeSnapshotTasks{task: &model.VideoTask{ID: 9, UserID: 7, FileMD5: "abcdefabcdefabcdefabcdefabcdefab"}},
		transcriptions: fakeSnapshotTranscriptions{transcription: &model.VideoTranscription{TaskID: 9, Content: content}},
		chunks:         fakeSnapshotChunks{chunks: []model.VideoChunk{{UserID: 7, TaskID: 9, ChunkIndex: 0, Content: content, ContentHash: hash, EmbeddingModel: "embed-v1", VectorID: "evidence-1"}}},
		indexes:        fakeSnapshotIndexes{index: &model.VideoRAGIndex{UserID: 7, TaskID: 9, EmbeddingModel: "embed-v1", Status: model.RAGIndexStatusIndexed, ChunkCount: 1}},
		vectors:        fakeSnapshotVectors{entries: []service.RAGVectorManifestEntry{{UserID: 7, TaskID: 9, ChunkIndex: 0, ContentHash: hash, EmbeddingModel: "embed-v1", EvidenceID: "evidence-1"}}},
	}
	_, _, err := buildLiveEvidenceSnapshot(t.Context(), dataset, rageval.SplitDev, service.DefaultRAGRetrievalConfig(), sources)
	if err == nil || !strings.Contains(err.Error(), "chunker provenance") {
		t.Fatalf("error = %v, want missing chunker provenance", err)
	}
}

func md5DigestForSnapshotTest(value string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(value)))
}
