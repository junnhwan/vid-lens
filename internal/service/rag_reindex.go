package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

type RAGReindexSource interface {
	ListForReindex(ctx context.Context, afterID int64, limit int, userID, taskID int64, embeddingModel string) ([]model.VideoChunk, error)
}

type RAGReindexProfileProvider interface {
	GetDefaultAIProfile(userID int64) (*ai.Profile, error)
}

type RAGReindexEmbeddingFactory interface {
	NewEmbeddingClient(profile ai.Profile) (ai.EmbeddingClient, error)
}

type RAGVectorWriter interface {
	UpsertChunks(context.Context, []RAGVector) error
}

type RAGReindexOptions struct {
	UserID               int64
	TaskID               int64
	EmbeddingModel       string
	DestinationDimension int
	AfterChunkID         int64
	PageSize             int
	DryRun               bool
	MaxRetries           int
	RetryBaseDelay       time.Duration
	OnChunkComplete      func(chunk model.VideoChunk, processed int64) error
}

type RAGReindexResult struct {
	Candidates  int64
	Processed   int64
	LastChunkID int64
}

// RAGReindexer rebuilds destination vectors from PostgreSQL video_chunks. Source
// rows are never modified or deleted, so Milvus remains a rollback path.
type RAGReindexer struct {
	source   RAGReindexSource
	profiles RAGReindexProfileProvider
	factory  RAGReindexEmbeddingFactory
	writer   RAGVectorWriter
}

func NewRAGReindexer(source RAGReindexSource, profiles RAGReindexProfileProvider, factory RAGReindexEmbeddingFactory, writer RAGVectorWriter) *RAGReindexer {
	return &RAGReindexer{source: source, profiles: profiles, factory: factory, writer: writer}
}

