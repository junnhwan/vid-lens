package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/observability"
	"vid-lens/internal/repository"
)

const (
	maxRAGIndexErrorLen = 500
)

var ErrRAGIndexAlreadyBuilding = errors.New("索引正在构建中，请等待现有任务完成")

type ragIndexBuild struct {
	projectionID string
	sourceID     string
	sourceDigest string
	indexContext string
	service      *RAGIndexService
	userID       int64
	taskID       int64
	fileMD5      string
	modelName    string
	expectedDim  int
	startedAt    time.Time
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
	build.sourceID = task.ActiveTextSourceID
	if build.sourceID != "" {
		source, sourceErr := s.repos.TextSource.Read(ctx, userID, taskID, build.sourceID)
		if sourceErr != nil {
			return nil, sourceErr
		}
		build.sourceDigest = source.SourceDigest
	}
	build.indexContext, err = s.taskIndexContext(task)
	if err != nil {
		return nil, err
	}

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
	if err := build.complete(ctx, len(chunks), manifest); err != nil {
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
	if err := build.withSourceFence(ctx, nil); err != nil {
		return err
	}
	// Each build owns an immutable vector namespace. A remote write can finish
	// after this preflight and after a newer source completes; deleting the whole
	// task/model scope would erase that newer projection despite the final CAS.
	// Relational ChunkIDs select the published projection before retrieval TopK.
	if err := checkRAGBuildContext(ctx); err != nil {
		return err
	}
	return s.store.UpsertChunks(ctx, vectors)
}

func (s *RAGIndexService) loadTaskIndexChunks(userID int64, task *model.VideoTask) ([]TextChunk, error) {
	if task.UserID != userID {
		return nil, fmt.Errorf("无权访问此任务")
	}

	source, err := taskTextSource(context.Background(), s.repos, task)
	if err != nil {
		return nil, err
	}
	if s.store == nil {
		return nil, fmt.Errorf("向量数据库未启用")
	}

	chunks := make([]TextChunk, 0)
	if source.Snapshot != nil {
		chunks = append(chunks, SplitObservationsIntoChunks(source.Observations, s.cfg.ChunkSize, s.cfg.ChunkOverlap)...)
	} else if source.Transcription != nil && strings.TrimSpace(source.Transcription.Content) != "" {
		chunks = append(chunks, buildTranscriptIndexChunks(source.Transcription.Content, source.LegacyChunks, s.cfg.ChunkSize, s.cfg.ChunkOverlap)...)
	}
	if source.Snapshot == nil {
		retainLegacyTiming(chunks, source.Observations)
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
		projectionID: uuid.NewString(),
		service:      s,
		userID:       userID,
		taskID:       taskID,
		fileMD5:      fileMD5,
		modelName:    profile.EmbeddingModel,
		expectedDim:  expectedDim,
		startedAt:    time.Now(),
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
	if sourceErr := b.withSourceFence(ctx, nil); errors.Is(sourceErr, repository.ErrRAGSourceChanged) {
		return nil, sourceErr
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

func (b *ragIndexBuild) complete(ctx context.Context, chunkCount int, manifest string) error {
	return b.withSourceFence(ctx, func(tx *repository.Repositories) error {
		finishedAt := time.Now()
		ok, err := tx.RAGIndex.UpdateBuild(b.userID, b.taskID, b.modelName, b.startedAt, map[string]interface{}{
			"status": model.RAGIndexStatusIndexed, "chunk_count": chunkCount,
			"completed_chunks": chunkCount, "total_chunks": chunkCount,
			"build_phase": "completed", "wait_reason": "", "next_retry_at": nil,
			"chunk_manifest_sha256": manifest, "index_context_sha256": indexContextHash(b.indexContext), "finished_at": finishedAt,
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
	})
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
		vector, err := embedWithAdmissionProgress(ctx, embedding, (model.VideoChunk{IndexContext: b.indexContext, Content: chunk.Content}).EmbeddingText(), func(reason string, retryAt time.Time) error {
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
		vectorID := b.vectorID(chunk.Index, hash)
		sourceRefs, err := MarshalChunkSourceRefs(chunk.SourceRefs)
		if err != nil {
			return nil, nil, fmt.Errorf("encode chunk source refs: %w", err)
		}
		dbChunks = append(dbChunks, model.VideoChunk{
			UserID:         b.userID,
			TaskID:         b.taskID,
			ChunkIndex:     chunk.Index,
			Content:        chunk.Content,
			IndexContext:   b.indexContext,
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

func (b *ragIndexBuild) vectorID(index int, contentHash string) string {
	// The nonce isolates concurrent/stale builds even for identical wording.
	// Source/model metadata additionally makes the namespace evidence-bound.
	envelope := fmt.Sprintf("rag-projection-v1:%d:%d:%s:%s:%s:%s:%d:%s", b.userID, b.taskID, b.modelName, b.projectionID, b.sourceID, b.sourceDigest, index, contentHash)
	sum := sha256.Sum256([]byte(envelope))
	return "projection_" + hex.EncodeToString(sum[:])
}

func (b *ragIndexBuild) withSourceFence(ctx context.Context, fn func(*repository.Repositories) error) error {
	return b.service.repos.RunWithRAGBuildSource(ctx, repository.RAGBuildSourceFence{UserID: b.userID, TaskID: b.taskID, EmbeddingModel: b.modelName, StartedAt: b.startedAt, MediaFingerprint: b.fileMD5, SourceID: b.sourceID, SourceDigest: b.sourceDigest}, fn)
}

func (b *ragIndexBuild) persistChunkSource(ctx context.Context, dbChunks []model.VideoChunk, vectors []RAGVector) (string, error) {
	if err := checkRAGBuildContext(ctx); err != nil {
		return "", err
	}
	var manifest string
	err := b.withSourceFence(ctx, func(tx *repository.Repositories) error {
		if err := tx.VideoChunk.ReplaceTaskChunks(b.taskID, b.modelName, dbChunks); err != nil {
			return err
		}
		if err := checkRAGBuildContext(ctx); err != nil {
			return err
		}
		stored, err := tx.VideoChunk.ListByTaskID(b.userID, b.taskID, b.modelName)
		if err != nil {
			return err
		}
		manifest, err = ComputeChunkManifestSHA256(stored)
		if err != nil {
			return err
		}
		return attachStoredChunkIDs(vectors, stored)
	})
	return manifest, err
}

func indexContextHash(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:])
}
func (s *RAGIndexService) taskIndexContext(task *model.VideoTask) (string, error) {
	title := task.Title
	if title == "" {
		title = task.Filename
	}
	text := "视频：" + boundedVideoText(title, 200)
	if s.repos.SummaryRevision != nil {
		effective, err := s.repos.SummaryRevision.Effective(context.Background(), task.UserID, task.ID)
		if err != nil {
			return "", err
		}
		if effective.Content != "" {
			text += "\n概要导航（非原文）：" + boundedVideoText(effective.Content, 600)
		}
	} else if s.repos.Summary != nil {
		summary, err := s.repos.Summary.FindByTaskID(task.ID)
		if err != nil {
			return "", err
		}
		if summary != nil {
			text += "\n概要导航（非原文）：" + boundedVideoText(summary.Content, 600)
		}
	}
	return text, nil
}
