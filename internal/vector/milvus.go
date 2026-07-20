package vector

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

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

func (s *MilvusStore) ListTaskVectorManifest(ctx context.Context, userID, taskID int64, embeddingModel string) ([]service.RAGVectorManifestEntry, error) {
	filter := fmt.Sprintf("%s == %d and %s == %d and %s == %q",
		fieldUserID, userID, fieldTaskID, taskID, fieldEmbeddingModel, embeddingModel)
	iterator, err := s.client.QueryIterator(ctx, client.NewQueryIteratorOption(s.collection).
		WithExpr(filter).
		WithOutputFields(fieldVectorID, fieldUserID, fieldTaskID, fieldChunkID, fieldChunkIndex, fieldContentHash, fieldEmbeddingModel).
		WithBatchSize(1000))
	if err != nil {
		return nil, err
	}
	entries := make([]service.RAGVectorManifestEntry, 0)
	for {
		result, nextErr := iterator.Next(ctx)
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return nil, nextErr
		}
		batch, parseErr := parseVectorManifestResultSet(result)
		if parseErr != nil {
			return nil, parseErr
		}
		entries = append(entries, batch...)
	}
	return entries, nil
}

func parseVectorManifestResultSet(result client.ResultSet) ([]service.RAGVectorManifestEntry, error) {
	columns := map[string]entity.Column{
		fieldVectorID: result.GetColumn(fieldVectorID), fieldUserID: result.GetColumn(fieldUserID),
		fieldTaskID: result.GetColumn(fieldTaskID), fieldChunkID: result.GetColumn(fieldChunkID),
		fieldChunkIndex: result.GetColumn(fieldChunkIndex), fieldContentHash: result.GetColumn(fieldContentHash),
		fieldEmbeddingModel: result.GetColumn(fieldEmbeddingModel),
	}
	for name, column := range columns {
		if column == nil {
			return nil, fmt.Errorf("milvus vector manifest missing output field %s", name)
		}
	}
	entries := make([]service.RAGVectorManifestEntry, 0, result.Len())
	for i := 0; i < result.Len(); i++ {
		evidenceID, err := columns[fieldVectorID].GetAsString(i)
		if err != nil {
			return nil, err
		}
		userID, err := columns[fieldUserID].GetAsInt64(i)
		if err != nil {
			return nil, err
		}
		taskID, err := columns[fieldTaskID].GetAsInt64(i)
		if err != nil {
			return nil, err
		}
		chunkID, err := columns[fieldChunkID].GetAsInt64(i)
		if err != nil {
			return nil, err
		}
		chunkIndex, err := columns[fieldChunkIndex].GetAsInt64(i)
		if err != nil {
			return nil, err
		}
		contentHash, err := columns[fieldContentHash].GetAsString(i)
		if err != nil {
			return nil, err
		}
		embeddingModel, err := columns[fieldEmbeddingModel].GetAsString(i)
		if err != nil {
			return nil, err
		}
		entries = append(entries, service.RAGVectorManifestEntry{
			EvidenceID: evidenceID, UserID: userID, TaskID: taskID, ChunkID: chunkID,
			ChunkIndex: int(chunkIndex), ContentHash: contentHash, EmbeddingModel: embeddingModel,
		})
	}
	return entries, nil
}

func (s *MilvusStore) Search(ctx context.Context, query []float32, req service.RetrievalRequest) ([]service.RetrievedChunk, error) {
	if len(query) != s.dim {
		return nil, fmt.Errorf("query embedding dim = %d, want %d", len(query), s.dim)
	}
	topK := req.TopK
	if topK <= 0 {
		topK = 5
	}
	taskIDs := req.TaskIDs
	if len(taskIDs) == 0 && req.TaskID > 0 {
		taskIDs = []int64{req.TaskID}
	}
	filter, _, err := buildMilvusSearchFilter(req.UserID, taskIDs, req.EmbeddingModel)
	if err != nil {
		return nil, err
	}
	searchParam, err := entity.NewIndexAUTOINDEXSearchParam(1)
	if err != nil {
		return nil, err
	}
	resultSets, err := s.client.Search(ctx, s.collection, nil, filter,
		[]string{fieldVectorID, fieldTaskID, fieldChunkID, fieldChunkIndex, fieldContent},
		[]entity.Vector{entity.FloatVector(query)}, fieldEmbedding, entity.COSINE, topK, searchParam)
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
	vectorIDCol := rs.Fields.GetColumn(fieldVectorID)
	taskIDCol := rs.Fields.GetColumn(fieldTaskID)
	chunkIDCol := rs.Fields.GetColumn(fieldChunkID)
	chunkIndexCol := rs.Fields.GetColumn(fieldChunkIndex)
	contentCol := rs.Fields.GetColumn(fieldContent)
	if vectorIDCol == nil || taskIDCol == nil || chunkIDCol == nil || chunkIndexCol == nil || contentCol == nil {
		return nil, fmt.Errorf("milvus search missing output fields")
	}
	results := make([]service.RetrievedChunk, 0, rs.ResultCount)
	for i := 0; i < rs.ResultCount; i++ {
		score := rs.Scores[i]
		if req.MinScore > 0 && score < req.MinScore {
			continue
		}
		vectorID, err := vectorIDCol.GetAsString(i)
		if err != nil {
			return nil, err
		}
		taskID, err := taskIDCol.GetAsInt64(i)
		if err != nil {
			return nil, err
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
		results = append(results, service.RetrievedChunk{TaskID: taskID, EvidenceID: vectorID, ChunkID: chunkID, ChunkIndex: int(chunkIndex), Score: score, Content: content})
	}
	return results, nil
}

func buildMilvusSearchFilter(userID int64, taskIDs []int64, embeddingModel string) (string, []int64, error) {
	ids := normalizeVectorTaskIDs(taskIDs)
	if len(ids) == 0 {
		return "", nil, fmt.Errorf("task_ids must not be empty")
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	filter := fmt.Sprintf("%s == %d and %s in [%s] and %s == %q", fieldUserID, userID, fieldTaskID, strings.Join(parts, ","), fieldEmbeddingModel, embeddingModel)
	return filter, ids, nil
}

// HealthCheck verifies the Milvus server is healthy without changing collection state.
func (s *MilvusStore) HealthCheck(ctx context.Context) error {
	if s.client == nil {
		return fmt.Errorf("milvus client is not initialized")
	}
	state, err := s.client.CheckHealth(ctx)
	if err != nil {
		return err
	}
	if state == nil || !state.IsHealthy {
		return fmt.Errorf("milvus is unhealthy")
	}
	return nil
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
