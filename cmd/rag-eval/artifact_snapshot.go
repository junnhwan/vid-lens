package main

import (
	"context"
	"fmt"
	"strings"

	rageval "vid-lens/internal/eval"
	"vid-lens/internal/model"
	"vid-lens/internal/service"
)

type snapshotTaskRepository interface {
	FindByID(int64) (*model.VideoTask, error)
}

type snapshotTranscriptionRepository interface {
	FindByTaskID(int64) (*model.VideoTranscription, error)
}

type snapshotChunkRepository interface {
	ListAllByTaskID(int64) ([]model.VideoChunk, error)
}

type snapshotIndexRepository interface {
	FindByTaskAndModel(userID, taskID int64, embeddingModel string) (*model.VideoRAGIndex, error)
}

type snapshotVectorStore interface {
	ListTaskVectorManifest(context.Context, int64, int64, string) ([]service.RAGVectorManifestEntry, error)
}

type liveEvidenceSources struct {
	tasks          snapshotTaskRepository
	transcriptions snapshotTranscriptionRepository
	chunks         snapshotChunkRepository
	indexes        snapshotIndexRepository
	vectors        snapshotVectorStore
}

type vectorManifestGroup struct {
	userID         int64
	taskID         int64
	embeddingModel string
	videoID        string
	chunks         []model.VideoChunk
}

