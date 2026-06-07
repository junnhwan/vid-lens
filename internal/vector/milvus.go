package vector

import (
	"context"
	"fmt"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
	"vid-lens/internal/service"
)

const (
	fieldVectorID       = "vector_id"
	fieldUserID         = "user_id"
	fieldTaskID         = "task_id"
	fieldChunkID        = "chunk_id"
	fieldChunkIndex     = "chunk_index"
	fieldContentHash    = "content_hash"
	fieldEmbeddingModel = "embedding_model"
	fieldContent        = "content"
	fieldEmbedding      = "embedding"
)

type MilvusConfig struct {
	Address    string
	Username   string
	Password   string
	Token      string
	Database   string
	Collection string
	Dim        int
}

type MilvusStore struct {
	client     client.Client
	collection string
	dim        int
}

func NewMilvusStore(ctx context.Context, cfg MilvusConfig) (*MilvusStore, error) {
	if cfg.Collection == "" {
		cfg.Collection = "vidlens_video_chunks"
	}
	if cfg.Dim <= 0 {
		cfg.Dim = 1536
	}

	milvusClient, err := client.NewClient(ctx, client.Config{
		Address:  cfg.Address,
		Username: cfg.Username,
		Password: cfg.Password,
		APIKey:   cfg.Token,
		DBName:   cfg.Database,
	})
	if err != nil {
		return nil, err
	}

	store := &MilvusStore{client: milvusClient, collection: cfg.Collection, dim: cfg.Dim}
	if err := store.EnsureCollection(ctx); err != nil {
		_ = milvusClient.Close()
		return nil, err
	}
	return store, nil
}

func (s *MilvusStore) EnsureCollection(ctx context.Context) error {
	has, err := s.client.HasCollection(ctx, s.collection)
	if err != nil {
		return err
	}
	if has {
		return s.client.LoadCollection(ctx, s.collection, false)
	}

	schema := entity.NewSchema().
		WithName(s.collection).
		WithDescription("VidLens video transcript chunks").
		WithField(entity.NewField().WithName(fieldVectorID).WithDataType(entity.FieldTypeVarChar).WithMaxLength(100).WithIsPrimaryKey(true)).
		WithField(entity.NewField().WithName(fieldUserID).WithDataType(entity.FieldTypeInt64)).
		WithField(entity.NewField().WithName(fieldTaskID).WithDataType(entity.FieldTypeInt64)).
		WithField(entity.NewField().WithName(fieldChunkID).WithDataType(entity.FieldTypeInt64)).
		WithField(entity.NewField().WithName(fieldChunkIndex).WithDataType(entity.FieldTypeInt64)).
		WithField(entity.NewField().WithName(fieldContentHash).WithDataType(entity.FieldTypeVarChar).WithMaxLength(64)).
		WithField(entity.NewField().WithName(fieldEmbeddingModel).WithDataType(entity.FieldTypeVarChar).WithMaxLength(100)).
		WithField(entity.NewField().WithName(fieldContent).WithDataType(entity.FieldTypeVarChar).WithMaxLength(8192)).
		WithField(entity.NewField().WithName(fieldEmbedding).WithDataType(entity.FieldTypeFloatVector).WithDim(int64(s.dim)))

	if err := s.client.CreateCollection(ctx, schema, entity.DefaultShardNumber); err != nil {
		return err
	}
	idx, err := entity.NewIndexAUTOINDEX(entity.COSINE)
	if err != nil {
		return err
	}
	if err := s.client.CreateIndex(ctx, s.collection, fieldEmbedding, idx, false); err != nil {
		return err
	}
	return s.client.LoadCollection(ctx, s.collection, false)
}

