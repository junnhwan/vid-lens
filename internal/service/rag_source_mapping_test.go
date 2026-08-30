package service

import (
	"context"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

func TestRAGIndexBuildPersistsASRSourceMappingAcrossSemanticChunk(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "abababababababababababababababab", Filename: "mapped.mp4", FileURL: "videos/mapped.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	rows := []struct {
		content  string
		timeline repository.TranscriptionChunkTimeline
	}{
		{"第一句。共享边界", repository.TranscriptionChunkTimeline{SegmentKey: "segment-a", SegmenterVersion: "overlap_windows_v1", WindowStartMS: 0, WindowEndMS: 305000, CoreStartMS: 0, CoreEndMS: 300000}},
		{"共享边界。第二句。", repository.TranscriptionChunkTimeline{SegmentKey: "segment-b", SegmenterVersion: "overlap_windows_v1", WindowStartMS: 295000, WindowEndMS: 605000, CoreStartMS: 300000, CoreEndMS: 600000}},
	}
	for i, row := range rows {
		if err := repos.TranscriptionChunk.UpsertCompletedWithTimeline(task.ID, i, "audio", row.content, row.timeline); err != nil {
			t.Fatal(err)
		}
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "第一句。共享边界。第二句。", Words: 3}); err != nil {
		t.Fatal(err)
	}
	svc := NewRAGIndexService(repos, &fakeVectorStore{}, RAGIndexConfig{ChunkSize: 100, EmbeddingDim: 3})
	if _, err := svc.BuildTaskIndex(context.Background(), 7, task.ID, &fakeEmbeddingClient{dim: 3}, ai.Profile{EmbeddingModel: "embed", EmbeddingDim: 3}); err != nil {
		t.Fatal(err)
	}
	chunks, err := repos.VideoChunk.ListByTaskID(7, task.ID, "embed")
	if err != nil || len(chunks) != 1 {
		t.Fatalf("chunks=%+v err=%v", chunks, err)
	}
	chunk := chunks[0]
	refs, err := ParseChunkSourceRefs(chunk.SourceRefs)
	if err != nil {
		t.Fatal(err)
	}
	if chunk.Modality != model.ChunkModalityTranscript || chunk.SourceMappingStatus != model.ChunkSourceMapped || chunk.TimeRangeStatus != model.ChunkTimeRangeCoarse || chunk.StartMS != 0 || chunk.EndMS != 605000 || len(refs) != 2 || refs[0].StableID != "segment-a" || refs[1].StableID != "segment-b" {
		t.Fatalf("mapped chunk = %+v refs=%+v", chunk, refs)
	}
	index, _ := repos.RAGIndex.FindByTaskAndModel(7, task.ID, "embed")
	if index == nil || index.BuildVersion != model.CurrentRAGIndexBuildVersion || index.SourceMappingVersion != model.CurrentRAGSourceMappingVersion || index.ChunkerVersion != model.CurrentRAGChunkerVersion {
		t.Fatalf("index = %+v", index)
	}
}

func TestRAGIndexBuildDoesNotGuessWhenTranscriptReplayDiffers(t *testing.T) {
	rows := []model.VideoTranscriptionChunk{{ID: 1, ChunkIndex: 0, Status: model.TranscriptionChunkStatusCompleted, Content: "原始 observation", StartSecond: 10, EndSecond: 20}}
	chunks := buildTranscriptIndexChunks("人工修改后的 transcript", rows, 100, 0)
	if len(chunks) != 1 || chunks[0].SourceMappingStatus != model.ChunkSourceUnmapped || chunks[0].TimeRangeStatus != model.ChunkTimeRangeUnknown || len(chunks[0].SourceRefs) != 0 {
		t.Fatalf("fallback chunks = %+v", chunks)
	}
}

func TestChunkManifestChangesWhenProvenanceChanges(t *testing.T) {
	base := []model.VideoChunk{{UserID: 7, TaskID: 1, ChunkIndex: 0, VectorID: "v", ContentHash: "h", Content: "same", EmbeddingModel: "embed", Modality: model.ChunkModalityTranscript, SourceMappingStatus: model.ChunkSourceMapped, SourceRefs: `[{"stable_id":"a"}]`}}
	first, err := ComputeChunkManifestSHA256(base)
	if err != nil {
		t.Fatal(err)
	}
	base[0].SourceRefs = `[{"stable_id":"b"}]`
	second, err := ComputeChunkManifestSHA256(base)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("manifest ignored provenance change")
	}
}

func TestRAGIndexStatusMarksLegacySourceMappingForRebuild(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	if err := repos.RAGIndex.Upsert(&model.VideoRAGIndex{UserID: 7, TaskID: 9, FileMD5: "cccccccccccccccccccccccccccccccc", EmbeddingModel: "embed", EmbeddingDim: 3, Status: model.RAGIndexStatusIndexed, ChunkCount: 2, ChunkerVersion: "recursive-sentence-v1", BuildVersion: 1}); err != nil {
		t.Fatal(err)
	}
	svc := NewRAGIndexService(repos, &fakeVectorStore{}, RAGIndexConfig{EmbeddingDim: 3})
	status, err := svc.GetTaskIndexStatus(context.Background(), 7, 9, ai.Profile{EmbeddingModel: "embed"})
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != model.RAGIndexStatusNeedsRebuild || status.Indexed || !status.NeedsRebuild {
		t.Fatalf("status=%+v", status)
	}
}