func buildLiveEvidenceSnapshot(ctx context.Context, dataset rageval.Dataset, split rageval.Split, cfg service.RAGRetrievalConfig, sources liveEvidenceSources) (rageval.Dataset, rageval.EvidenceSnapshot, error) {
	if sources.tasks == nil || sources.transcriptions == nil || sources.chunks == nil || sources.indexes == nil || sources.vectors == nil {
		return rageval.Dataset{}, rageval.EvidenceSnapshot{}, fmt.Errorf("live evidence sources are incomplete")
	}
	splitDataset := dataset
	splitDataset.Cases = nil
	for _, c := range dataset.Cases {
		if c.Split == split {
			splitDataset.Cases = append(splitDataset.Cases, c)
		}
	}
	if len(splitDataset.Cases) == 0 {
		return rageval.Dataset{}, rageval.EvidenceSnapshot{}, fmt.Errorf("split %q has no evaluation cases", split)
	}

	snapshot := rageval.EvidenceSnapshot{SchemaVersion: "1"}
	seenTasks := make(map[int64]string)
	groups := make(map[string]vectorManifestGroup)
	for _, c := range splitDataset.Cases {
		if c.TaskID <= 0 {
			return rageval.Dataset{}, rageval.EvidenceSnapshot{}, fmt.Errorf("case %q missing task_id for strict execution", c.CaseID)
		}
		if previous, seen := seenTasks[c.TaskID]; seen {
			if previous != strings.ToLower(strings.TrimSpace(c.VideoID)) {
				return rageval.Dataset{}, rageval.EvidenceSnapshot{}, fmt.Errorf("case %q task/video mismatch", c.CaseID)
			}
			continue
		}
		task, err := sources.tasks.FindByID(c.TaskID)
		if err != nil {
			return rageval.Dataset{}, rageval.EvidenceSnapshot{}, fmt.Errorf("load task %d: %w", c.TaskID, err)
		}
		videoID := strings.ToLower(strings.TrimSpace(task.FileMD5))
		if videoID == "" || videoID != strings.ToLower(strings.TrimSpace(c.VideoID)) {
			return rageval.Dataset{}, rageval.EvidenceSnapshot{}, fmt.Errorf("case %q task/video mismatch: task %d file_md5=%q video_id=%q", c.CaseID, c.TaskID, task.FileMD5, c.VideoID)
		}
		seenTasks[c.TaskID] = videoID
		transcription, err := sources.transcriptions.FindByTaskID(c.TaskID)
		if err != nil {
			return rageval.Dataset{}, rageval.EvidenceSnapshot{}, fmt.Errorf("load transcription for task %d: %w", c.TaskID, err)
		}
		if transcription == nil {
			return rageval.Dataset{}, rageval.EvidenceSnapshot{}, fmt.Errorf("task %d has no transcription corpus", c.TaskID)
		}
		snapshot.Corpus = append(snapshot.Corpus, rageval.CorpusSnapshotEntry{VideoID: videoID, TaskID: c.TaskID, Content: transcription.Content})
		chunks, err := sources.chunks.ListAllByTaskID(c.TaskID)
		if err != nil {
			return rageval.Dataset{}, rageval.EvidenceSnapshot{}, fmt.Errorf("load chunks for task %d: %w", c.TaskID, err)
		}
		if len(chunks) == 0 {
			return rageval.Dataset{}, rageval.EvidenceSnapshot{}, fmt.Errorf("task %d has empty MySQL chunk manifest", c.TaskID)
		}
		for _, chunk := range chunks {
			if chunk.TaskID != c.TaskID || chunk.UserID != task.UserID {
				return rageval.Dataset{}, rageval.EvidenceSnapshot{}, fmt.Errorf("task %d chunk ownership drift", c.TaskID)
			}
			snapshot.Chunks = append(snapshot.Chunks, rageval.ChunkSnapshotEntry{
				VideoID: videoID, UserID: chunk.UserID, TaskID: chunk.TaskID,
				EmbeddingModel: chunk.EmbeddingModel, ChunkIndex: chunk.ChunkIndex,
				EvidenceID: chunk.VectorID, ContentHash: chunk.ContentHash, Content: chunk.Content,
			})
			key := fmt.Sprintf("%d/%d/%s", chunk.UserID, chunk.TaskID, chunk.EmbeddingModel)
			group := groups[key]
			group.userID, group.taskID, group.embeddingModel, group.videoID = chunk.UserID, chunk.TaskID, chunk.EmbeddingModel, videoID
			group.chunks = append(group.chunks, chunk)
			groups[key] = group
		}
	}
	for _, group := range groups {
		index, err := sources.indexes.FindByTaskAndModel(group.userID, group.taskID, group.embeddingModel)
		if err != nil {
			return rageval.Dataset{}, rageval.EvidenceSnapshot{}, fmt.Errorf("load RAG index provenance for task %d model %q: %w", group.taskID, group.embeddingModel, err)
		}
		if index == nil || index.Status != model.RAGIndexStatusIndexed || strings.TrimSpace(index.ChunkerStrategy) == "" || strings.TrimSpace(index.ChunkerVersion) == "" || index.ChunkSize <= 0 || index.ChunkOverlap < 0 || index.ChunkOverlap >= index.ChunkSize || len(strings.TrimSpace(index.ChunkManifestSHA256)) != 64 {
			return rageval.Dataset{}, rageval.EvidenceSnapshot{}, fmt.Errorf("task %d model %q is missing indexed chunker provenance; rebuild the index", group.taskID, group.embeddingModel)
		}
		if index.ChunkCount != len(group.chunks) {
			return rageval.Dataset{}, rageval.EvidenceSnapshot{}, fmt.Errorf("task %d model %q chunk count drift: index=%d mysql=%d", group.taskID, group.embeddingModel, index.ChunkCount, len(group.chunks))
		}
		manifest, err := service.ComputeChunkManifestSHA256(group.chunks)
		if err != nil {
			return rageval.Dataset{}, rageval.EvidenceSnapshot{}, err
		}
		if !strings.EqualFold(manifest, index.ChunkManifestSHA256) {
			return rageval.Dataset{}, rageval.EvidenceSnapshot{}, fmt.Errorf("task %d model %q chunk manifest drift: indexed=%s live=%s", group.taskID, group.embeddingModel, index.ChunkManifestSHA256, manifest)
		}
		if snapshot.ChunkerStrategy == "" {
			snapshot.ChunkerStrategy, snapshot.ChunkerVersion = index.ChunkerStrategy, index.ChunkerVersion
			snapshot.ChunkSize, snapshot.ChunkOverlap = index.ChunkSize, index.ChunkOverlap
		} else if snapshot.ChunkerStrategy != index.ChunkerStrategy || snapshot.ChunkerVersion != index.ChunkerVersion || snapshot.ChunkSize != index.ChunkSize || snapshot.ChunkOverlap != index.ChunkOverlap {
			return rageval.Dataset{}, rageval.EvidenceSnapshot{}, fmt.Errorf("strict split contains mixed chunker provenance")
		}
		if cfg.ChunkerStrategy != index.ChunkerStrategy || cfg.ChunkerVersion != index.ChunkerVersion || cfg.ChunkSize != index.ChunkSize || cfg.ChunkOverlap != index.ChunkOverlap {
			return rageval.Dataset{}, rageval.EvidenceSnapshot{}, fmt.Errorf("retrieval config chunker provenance does not match indexed artifacts")
		}

		vectors, err := sources.vectors.ListTaskVectorManifest(ctx, group.userID, group.taskID, group.embeddingModel)
		if err != nil {
			return rageval.Dataset{}, rageval.EvidenceSnapshot{}, fmt.Errorf("load Milvus vector manifest for task %d model %q: %w", group.taskID, group.embeddingModel, err)
		}
		for _, vector := range vectors {
			snapshot.Vectors = append(snapshot.Vectors, rageval.VectorSnapshotEntry{
				VideoID: group.videoID, UserID: vector.UserID, TaskID: vector.TaskID,
				EmbeddingModel: vector.EmbeddingModel, ChunkIndex: vector.ChunkIndex,
				EvidenceID: vector.EvidenceID, ContentHash: vector.ContentHash,
			})
		}
	}
	return splitDataset, snapshot, nil
}
