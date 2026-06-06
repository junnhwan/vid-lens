package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

type RAGIndexConfig struct {
	ChunkSize      int
	ChunkOverlap   int
	EmbeddingDim   int
	CollectionName string
}

type RAGVector struct {
	VectorID       string
	UserID         int64
	TaskID         int64
	ChunkID        int64
	ChunkIndex     int
	ContentHash    string
	Content        string
	EmbeddingModel string
	Vector         []float32
}

type RAGVectorStore interface {
	UpsertChunks(ctx context.Context, vectors []RAGVector) error
}

type RAGIndexService struct {
	repos *repository.Repositories
	store RAGVectorStore
	cfg   RAGIndexConfig
}

type RAGIndexResult struct {
	TaskID         int64  `json:"task_id"`
	Indexed        bool   `json:"indexed"`
	Chunks         int    `json:"chunks"`
	EmbeddingModel string `json:"embedding_model"`
}

func NewRAGIndexService(repos *repository.Repositories, store RAGVectorStore, cfg RAGIndexConfig) *RAGIndexService {
	if cfg.ChunkSize <= 0 {
		cfg.ChunkSize = 800
	}
	if cfg.EmbeddingDim <= 0 {
		cfg.EmbeddingDim = 1536
	}
	return &RAGIndexService{repos: repos, store: store, cfg: cfg}
}

func (s *RAGIndexService) BuildTaskIndex(ctx context.Context, userID, taskID int64, embedding ai.EmbeddingClient, profile ai.Profile) (*RAGIndexResult, error) {
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

	modelName := profile.EmbeddingModel
	expectedDim := profile.EmbeddingDim
	if expectedDim <= 0 {
		expectedDim = s.cfg.EmbeddingDim
	}
	if expectedDim != s.cfg.EmbeddingDim {
		return nil, fmt.Errorf("Embedding 维度必须等于系统配置 %d，当前配置 %d", s.cfg.EmbeddingDim, expectedDim)
	}

	dbChunks := make([]model.VideoChunk, 0, len(chunks))
	vectors := make([]RAGVector, 0, len(chunks))
	for _, chunk := range chunks {
		vector, err := embedding.Embed(ctx, chunk.Content)
		if err != nil {
			return nil, err
		}
		if len(vector) != expectedDim {
			return nil, fmt.Errorf("Embedding 维度不匹配: 返回 %d，配置 %d", len(vector), expectedDim)
		}

		hash := md5Hex(chunk.Content)
		vectorID := fmt.Sprintf("task_%d_%s_%d", taskID, hash[:12], chunk.Index)
		dbChunks = append(dbChunks, model.VideoChunk{
			UserID:         userID,
			TaskID:         taskID,
			ChunkIndex:     chunk.Index,
			Content:        chunk.Content,
			ContentHash:    hash,
			TokenCount:     len([]rune(chunk.Content)),
			EmbeddingModel: modelName,
			EmbeddingDim:   expectedDim,
			VectorID:       vectorID,
		})
		vectors = append(vectors, RAGVector{
			VectorID:       vectorID,
			UserID:         userID,
			TaskID:         taskID,
			ChunkIndex:     chunk.Index,
			ContentHash:    hash,
			Content:        chunk.Content,
			EmbeddingModel: modelName,
			Vector:         vector,
		})
	}

	if err := s.repos.VideoChunk.ReplaceTaskChunks(taskID, modelName, dbChunks); err != nil {
		return nil, err
	}

	stored, err := s.repos.VideoChunk.ListByTaskID(userID, taskID, modelName)
	if err != nil {
		return nil, err
	}
	chunkIDsByVectorID := make(map[string]int64, len(stored))
	for _, chunk := range stored {
		chunkIDsByVectorID[chunk.VectorID] = chunk.ID
	}
	for i := range vectors {
		vectors[i].ChunkID = chunkIDsByVectorID[vectors[i].VectorID]
	}

	if s.store != nil {
		if err := s.store.UpsertChunks(ctx, vectors); err != nil {
			return nil, err
		}
	}

	return &RAGIndexResult{
		TaskID:         taskID,
		Indexed:        true,
		Chunks:         len(chunks),
		EmbeddingModel: modelName,
	}, nil
}

func (s *RAGIndexService) GetTaskIndexStatus(ctx context.Context, userID, taskID int64, profile ai.Profile) (*RAGIndexResult, error) {
	_ = ctx

	modelName := profile.EmbeddingModel
	if modelName == "" {
		return &RAGIndexResult{TaskID: taskID, Indexed: false, Chunks: 0}, nil
	}

	chunks, err := s.repos.VideoChunk.ListByTaskID(userID, taskID, modelName)
	if err != nil {
		return nil, err
	}
	return &RAGIndexResult{
		TaskID:         taskID,
		Indexed:        len(chunks) > 0,
		Chunks:         len(chunks),
		EmbeddingModel: modelName,
	}, nil
}

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}
