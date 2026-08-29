package ragtool

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/service"
)

type reindexSourceFake struct {
	chunks []model.VideoChunk
	calls  []int64
}

func (s *reindexSourceFake) ListForReindex(_ context.Context, afterID int64, limit int, userID, taskID int64, embeddingModel string) ([]model.VideoChunk, error) {
	s.calls = append(s.calls, afterID)
	out := make([]model.VideoChunk, 0, limit)
	for _, chunk := range s.chunks {
		if chunk.ID <= afterID || (userID > 0 && chunk.UserID != userID) || (taskID > 0 && chunk.TaskID != taskID) || (embeddingModel != "" && chunk.EmbeddingModel != embeddingModel) {
			continue
		}
		out = append(out, chunk)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

type reindexProfilesFake struct{ profile ai.Profile }

func (p reindexProfilesFake) GetDefaultAIProfile(int64) (*ai.Profile, error) {
	profile := p.profile
	return &profile, nil
}

type reindexEmbeddingFake struct {
	vectors [][]float32
	errs    []error
	calls   int
}

func (e *reindexEmbeddingFake) Embed(context.Context, string) ([]float32, error) {
	i := e.calls
	e.calls++
	if i < len(e.errs) && e.errs[i] != nil {
		return nil, e.errs[i]
	}
	if i >= len(e.vectors) {
		return []float32{1, 0, 0}, nil
	}
	return e.vectors[i], nil
}

type reindexFactoryFake struct{ client ai.EmbeddingClient }

func (f reindexFactoryFake) NewEmbeddingClient(ai.Profile) (ai.EmbeddingClient, error) {
	return f.client, nil
}

type reindexWriterFake struct{ vectors []service.RAGVector }

func (w *reindexWriterFake) UpsertChunks(_ context.Context, vectors []service.RAGVector) error {
	w.vectors = append(w.vectors, vectors...)
	return nil
}

func validReindexChunks() []model.VideoChunk {
	return []model.VideoChunk{
		{ID: 11, UserID: 7, TaskID: 8, ChunkIndex: 0, Content: "first", ContentHash: "h1", EmbeddingModel: "embed", EmbeddingDim: 3, VectorID: "v1"},
		{ID: 12, UserID: 7, TaskID: 8, ChunkIndex: 1, Content: "second", ContentHash: "h2", EmbeddingModel: "embed", EmbeddingDim: 3, VectorID: "v2"},
	}
}

func TestRAGReindexerRebuildsAndCheckpointsEachChunk(t *testing.T) {
	source := &reindexSourceFake{chunks: validReindexChunks()}
	client := &reindexEmbeddingFake{vectors: [][]float32{{1, 0, 0}, {0, 1, 0}}}
	writer := &reindexWriterFake{}
	var checkpoints []int64
	reindexer := NewRAGReindexer(source, reindexProfilesFake{profile: ai.Profile{EmbeddingModel: "embed", EmbeddingDim: 3}}, reindexFactoryFake{client: client}, writer)

	result, err := reindexer.Run(context.Background(), RAGReindexOptions{PageSize: 1, OnChunkComplete: func(chunk model.VideoChunk, _ int64) error {
		checkpoints = append(checkpoints, chunk.ID)
		return nil
	}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Processed != 2 || result.LastChunkID != 12 || len(writer.vectors) != 2 {
		t.Fatalf("result=%+v vectors=%+v", result, writer.vectors)
	}
	if writer.vectors[0].VectorID != "v1" || writer.vectors[0].ChunkID != 11 || writer.vectors[0].ContentHash != "h1" {
		t.Fatalf("first vector metadata drifted: %+v", writer.vectors[0])
	}
	if len(checkpoints) != 2 || checkpoints[0] != 11 || checkpoints[1] != 12 {
		t.Fatalf("checkpoints=%v", checkpoints)
	}
}

func TestRAGReindexerDryRunDoesNotNeedAIOrDestination(t *testing.T) {
	source := &reindexSourceFake{chunks: validReindexChunks()}
	result, err := NewRAGReindexer(source, nil, nil, nil).Run(context.Background(), RAGReindexOptions{DryRun: true, PageSize: 10, AfterChunkID: 10})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Candidates != 2 || result.Processed != 0 || result.LastChunkID != 12 {
		t.Fatalf("result=%+v", result)
	}
}

func TestRAGReindexerDryRunRejectsPGVectorDestinationDimensionDrift(t *testing.T) {
	source := &reindexSourceFake{chunks: validReindexChunks()[:1]}
	result, err := NewRAGReindexer(source, nil, nil, nil).Run(context.Background(), RAGReindexOptions{
		DryRun: true, PageSize: 10, DestinationDimension: 4,
	})
	if err == nil || !strings.Contains(err.Error(), "chunk 11 dimension 3 differs from pgvector destination dimension 4") {
		t.Fatalf("Run() result=%+v error=%v, want destination dimension drift", result, err)
	}
}

func TestRAGReindexerRejectsProfileModelDrift(t *testing.T) {
	source := &reindexSourceFake{chunks: validReindexChunks()[:1]}
	writer := &reindexWriterFake{}
	reindexer := NewRAGReindexer(source, reindexProfilesFake{profile: ai.Profile{EmbeddingModel: "different", EmbeddingDim: 3}}, reindexFactoryFake{client: &reindexEmbeddingFake{}}, writer)
	_, err := reindexer.Run(context.Background(), RAGReindexOptions{})
	if err == nil || len(writer.vectors) != 0 {
		t.Fatalf("Run() error=%v vectors=%v, want model drift failure", err, writer.vectors)
	}
}

func TestRAGReindexerRetriesEmbeddingWithoutAdvancingCheckpoint(t *testing.T) {
	source := &reindexSourceFake{chunks: validReindexChunks()[:1]}
	client := &reindexEmbeddingFake{errs: []error{errors.New("temporary")}, vectors: [][]float32{nil, {1, 0, 0}}}
	writer := &reindexWriterFake{}
	reindexer := NewRAGReindexer(source, reindexProfilesFake{profile: ai.Profile{EmbeddingModel: "embed", EmbeddingDim: 3}}, reindexFactoryFake{client: client}, writer)
	result, err := reindexer.Run(context.Background(), RAGReindexOptions{MaxRetries: 1})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if client.calls != 2 || result.Processed != 1 || result.LastChunkID != 11 {
		t.Fatalf("calls=%d result=%+v", client.calls, result)
	}
}
