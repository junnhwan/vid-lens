package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/observability"
)

const (
	maxRAGIndexErrorLen = 500
)

var ErrRAGIndexAlreadyBuilding = errors.New("索引正在构建中，请等待现有任务完成")

type ragIndexBuild struct {
	service     *RAGIndexService
	userID      int64
	taskID      int64
	fileMD5     string
	modelName   string
	expectedDim int
	startedAt   time.Time
}

func (s *RAGIndexService) BuildTaskIndex(ctx context.Context, userID, taskID int64, embedding ai.EmbeddingClient, profile ai.Profile) (*RAGIndexResult, error) {
	if err := checkRAGBuildContext(ctx); err != nil {
		return nil, err
	}
	task, err := s.repos.Task.FindByID(taskID)
	if err != nil {
		return nil, fmt.Errorf("任务不存在")
	}
	if task.UserID != userID {
		return nil, fmt.Errorf("无权访问此任务")
	}

	build := s.newRAGIndexBuild(userID, taskID, task.FileMD5, profile)
	if err := build.start(); err != nil {
		return nil, err
	}
	chunks, err := s.loadTaskIndexChunks(userID, task)
	if err != nil {
		return build.fail(ctx, err)
	}
	if err := build.setTotal(len(chunks)); err != nil {
		return build.fail(ctx, err)
	}
	if err := checkRAGBuildContext(ctx); err != nil {
		return build.fail(ctx, err)
	}

	dbChunks, vectors, err := build.embedChunks(ctx, embedding, profile, chunks)
	if err != nil {
		return build.fail(ctx, err)
	}
	if err := build.progress("writing", len(chunks), "", nil); err != nil {
		return build.fail(ctx, err)
	}
	manifest, err := build.persistChunkSource(ctx, dbChunks, vectors)
	if err != nil {
		return build.fail(ctx, err)
	}
	if err := checkRAGBuildContext(ctx); err != nil {
		return nil, err
	}
	if err := s.replaceVectorProjection(ctx, build, vectors); err != nil {
		return build.fail(ctx, err)
	}
	if err := checkRAGBuildContext(ctx); err != nil {
		return nil, err
	}
	if err := build.complete(len(chunks), manifest); err != nil {
		return nil, err
	}

	return &RAGIndexResult{
		TaskID:         taskID,
		Status:         model.RAGIndexStatusIndexed,
		Indexed:        true,
		Chunks:         len(chunks),
		EmbeddingModel: build.modelName,
	}, nil
}

func (s *RAGIndexService) replaceVectorProjection(ctx context.Context, build *ragIndexBuild, vectors []RAGVector) error {
	if replacer, ok := s.store.(RAGVectorReplacer); ok {
		return replacer.ReplaceTaskChunks(ctx, build.userID, build.taskID, build.modelName, vectors)
	}

	// Fallback path for stores that do not implement the atomic
	// delete-and-replace operation. Keep the weaker behavior explicit.
	if err := s.store.DeleteTaskChunks(ctx, build.userID, build.taskID, build.modelName); err != nil {
		return err
	}
	if err := checkRAGBuildContext(ctx); err != nil {
		return err
	}
	return s.store.UpsertChunks(ctx, vectors)
}

func (s *RAGIndexService) loadTaskIndexChunks(userID int64, task *model.VideoTask) ([]TextChunk, error) {
	if task.UserID != userID {
		return nil, fmt.Errorf("无权访问此任务")
	}

	transcription, err := s.repos.Transcription.FindByTaskID(task.ID)
	if err != nil {
		return nil, err
	}
	if s.store == nil {
		return nil, fmt.Errorf("向量数据库未启用")
	}

	chunks := make([]TextChunk, 0)
	var transcriptionRows []model.VideoTranscriptionChunk
	if transcription != nil && strings.TrimSpace(transcription.Content) != "" && s.repos.TranscriptionChunk != nil {
		transcriptionRows, err = s.repos.TranscriptionChunk.ListByTaskID(task.ID)
		if err != nil {
			return nil, err
		}
	}
	if transcription != nil && strings.TrimSpace(transcription.Content) != "" {
		chunks = append(chunks, buildTranscriptIndexChunks(transcription.Content, transcriptionRows, s.cfg.ChunkSize, s.cfg.ChunkOverlap)...)
	}
	var visualLoadErr error
	if s.repos.VisualFrame != nil {
		if frames, err := s.repos.VisualFrame.ListCompletedWithText(task.ID); err == nil && len(frames) > 0 {
			chunks = append(chunks, formatOCRChunksForIndex(frames, s.cfg.ChunkSize)...)
		} else if err != nil {
			visualLoadErr = err
			if metrics := observability.DefaultMetrics(); metrics != nil {
				metrics.ObserveMultimodalEvidence("rag_index", "visual", "failed")
			}
		}
	}
	if len(chunks) == 0 {
		if metrics := observability.DefaultMetrics(); metrics != nil {
			metrics.ObserveMultimodalEvidence("rag_index", "none", "failed")
		}
		if visualLoadErr != nil {
			return nil, fmt.Errorf("读取视觉索引证据: %w", visualLoadErr)
		}
		return nil, fmt.Errorf("没有可索引的文本或视觉证据")
	}
	hasTranscript, hasVisual := false, false
	for i := range chunks {
		chunks[i].Index = i
		hasTranscript = hasTranscript || chunks[i].Modality == model.ChunkModalityTranscript
		hasVisual = hasVisual || chunks[i].Modality == model.ChunkModalityVisualOCR || chunks[i].Modality == model.ChunkModalityVisualCaption
	}
	if metrics := observability.DefaultMetrics(); metrics != nil {
		modality := "mixed"
		if hasTranscript && !hasVisual {
			modality = model.ChunkModalityTranscript
		} else if hasVisual && !hasTranscript {
			modality = "visual"
		}
		metrics.ObserveMultimodalEvidence("rag_index", modality, "success")
	}
	return chunks, nil
}

