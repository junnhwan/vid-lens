package service

import (
	"context"
	"fmt"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

const (
	ragIndexBuildVersion = 1
	maxRAGIndexErrorLen  = 500
)

type ragIndexBuild struct {
	service     *RAGIndexService
	userID      int64
	taskID      int64
	modelName   string
	expectedDim int
	startedAt   time.Time
}

func (s *RAGIndexService) BuildTaskIndex(ctx context.Context, userID, taskID int64, embedding ai.EmbeddingClient, profile ai.Profile) (*RAGIndexResult, error) {
	if err := checkRAGBuildContext(ctx); err != nil {
		return nil, err
	}
	chunks, err := s.loadTaskIndexChunks(userID, taskID)
	if err != nil {
		return nil, err
	}

	build := s.newRAGIndexBuild(userID, taskID, profile)
	if err := build.start(); err != nil {
		return nil, err
	}
	if err := checkRAGBuildContext(ctx); err != nil {
		return nil, err
	}
	if build.expectedDim != s.cfg.EmbeddingDim {
		return build.fail(ctx, fmt.Errorf("embedding 维度必须等于系统配置 %d，当前配置 %d", s.cfg.EmbeddingDim, build.expectedDim))
	}

	if err := checkRAGBuildContext(ctx); err != nil {
		return nil, err
	}

	dbChunks, vectors, err := build.embedChunks(ctx, embedding, profile, chunks)
	if err != nil {
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

	// Compatibility path for stores such as Milvus that do not support an
	// atomic delete-and-replace operation. Keep the weaker behavior explicit.
	if err := s.store.DeleteTaskChunks(ctx, build.userID, build.taskID, build.modelName); err != nil {
		return err
	}
	if err := checkRAGBuildContext(ctx); err != nil {
		return err
	}
	return s.store.UpsertChunks(ctx, vectors)
}

func (s *RAGIndexService) loadTaskIndexChunks(userID, taskID int64) ([]TextChunk, error) {
	task, err := s.repos.Task.FindByID(taskID)
	if err != nil {
		return nil, fmt.Errorf("任务不存在")
	}
	if task.UserID != userID {
		return nil, fmt.Errorf("无权访问此任务")
	}

	transcription, err := s.repos.Transcription.FindByTaskID(taskID)
	if err != nil {
		return nil, err
	}
	if transcription == nil || transcription.Content == "" {
		return nil, fmt.Errorf("请先完成文字提取")
	}
	if s.store == nil {
		return nil, fmt.Errorf("向量数据库未启用")
	}

	chunks := SplitTextIntoChunks(transcription.Content, s.cfg.ChunkSize, s.cfg.ChunkOverlap)
	if len(chunks) == 0 {
		return nil, fmt.Errorf("没有可索引的转写文本")
	}
	return chunks, nil
}

func (s *RAGIndexService) newRAGIndexBuild(userID, taskID int64, profile ai.Profile) *ragIndexBuild {
	expectedDim := profile.EmbeddingDim
	if expectedDim <= 0 {
		expectedDim = s.cfg.EmbeddingDim
	}
	return &ragIndexBuild{
		service:     s,
		userID:      userID,
		taskID:      taskID,
		modelName:   profile.EmbeddingModel,
		expectedDim: expectedDim,
		startedAt:   time.Now(),
	}
}

func (b *ragIndexBuild) start() error {
	return b.writeStatus(model.RAGIndexStatusIndexing, 0, "", "", nil)
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
	_ = b.writeStatus(model.RAGIndexStatusFailed, 0, "", errMsg, &finishedAt)
	return nil, cause
}

func (b *ragIndexBuild) complete(chunkCount int, manifest string) error {
	finishedAt := time.Now()
	return b.writeStatus(model.RAGIndexStatusIndexed, chunkCount, manifest, "", &finishedAt)
}

func (b *ragIndexBuild) writeStatus(status string, chunkCount int, manifest, lastError string, finishedAt *time.Time) error {
	return b.service.repos.RAGIndex.Upsert(&model.VideoRAGIndex{
		UserID:              b.userID,
		TaskID:              b.taskID,
		EmbeddingModel:      b.modelName,
		EmbeddingDim:        b.expectedDim,
		Status:              status,
		ChunkCount:          chunkCount,
		ChunkerStrategy:     b.service.cfg.ChunkerStrategy,
		ChunkerVersion:      b.service.cfg.ChunkerVersion,
		ChunkSize:           b.service.cfg.ChunkSize,
		ChunkOverlap:        b.service.cfg.ChunkOverlap,
		ChunkManifestSHA256: manifest,
		LastError:           lastError,
		BuildVersion:        ragIndexBuildVersion,
		StartedAt:           &b.startedAt,
		FinishedAt:          finishedAt,
	})
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
		vector, err := embedWithAdmissionWait(ctx, embedding, chunk.Content)
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
		dbChunks = append(dbChunks, model.VideoChunk{
			UserID:         b.userID,
			TaskID:         b.taskID,
			ChunkIndex:     chunk.Index,
			Content:        chunk.Content,
			ContentHash:    hash,
			TokenCount:     len([]rune(chunk.Content)),
			EmbeddingModel: b.modelName,
			EmbeddingDim:   b.expectedDim,
			VectorID:       vectorID,
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
