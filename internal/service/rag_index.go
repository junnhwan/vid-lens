package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/processingguard"
	"vid-lens/internal/repository"
)

const (
	// Fixed-window provenance is retained for historical evaluation artifacts.
	ChunkerStrategyFixedWindow = "fixed_window"
	FixedWindowChunkerVersion  = "split-text-v1"

	ChunkerStrategyRecursiveSentence = "recursive_sentence"
	RecursiveSentenceChunkerVersion  = "recursive-sentence-v1"
)

type RAGIndexConfig struct {
	ChunkerStrategy string
	ChunkerVersion  string
	ChunkSize       int
	ChunkOverlap    int
	EmbeddingDim    int
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
	DeleteTaskChunks(ctx context.Context, userID, taskID int64, embeddingModel string) error
}

// RAGVectorReplacer is an optional stronger write path for stores that can
// atomically replace one task/model projection inside their own database.
// The relational chunk source and vector projection still use separate
// transactions, even when pgvector shares the same PostgreSQL database. This
// interface only narrows the failure window within the vector projection.
type RAGVectorReplacer interface {
	ReplaceTaskChunks(ctx context.Context, userID, taskID int64, embeddingModel string, vectors []RAGVector) error
}

type RAGIndexService struct {
	repos    *repository.Repositories
	store    RAGVectorStore
	recorder ai.CallRecorder
	cfg      RAGIndexConfig
}

type RAGIndexResult struct {
	TaskID         int64  `json:"task_id"`
	Status         string `json:"status"`
	Indexed        bool   `json:"indexed"`
	Chunks         int    `json:"chunks"`
	EmbeddingModel string `json:"embedding_model"`
	LastError      string `json:"last_error"`
}

func NewRAGIndexService(repos *repository.Repositories, store RAGVectorStore, cfg RAGIndexConfig) *RAGIndexService {
	if strings.TrimSpace(cfg.ChunkerStrategy) == "" {
		cfg.ChunkerStrategy = ChunkerStrategyRecursiveSentence
	}
	if strings.TrimSpace(cfg.ChunkerVersion) == "" {
		cfg.ChunkerVersion = RecursiveSentenceChunkerVersion
	}
	if cfg.ChunkSize <= 0 {
		cfg.ChunkSize = 800
	}
	if cfg.EmbeddingDim <= 0 {
		cfg.EmbeddingDim = 1536
	}
	return &RAGIndexService{repos: repos, store: store, cfg: cfg}
}

func embedWithAdmissionWait(ctx context.Context, embedding ai.EmbeddingClient, input string) ([]float32, error) {
	for {
		vector, err := embedding.Embed(ctx, input)
		if err == nil {
			return vector, nil
		}
		var admissionErr *ai.AdmissionError
		if !errors.As(err, &admissionErr) || admissionErr.Decision.RetryAfter <= 0 {
			return nil, err
		}
		timer := time.NewTimer(admissionErr.Decision.RetryAfter)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *RAGIndexService) SetAIRecorder(recorder ai.CallRecorder) {
	s.recorder = recorder
}

func (s *RAGIndexService) GetTaskIndexStatus(ctx context.Context, userID, taskID int64, profile ai.Profile) (*RAGIndexResult, error) {
	_ = ctx

	modelName := profile.EmbeddingModel
	if modelName == "" {
		return &RAGIndexResult{TaskID: taskID, Status: model.RAGIndexStatusNotIndexed, Indexed: false, Chunks: 0}, nil
	}

	index, err := s.repos.RAGIndex.FindByTaskAndModel(userID, taskID, modelName)
	if err != nil {
		return nil, err
	}
	if index != nil {
		return &RAGIndexResult{
			TaskID:         taskID,
			Status:         index.Status,
			Indexed:        index.Status == model.RAGIndexStatusIndexed,
			Chunks:         index.ChunkCount,
			EmbeddingModel: index.EmbeddingModel,
			LastError:      index.LastError,
		}, nil
	}

	chunks, err := s.repos.VideoChunk.ListByTaskID(userID, taskID, modelName)
	if err != nil {
		return nil, err
	}
	return &RAGIndexResult{
		TaskID:         taskID,
		Status:         statusFromChunks(len(chunks)),
		Indexed:        len(chunks) > 0,
		Chunks:         len(chunks),
		EmbeddingModel: modelName,
	}, nil
}

func checkRAGBuildContext(ctx context.Context) error {
	return processingguard.Check(ctx)
}

func statusFromChunks(chunks int) string {
	if chunks > 0 {
		return model.RAGIndexStatusIndexed
	}
	return model.RAGIndexStatusNotIndexed
}