func (s *MilvusStore) UpsertChunks(ctx context.Context, vectors []service.RAGVector) error {
	if len(vectors) == 0 {
		return nil
	}

	vectorIDs := make([]string, 0, len(vectors))
	userIDs := make([]int64, 0, len(vectors))
	taskIDs := make([]int64, 0, len(vectors))
	chunkIDs := make([]int64, 0, len(vectors))
	chunkIndexes := make([]int64, 0, len(vectors))
	contentHashes := make([]string, 0, len(vectors))
	embeddingModels := make([]string, 0, len(vectors))
	contents := make([]string, 0, len(vectors))
	embeddings := make([][]float32, 0, len(vectors))

	for _, v := range vectors {
		vectorIDs = append(vectorIDs, v.VectorID)
		userIDs = append(userIDs, v.UserID)
		taskIDs = append(taskIDs, v.TaskID)
		chunkIDs = append(chunkIDs, v.ChunkID)
		chunkIndexes = append(chunkIndexes, int64(v.ChunkIndex))
		contentHashes = append(contentHashes, v.ContentHash)
		embeddingModels = append(embeddingModels, v.EmbeddingModel)
		contents = append(contents, truncateForMilvus(v.Content, 8192))
		embeddings = append(embeddings, v.Vector)
	}

	_, err := s.client.Upsert(
		ctx,
		s.collection,
		"",
		entity.NewColumnVarChar(fieldVectorID, vectorIDs),
		entity.NewColumnInt64(fieldUserID, userIDs),
		entity.NewColumnInt64(fieldTaskID, taskIDs),
		entity.NewColumnInt64(fieldChunkID, chunkIDs),
		entity.NewColumnInt64(fieldChunkIndex, chunkIndexes),
		entity.NewColumnVarChar(fieldContentHash, contentHashes),
		entity.NewColumnVarChar(fieldEmbeddingModel, embeddingModels),
		entity.NewColumnVarChar(fieldContent, contents),
		entity.NewColumnFloatVector(fieldEmbedding, s.dim, embeddings),
	)
	if err != nil {
		return err
	}
	return s.client.Flush(ctx, s.collection, false)
}

func (s *MilvusStore) DeleteTaskChunks(ctx context.Context, userID, taskID int64, embeddingModel string) error {
	filter := fmt.Sprintf("%s == %d and %s == %d and %s == %q",
		fieldUserID, userID,
		fieldTaskID, taskID,
		fieldEmbeddingModel, embeddingModel,
	)
	if err := s.client.Delete(ctx, s.collection, "", filter); err != nil {
		return err
	}
	return s.client.Flush(ctx, s.collection, false)
}

func (s *MilvusStore) Search(ctx context.Context, query []float32, req service.RetrievalRequest) ([]service.RetrievedChunk, error) {
	if len(query) != s.dim {
		return nil, fmt.Errorf("query embedding dim = %d, want %d", len(query), s.dim)
	}
	topK := req.TopK
	if topK <= 0 {
		topK = 5
	}
	filter := fmt.Sprintf("%s == %d and %s == %d and %s == %q",
		fieldUserID, req.UserID,
		fieldTaskID, req.TaskID,
		fieldEmbeddingModel, req.EmbeddingModel,
	)
	searchParam, err := entity.NewIndexAUTOINDEXSearchParam(1)
	if err != nil {
		return nil, err
	}
	resultSets, err := s.client.Search(
		ctx,
		s.collection,
		nil,
		filter,
		[]string{fieldChunkID, fieldChunkIndex, fieldContent},
		[]entity.Vector{entity.FloatVector(query)},
		fieldEmbedding,
		entity.COSINE,
		topK,
		searchParam,
	)
	if err != nil {
		return nil, err
	}
	if len(resultSets) == 0 {
		return nil, nil
	}

	rs := resultSets[0]
	if rs.Err != nil {
		return nil, rs.Err
	}
	chunkIDCol := rs.Fields.GetColumn(fieldChunkID)
	chunkIndexCol := rs.Fields.GetColumn(fieldChunkIndex)
	contentCol := rs.Fields.GetColumn(fieldContent)
	if chunkIDCol == nil || chunkIndexCol == nil || contentCol == nil {
		return nil, fmt.Errorf("milvus search missing output fields")
	}

	results := make([]service.RetrievedChunk, 0, rs.ResultCount)
	for i := 0; i < rs.ResultCount; i++ {
		score := rs.Scores[i]
		if req.MinScore > 0 && score < req.MinScore {
			continue
		}
		chunkID, err := chunkIDCol.GetAsInt64(i)
		if err != nil {
			return nil, err
		}
		chunkIndex, err := chunkIndexCol.GetAsInt64(i)
		if err != nil {
			return nil, err
		}
		content, err := contentCol.GetAsString(i)
		if err != nil {
			return nil, err
		}
		results = append(results, service.RetrievedChunk{
			ChunkID:    chunkID,
			ChunkIndex: int(chunkIndex),
			Score:      score,
			Content:    content,
		})
	}
	return results, nil
}

func (s *MilvusStore) Close() error {
	if s.client == nil {
		return nil
	}
	return s.client.Close()
}

func truncateForMilvus(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes])
}