func (s *RAGIndexService) newRAGIndexBuild(userID, taskID int64, fileMD5 string, profile ai.Profile) *ragIndexBuild {
	expectedDim := profile.EmbeddingDim
	if expectedDim <= 0 {
		expectedDim = s.cfg.EmbeddingDim
	}
	return &ragIndexBuild{
		service:     s,
		userID:      userID,
		taskID:      taskID,
		fileMD5:     fileMD5,
		modelName:   profile.EmbeddingModel,
		expectedDim: expectedDim,
		startedAt:   time.Now(),
	}
}

func (b *ragIndexBuild) start() error {
	claimed, err := b.service.repos.RAGIndex.ClaimBuild(&model.VideoRAGIndex{
		UserID: b.userID, TaskID: b.taskID, FileMD5: b.fileMD5,
		EmbeddingModel: b.modelName, EmbeddingDim: b.expectedDim,
		Status:     model.RAGIndexStatusIndexing,
		BuildPhase: "preparing", BuildVersion: model.CurrentRAGIndexBuildVersion,
		StartedAt: &b.startedAt,
	})
	if err != nil {
		return err
	}
	if !claimed {
		return ErrRAGIndexAlreadyBuilding
	}
	return nil
}

func (b *ragIndexBuild) setTotal(total int) error {
	ok, err := b.service.repos.RAGIndex.UpdateBuild(b.userID, b.taskID, b.modelName, b.startedAt, map[string]interface{}{
		"total_chunks": total, "build_phase": "embedding",
	})
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("索引构建占用已失效")
	}
	return nil
}

func (b *ragIndexBuild) fail(ctx context.Context, cause error) (*RAGIndexResult, error) {
	if guardErr := checkRAGBuildContext(ctx); guardErr != nil {
		return nil, guardErr
	}
	finishedAt := time.Now()
	errMsg := cause.Error()
	if len(errMsg) > maxRAGIndexErrorLen {
		errMsg = errMsg[:maxRAGIndexErrorLen]
	}
	_, _ = b.service.repos.RAGIndex.UpdateBuild(b.userID, b.taskID, b.modelName, b.startedAt, map[string]interface{}{
		"status": model.RAGIndexStatusFailed, "build_phase": "failed", "wait_reason": "", "next_retry_at": nil,
		"last_error": errMsg, "finished_at": finishedAt,
	})
	return nil, cause
}

func (b *ragIndexBuild) complete(chunkCount int, manifest string) error {
	finishedAt := time.Now()
	ok, err := b.service.repos.RAGIndex.UpdateBuild(b.userID, b.taskID, b.modelName, b.startedAt, map[string]interface{}{
		"status": model.RAGIndexStatusIndexed, "chunk_count": chunkCount,
		"completed_chunks": chunkCount, "total_chunks": chunkCount,
		"build_phase": "completed", "wait_reason": "", "next_retry_at": nil,
		"chunk_manifest_sha256": manifest, "finished_at": finishedAt,
		"chunker_strategy": b.service.cfg.ChunkerStrategy, "chunker_version": b.service.cfg.ChunkerVersion,
		"chunk_size": b.service.cfg.ChunkSize, "chunk_overlap": b.service.cfg.ChunkOverlap,
		"source_mapping_version": model.CurrentRAGSourceMappingVersion,
		"build_version":          model.CurrentRAGIndexBuildVersion,
	})
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("索引构建占用已失效")
	}
	return nil
}