func (r *RAGReindexer) Run(ctx context.Context, opts RAGReindexOptions) (RAGReindexResult, error) {
	var result RAGReindexResult
	if r == nil || r.source == nil {
		return result, errors.New("RAG reindex source is required")
	}
	if !opts.DryRun && (r.profiles == nil || r.factory == nil || r.writer == nil) {
		return result, errors.New("RAG reindex profile provider, embedding factory, and writer are required")
	}
	if opts.AfterChunkID < 0 || opts.UserID < 0 || opts.TaskID < 0 {
		return result, errors.New("RAG reindex IDs cannot be negative")
	}
	if opts.PageSize <= 0 {
		opts.PageSize = 100
	}
	if opts.PageSize > 1000 {
		return result, fmt.Errorf("RAG reindex page size %d exceeds 1000", opts.PageSize)
	}
	if opts.MaxRetries < 0 {
		return result, errors.New("RAG reindex max retries cannot be negative")
	}
	opts.EmbeddingModel = strings.TrimSpace(opts.EmbeddingModel)
	result.LastChunkID = opts.AfterChunkID
	clients := make(map[string]ai.EmbeddingClient)

	for {
		chunks, err := r.source.ListForReindex(ctx, result.LastChunkID, opts.PageSize, opts.UserID, opts.TaskID, opts.EmbeddingModel)
		if err != nil {
			return result, fmt.Errorf("list chunks after id %d: %w", result.LastChunkID, err)
		}
		if len(chunks) == 0 {
			return result, nil
		}
		for _, chunk := range chunks {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			if chunk.ID <= result.LastChunkID {
				return result, fmt.Errorf("reindex source returned non-increasing chunk id %d after %d", chunk.ID, result.LastChunkID)
			}
			if err := validateReindexChunk(chunk); err != nil {
				return result, fmt.Errorf("validate chunk id %d: %w", chunk.ID, err)
			}
			if opts.DestinationDimension > 0 && chunk.EmbeddingDim != opts.DestinationDimension {
				return result, fmt.Errorf("chunk %d dimension %d differs from pgvector destination dimension %d", chunk.ID, chunk.EmbeddingDim, opts.DestinationDimension)
			}
			result.Candidates++
			if opts.DryRun {
				result.LastChunkID = chunk.ID
				continue
			}

			client, ok := clients[reindexClientKey(chunk)]
			if !ok {
				profile, err := r.profiles.GetDefaultAIProfile(chunk.UserID)
				if err != nil {
					return result, fmt.Errorf("load default AI profile for user %d: %w", chunk.UserID, err)
				}
				if profile == nil {
					return result, fmt.Errorf("load default AI profile for user %d: profile is nil", chunk.UserID)
				}
				if strings.TrimSpace(profile.EmbeddingModel) != chunk.EmbeddingModel {
					return result, fmt.Errorf("chunk %d model %q differs from user %d default profile model %q", chunk.ID, chunk.EmbeddingModel, chunk.UserID, profile.EmbeddingModel)
				}
				if profile.EmbeddingDim > 0 && profile.EmbeddingDim != chunk.EmbeddingDim {
					return result, fmt.Errorf("chunk %d dimension %d differs from user %d default profile dimension %d", chunk.ID, chunk.EmbeddingDim, chunk.UserID, profile.EmbeddingDim)
				}
				client, err = r.factory.NewEmbeddingClient(*profile)
				if err != nil {
					return result, fmt.Errorf("create embedding client for user %d model %q: %w", chunk.UserID, chunk.EmbeddingModel, err)
				}
				clients[reindexClientKey(chunk)] = client
			}

			embedding, err := embedForReindex(ctx, client, chunk.Content, opts.MaxRetries, opts.RetryBaseDelay)
			if err != nil {
				return result, fmt.Errorf("embed chunk id %d: %w", chunk.ID, err)
			}
			if len(embedding) != chunk.EmbeddingDim {
				return result, fmt.Errorf("chunk %d embedding dimension = %d, want %d", chunk.ID, len(embedding), chunk.EmbeddingDim)
			}
			if err := r.writer.UpsertChunks(ctx, []RAGVector{{
				VectorID: chunk.VectorID, UserID: chunk.UserID, TaskID: chunk.TaskID,
				ChunkID: chunk.ID, ChunkIndex: chunk.ChunkIndex, ContentHash: chunk.ContentHash,
				Content: chunk.Content, EmbeddingModel: chunk.EmbeddingModel, Vector: embedding,
			}}); err != nil {
				return result, fmt.Errorf("write chunk id %d vector: %w", chunk.ID, err)
			}
			result.Processed++
			result.LastChunkID = chunk.ID
			if opts.OnChunkComplete != nil {
				if err := opts.OnChunkComplete(chunk, result.Processed); err != nil {
					return result, fmt.Errorf("persist checkpoint after chunk id %d: %w", chunk.ID, err)
				}
			}
		}
	}
}

func validateReindexChunk(chunk model.VideoChunk) error {
	switch {
	case chunk.ID <= 0:
		return errors.New("persisted chunk ID must be positive")
	case chunk.UserID <= 0 || chunk.TaskID <= 0:
		return errors.New("user and task IDs must be positive")
	case strings.TrimSpace(chunk.VectorID) == "":
		return errors.New("vector ID is required")
	case strings.TrimSpace(chunk.ContentHash) == "":
		return errors.New("content hash is required")
	case strings.TrimSpace(chunk.Content) == "":
		return errors.New("content is required")
	case strings.TrimSpace(chunk.EmbeddingModel) == "":
		return errors.New("embedding model is required")
	case chunk.EmbeddingDim <= 0:
		return errors.New("embedding dimension must be positive")
	default:
		return nil
	}
}

func reindexClientKey(chunk model.VideoChunk) string {
	return fmt.Sprintf("%d/%s/%d", chunk.UserID, chunk.EmbeddingModel, chunk.EmbeddingDim)
}

func embedForReindex(ctx context.Context, client ai.EmbeddingClient, content string, maxRetries int, baseDelay time.Duration) ([]float32, error) {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		vector, err := embedWithAdmissionWait(ctx, client, content)
		if err == nil {
			return vector, nil
		}
		lastErr = err
		if attempt == maxRetries {
			break
		}
		delay := baseDelay
		for i := 0; i < attempt; i++ {
			delay *= 2
		}
		if delay <= 0 {
			continue
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, lastErr
}