func (b *ragIndexBuild) progress(phase string, completed int, waitReason string, retryAt *time.Time) error {
	ok, err := b.service.repos.RAGIndex.UpdateBuild(b.userID, b.taskID, b.modelName, b.startedAt, map[string]interface{}{
		"build_phase": phase, "completed_chunks": completed,
		"wait_reason": waitReason, "next_retry_at": retryAt,
	})
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("索引构建占用已失效")
	}
	return nil
}

func (b *ragIndexBuild) embedChunks(ctx context.Context, embedding ai.EmbeddingClient, profile ai.Profile, chunks []TextChunk) ([]model.VideoChunk, []RAGVector, error) {
	embedding = ai.NewObservedEmbeddingClient(embedding, b.service.recorder, ai.CallContext{
		UserID:   b.userID,
		TaskID:   b.taskID,
		Provider: profile.EmbeddingProvider,
		Model:    b.modelName,
	})

	dbChunks := make([]model.VideoChunk, 0, len(chunks))
	vectors := make([]RAGVector, 0, len(chunks))
	for _, chunk := range chunks {
		if err := checkRAGBuildContext(ctx); err != nil {
			return nil, nil, err
		}
		if err := b.progress("embedding", len(dbChunks), "", nil); err != nil {
			return nil, nil, err
		}
		vector, err := embedWithAdmissionProgress(ctx, embedding, chunk.Content, func(reason string, retryAt time.Time) error {
			return b.progress("waiting", len(dbChunks), reason, &retryAt)
		})
		if err != nil {
			return nil, nil, err
		}
		if err := checkRAGBuildContext(ctx); err != nil {
			return nil, nil, err
		}
		if len(vector) != b.expectedDim {
			return nil, nil, fmt.Errorf("embedding 维度不匹配: 返回 %d，配置 %d", len(vector), b.expectedDim)
		}

		hash := md5Hex(chunk.Content)
		vectorID := ChunkEvidenceID(b.taskID, chunk.Index, hash)
		sourceRefs, err := MarshalChunkSourceRefs(chunk.SourceRefs)
		if err != nil {
			return nil, nil, fmt.Errorf("encode chunk source refs: %w", err)
		}
		dbChunks = append(dbChunks, model.VideoChunk{
			UserID:         b.userID,
			TaskID:         b.taskID,
			ChunkIndex:     chunk.Index,
			Content:        chunk.Content,
			ContentHash:    hash,
			TokenCount:     chunk.TokenCount,
			EmbeddingModel: b.modelName,
			EmbeddingDim:   b.expectedDim,
			VectorID:       vectorID,
			Modality:       chunk.Modality, StartMS: chunk.StartMS, EndMS: chunk.EndMS,
			TimeRangeStatus: chunk.TimeRangeStatus, SourceMappingStatus: chunk.SourceMappingStatus,
			SourceRefs: sourceRefs, ChunkerStrategy: b.service.cfg.ChunkerStrategy, ChunkerVersion: b.service.cfg.ChunkerVersion,
		})
		vectors = append(vectors, RAGVector{
			VectorID:       vectorID,
			UserID:         b.userID,
			TaskID:         b.taskID,
			ChunkIndex:     chunk.Index,
			ContentHash:    hash,
			Content:        chunk.Content,
			EmbeddingModel: b.modelName,
			Vector:         vector,
		})
		if err := b.progress("embedding", len(dbChunks), "", nil); err != nil {
			return nil, nil, err
		}
	}
	return dbChunks, vectors, nil
}

func (b *ragIndexBuild) persistChunkSource(ctx context.Context, dbChunks []model.VideoChunk, vectors []RAGVector) (string, error) {
	if err := checkRAGBuildContext(ctx); err != nil {
		return "", err
	}
	if err := b.service.repos.VideoChunk.ReplaceTaskChunks(b.taskID, b.modelName, dbChunks); err != nil {
		return "", err
	}
	if err := checkRAGBuildContext(ctx); err != nil {
		return "", err
	}

	stored, err := b.service.repos.VideoChunk.ListByTaskID(b.userID, b.taskID, b.modelName)
	if err != nil {
		return "", err
	}
	manifest, err := ComputeChunkManifestSHA256(stored)
	if err != nil {
		return "", err
	}
	if err := attachStoredChunkIDs(vectors, stored); err != nil {
		return "", err
	}
	return manifest, nil
}
