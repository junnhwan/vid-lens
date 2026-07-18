package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/quota"
	"vid-lens/internal/repository"
)

type fakeEmbeddingClient struct {
	dim    int
	inputs []string
}

func (c *fakeEmbeddingClient) Embed(_ context.Context, input string) ([]float32, error) {
	c.inputs = append(c.inputs, input)
	vector := make([]float32, c.dim)
	for i := range vector {
		vector[i] = float32(i)
	}
	return vector, nil
}

type admissionThenEmbeddingClient struct {
	dim      int
	attempts int
}

func (c *admissionThenEmbeddingClient) Embed(_ context.Context, _ string) ([]float32, error) {
	c.attempts++
	if c.attempts == 1 {
		return nil, &ai.AdmissionError{Decision: quota.Decision{
			Allowed:    false,
			Scope:      "model",
			RetryAfter: time.Millisecond,
		}}
	}
	return make([]float32, c.dim), nil
}

type fakeVectorStore struct {
	upserts     []RAGVector
	deleteCalls []struct {
		userID int64
		taskID int64
		model  string
	}
	events    []string
	err       error
	deleteErr error
}

func (s *fakeVectorStore) DeleteTaskChunks(_ context.Context, userID, taskID int64, embeddingModel string) error {
	s.events = append(s.events, "delete")
	s.deleteCalls = append(s.deleteCalls, struct {
		userID int64
		taskID int64
		model  string
	}{userID: userID, taskID: taskID, model: embeddingModel})
	return s.deleteErr
}

func (s *fakeVectorStore) UpsertChunks(_ context.Context, vectors []RAGVector) error {
	s.events = append(s.events, "upsert")
	if s.err != nil {
		return s.err
	}
	s.upserts = append([]RAGVector(nil), vectors...)
	return nil
}

type fakeReplacingVectorStore struct {
	*fakeVectorStore
	replacements []RAGVector
	replaceCalls []struct {
		userID int64
		taskID int64
		model  string
	}
	replaceErr error
}

func (s *fakeReplacingVectorStore) ReplaceTaskChunks(_ context.Context, userID, taskID int64, embeddingModel string, vectors []RAGVector) error {
	s.events = append(s.events, "replace")
	s.replaceCalls = append(s.replaceCalls, struct {
		userID int64
		taskID int64
		model  string
	}{userID: userID, taskID: taskID, model: embeddingModel})
	if s.replaceErr != nil {
		return s.replaceErr
	}
	s.replacements = append([]RAGVector(nil), vectors...)
	return nil
}

func TestRAGIndexServiceBuildTaskIndexCreatesChunksAndVectors(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Filename: "video.mp4", FileURL: "videos/a.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{
		TaskID:  task.ID,
		Content: "abcdefghijklmnopqrstuvwxyz",
		Words:   26,
	}); err != nil {
		t.Fatalf("upsert transcription: %v", err)
	}

	embedding := &fakeEmbeddingClient{dim: 3}
	store := &fakeVectorStore{}
	svc := NewRAGIndexService(repos, store, RAGIndexConfig{
		ChunkSize:    10,
		ChunkOverlap: 2,
		EmbeddingDim: 3,
	})

	result, err := svc.BuildTaskIndex(context.Background(), 7, task.ID, embedding, ai.Profile{
		EmbeddingModel: "text-embedding-3-small",
		EmbeddingDim:   3,
	})
	if err != nil {
		t.Fatalf("BuildTaskIndex() error = %v", err)
	}
	if result.Chunks != 3 {
		t.Fatalf("result.Chunks = %d, want 3", result.Chunks)
	}
	if len(embedding.inputs) != 3 {
		t.Fatalf("embedding calls = %d, want 3", len(embedding.inputs))
	}
	if len(store.upserts) != 3 {
		t.Fatalf("vector upserts = %d, want 3", len(store.upserts))
	}

	chunks, err := repos.VideoChunk.ListByTaskID(7, task.ID, "text-embedding-3-small")
	if err != nil {
		t.Fatalf("ListByTaskID() error = %v", err)
	}
	if len(chunks) != 3 {
		t.Fatalf("stored chunks = %d, want 3", len(chunks))
	}
	if chunks[0].VectorID == "" || store.upserts[0].VectorID != chunks[0].VectorID {
		t.Fatalf("vector id mismatch: chunk=%q vector=%q", chunks[0].VectorID, store.upserts[0].VectorID)
	}
	if store.upserts[0].Content != chunks[0].Content {
		t.Fatalf("vector content = %q, want %q", store.upserts[0].Content, chunks[0].Content)
	}

	index, err := repos.RAGIndex.FindByTaskAndModel(7, task.ID, "text-embedding-3-small")
	if err != nil {
		t.Fatalf("find rag index: %v", err)
	}
	if index == nil {
		t.Fatal("expected rag index status row")
	}
	if index.Status != model.RAGIndexStatusIndexed || index.ChunkCount != 3 || index.EmbeddingDim != 3 {
		t.Fatalf("rag index = %+v, want indexed with 3 chunks and dim 3", index)
	}
	if index.ChunkerStrategy != ChunkerStrategyRecursiveSentence || index.ChunkerVersion != RecursiveSentenceChunkerVersion || index.ChunkSize != 10 || index.ChunkOverlap != 2 {
		t.Fatalf("rag index chunker provenance = %+v", index)
	}
	wantManifest, err := ComputeChunkManifestSHA256(chunks)
	if err != nil {
		t.Fatalf("ComputeChunkManifestSHA256() error = %v", err)
	}
	if index.ChunkManifestSHA256 != wantManifest {
		t.Fatalf("chunk manifest hash = %q, want %q", index.ChunkManifestSHA256, wantManifest)
	}
}

func TestRAGIndexServiceWaitsAndRetriesAdmissionRejection(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "59595959595959595959595959595959", Filename: "video.mp4", FileURL: "videos/admission-retry.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{
		TaskID:  task.ID,
		Content: "one complete sentence.",
		Words:   3,
	}); err != nil {
		t.Fatalf("upsert transcription: %v", err)
	}

	embedding := &admissionThenEmbeddingClient{dim: 3}
	svc := NewRAGIndexService(repos, &fakeVectorStore{}, RAGIndexConfig{ChunkSize: 100, EmbeddingDim: 3})
	result, err := svc.BuildTaskIndex(context.Background(), 7, task.ID, embedding, ai.Profile{
		EmbeddingModel: "text-embedding-3-small",
		EmbeddingDim:   3,
	})
	if err != nil {
		t.Fatalf("BuildTaskIndex() error = %v", err)
	}
	if embedding.attempts != 2 {
		t.Fatalf("embedding attempts = %d, want 2", embedding.attempts)
	}
	if result.Status != model.RAGIndexStatusIndexed || result.Chunks != 1 {
		t.Fatalf("result = %+v, want indexed with 1 chunk", result)
	}
}

func TestRAGIndexServiceRecordsEmbeddingCalls(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "58585858585858585858585858585858", Filename: "video.mp4", FileURL: "videos/rag-audit.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{
		TaskID:  task.ID,
		Content: "abcdefghijklmnopqrst",
		Words:   20,
	}); err != nil {
		t.Fatalf("upsert transcription: %v", err)
	}

	svc := NewRAGIndexService(repos, &fakeVectorStore{}, RAGIndexConfig{ChunkSize: 10, EmbeddingDim: 3})
	svc.SetAIRecorder(NewAIObserver(repos))
	if _, err := svc.BuildTaskIndex(context.Background(), 7, task.ID, &fakeEmbeddingClient{dim: 3}, ai.Profile{
		EmbeddingProvider: "openai_compatible",
		EmbeddingModel:    "text-embedding-3-small",
		EmbeddingDim:      3,
	}); err != nil {
		t.Fatalf("BuildTaskIndex() error = %v", err)
	}

	logs, err := repos.AICallLog.ListByUserID(7, 10)
	if err != nil {
		t.Fatalf("list ai call logs: %v", err)
	}
	if len(logs) == 0 {
		t.Fatal("expected embedding call logs")
	}
	for _, log := range logs {
		if log.Kind != model.AICallKindEmbedding || log.TaskID != task.ID || log.ModelName != "text-embedding-3-small" {
			t.Fatalf("log = %+v, want scoped embedding log", log)
		}
	}
}

func TestRAGIndexServiceDeletesOldVectorsBeforeReplacingChunks(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "56565656565656565656565656565656", Filename: "video.mp4", FileURL: "videos/rebuild.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "new transcript content for rebuild", Words: 5}); err != nil {
		t.Fatalf("upsert transcription: %v", err)
	}
	if err := repos.VideoChunk.ReplaceTaskChunks(task.ID, "text-embedding-3-small", []model.VideoChunk{
		{UserID: 7, TaskID: task.ID, ChunkIndex: 0, Content: "old stale chunk", ContentHash: "old", EmbeddingModel: "text-embedding-3-small", EmbeddingDim: 3, VectorID: "old-vector"},
	}); err != nil {
		t.Fatalf("seed old chunks: %v", err)
	}

	store := &fakeVectorStore{}
	svc := NewRAGIndexService(repos, store, RAGIndexConfig{ChunkSize: 12, EmbeddingDim: 3})
	if _, err := svc.BuildTaskIndex(context.Background(), 7, task.ID, &fakeEmbeddingClient{dim: 3}, ai.Profile{
		EmbeddingModel: "text-embedding-3-small",
		EmbeddingDim:   3,
	}); err != nil {
		t.Fatalf("BuildTaskIndex() error = %v", err)
	}

	if len(store.events) < 2 || store.events[0] != "delete" || store.events[1] != "upsert" {
		t.Fatalf("vector store events = %#v, want delete before upsert", store.events)
	}
	if len(store.deleteCalls) != 1 || store.deleteCalls[0].userID != 7 || store.deleteCalls[0].taskID != task.ID || store.deleteCalls[0].model != "text-embedding-3-small" {
		t.Fatalf("delete calls = %+v", store.deleteCalls)
	}
	chunks, err := repos.VideoChunk.ListByTaskID(7, task.ID, "text-embedding-3-small")
	if err != nil {
		t.Fatalf("list chunks: %v", err)
	}
	if len(chunks) == 0 || chunks[0].Content == "old stale chunk" {
		t.Fatalf("chunks were not replaced after delete succeeded: %+v", chunks)
	}
}

func TestRAGIndexServiceRecordsFailureAfterSourceReplacementWhenFallbackDeleteFails(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "57575757575757575757575757575757", Filename: "video.mp4", FileURL: "videos/rebuild-fail.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "new transcript should not replace old chunk", Words: 7}); err != nil {
		t.Fatalf("upsert transcription: %v", err)
	}
	if err := repos.VideoChunk.ReplaceTaskChunks(task.ID, "text-embedding-3-small", []model.VideoChunk{
		{UserID: 7, TaskID: task.ID, ChunkIndex: 0, Content: "old stale chunk", ContentHash: "old", EmbeddingModel: "text-embedding-3-small", EmbeddingDim: 3, VectorID: "old-vector"},
	}); err != nil {
		t.Fatalf("seed old chunks: %v", err)
	}

	store := &fakeVectorStore{deleteErr: fmt.Errorf("milvus delete failed")}
	svc := NewRAGIndexService(repos, store, RAGIndexConfig{ChunkSize: 12, EmbeddingDim: 3})
	_, err := svc.BuildTaskIndex(context.Background(), 7, task.ID, &fakeEmbeddingClient{dim: 3}, ai.Profile{
		EmbeddingModel: "text-embedding-3-small",
		EmbeddingDim:   3,
	})
	if err == nil {
		t.Fatal("BuildTaskIndex() succeeded, want delete failure")
	}
	if len(store.events) != 1 || store.events[0] != "delete" {
		t.Fatalf("events = %#v, want only delete", store.events)
	}
	chunks, listErr := repos.VideoChunk.ListByTaskID(7, task.ID, "text-embedding-3-small")
	if listErr != nil {
		t.Fatalf("list chunks: %v", listErr)
	}
	if len(chunks) == 0 || chunks[0].Content == "old stale chunk" {
		t.Fatalf("relational source should be replaced before the fallback projection failure: %+v", chunks)
	}
	index, findErr := repos.RAGIndex.FindByTaskAndModel(7, task.ID, "text-embedding-3-small")
	if findErr != nil {
		t.Fatalf("find rag index: %v", findErr)
	}
	if index == nil || index.Status != model.RAGIndexStatusFailed || index.LastError == "" {
		t.Fatalf("rag index should record delete failure: %+v", index)
	}
}

func TestRAGIndexServiceUsesAtomicVectorReplacerWhenAvailable(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "54545454545454545454545454545454", Filename: "video.mp4", FileURL: "videos/atomic-replace.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "atomic replacement content", Words: 3}); err != nil {
		t.Fatalf("upsert transcription: %v", err)
	}

	store := &fakeReplacingVectorStore{fakeVectorStore: &fakeVectorStore{}}
	svc := NewRAGIndexService(repos, store, RAGIndexConfig{ChunkSize: 100, EmbeddingDim: 3})
	if _, err := svc.BuildTaskIndex(context.Background(), 7, task.ID, &fakeEmbeddingClient{dim: 3}, ai.Profile{
		EmbeddingModel: "text-embedding-3-small",
		EmbeddingDim:   3,
	}); err != nil {
		t.Fatalf("BuildTaskIndex() error = %v", err)
	}

	if len(store.events) != 1 || store.events[0] != "replace" {
		t.Fatalf("events = %#v, want only atomic replace", store.events)
	}
	if len(store.deleteCalls) != 0 || len(store.upserts) != 0 {
		t.Fatalf("fallback path was called: deletes=%+v upserts=%+v", store.deleteCalls, store.upserts)
	}
	if len(store.replaceCalls) != 1 || store.replaceCalls[0].userID != 7 || store.replaceCalls[0].taskID != task.ID || store.replaceCalls[0].model != "text-embedding-3-small" {
		t.Fatalf("replace calls = %+v", store.replaceCalls)
	}
	if len(store.replacements) != 1 || store.replacements[0].ChunkID <= 0 {
		t.Fatalf("replacement vectors must reference persisted relational chunks: %+v", store.replacements)
	}
}

func TestRAGIndexServiceRecordsAtomicReplacementFailure(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "53535353535353535353535353535353", Filename: "video.mp4", FileURL: "videos/atomic-replace-fail.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "atomic replacement failure", Words: 3}); err != nil {
		t.Fatalf("upsert transcription: %v", err)
	}

	store := &fakeReplacingVectorStore{fakeVectorStore: &fakeVectorStore{}, replaceErr: fmt.Errorf("pgvector replace failed")}
	svc := NewRAGIndexService(repos, store, RAGIndexConfig{ChunkSize: 100, EmbeddingDim: 3})
	_, err := svc.BuildTaskIndex(context.Background(), 7, task.ID, &fakeEmbeddingClient{dim: 3}, ai.Profile{
		EmbeddingModel: "text-embedding-3-small",
		EmbeddingDim:   3,
	})
	if err == nil {
		t.Fatal("BuildTaskIndex() succeeded, want replacement failure")
	}
	if len(store.events) != 1 || store.events[0] != "replace" {
		t.Fatalf("events = %#v, want only atomic replace", store.events)
	}

	index, findErr := repos.RAGIndex.FindByTaskAndModel(7, task.ID, "text-embedding-3-small")
	if findErr != nil {
		t.Fatalf("find rag index: %v", findErr)
	}
	if index == nil || index.Status != model.RAGIndexStatusFailed || index.LastError == "" {
		t.Fatalf("rag index should record replacement failure: %+v", index)
	}
}

func TestRAGIndexServiceRejectsEmbeddingDimMismatch(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Filename: "video.mp4", FileURL: "videos/b.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "abcdefghij", Words: 10}); err != nil {
		t.Fatalf("upsert transcription: %v", err)
	}

	svc := NewRAGIndexService(repos, &fakeVectorStore{}, RAGIndexConfig{ChunkSize: 10, EmbeddingDim: 1536})
	_, err := svc.BuildTaskIndex(context.Background(), 7, task.ID, &fakeEmbeddingClient{dim: 3}, ai.Profile{
		EmbeddingModel: "text-embedding-3-small",
		EmbeddingDim:   1536,
	})
	if err == nil {
		t.Fatal("BuildTaskIndex() succeeded with mismatched embedding dim")
	}
}

func TestRAGIndexServiceRecordsFailedStatusWhenVectorStoreFails(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "cccccccccccccccccccccccccccccccc", Filename: "video.mp4", FileURL: "videos/c.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "abcdefghij", Words: 10}); err != nil {
		t.Fatalf("upsert transcription: %v", err)
	}

	svc := NewRAGIndexService(repos, &fakeVectorStore{err: fmt.Errorf("milvus service unavailable")}, RAGIndexConfig{ChunkSize: 10, EmbeddingDim: 3})
	_, err := svc.BuildTaskIndex(context.Background(), 7, task.ID, &fakeEmbeddingClient{dim: 3}, ai.Profile{
		EmbeddingModel: "text-embedding-3-small",
		EmbeddingDim:   3,
	})
	if err == nil {
		t.Fatal("BuildTaskIndex() succeeded, want vector store failure")
	}

	index, findErr := repos.RAGIndex.FindByTaskAndModel(7, task.ID, "text-embedding-3-small")
	if findErr != nil {
		t.Fatalf("find rag index: %v", findErr)
	}
	if index == nil {
		t.Fatal("expected failed rag index status row")
	}
	if index.Status != model.RAGIndexStatusFailed {
		t.Fatalf("rag index status = %q, want failed", index.Status)
	}
	if index.LastError == "" || index.ChunkCount != 0 {
		t.Fatalf("rag index failure details = %+v", index)
	}
}

func TestRAGIndexServiceRejectsProfileDimDifferentFromCollectionDim(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "99999999999999999999999999999999", Filename: "video.mp4", FileURL: "videos/g.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "abcdefghij", Words: 10}); err != nil {
		t.Fatalf("upsert transcription: %v", err)
	}

	svc := NewRAGIndexService(repos, &fakeVectorStore{}, RAGIndexConfig{ChunkSize: 10, EmbeddingDim: 1536})
	_, err := svc.BuildTaskIndex(context.Background(), 7, task.ID, &fakeEmbeddingClient{dim: 1024}, ai.Profile{
		EmbeddingModel: "custom-embedding",
		EmbeddingDim:   1024,
	})
	if err == nil {
		t.Fatal("BuildTaskIndex() succeeded with profile dim different from collection dim")
	}
}

func TestRAGIndexServiceRejectsMissingVectorStore(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "ffffffffffffffffffffffffffffffff", Filename: "video.mp4", FileURL: "videos/f.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "abcdefghij", Words: 10}); err != nil {
		t.Fatalf("upsert transcription: %v", err)
	}

	svc := NewRAGIndexService(repos, nil, RAGIndexConfig{ChunkSize: 10, EmbeddingDim: 3})
	_, err := svc.BuildTaskIndex(context.Background(), 7, task.ID, &fakeEmbeddingClient{dim: 3}, ai.Profile{
		EmbeddingModel: "text-embedding-3-small",
		EmbeddingDim:   3,
	})
	if err == nil {
		t.Fatal("BuildTaskIndex() succeeded without vector store")
	}
}

func TestRAGIndexServiceGetTaskIndexStatusUsesStoredChunks(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "12121212121212121212121212121212", Filename: "video.mp4", FileURL: "videos/status.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.VideoChunk.ReplaceTaskChunks(task.ID, "text-embedding-3-small", []model.VideoChunk{
		{UserID: 7, TaskID: task.ID, ChunkIndex: 0, Content: "chunk 0", ContentHash: "hash0", EmbeddingModel: "text-embedding-3-small", EmbeddingDim: 1536, VectorID: "vector-0"},
		{UserID: 7, TaskID: task.ID, ChunkIndex: 1, Content: "chunk 1", ContentHash: "hash1", EmbeddingModel: "text-embedding-3-small", EmbeddingDim: 1536, VectorID: "vector-1"},
	}); err != nil {
		t.Fatalf("replace chunks: %v", err)
	}

	svc := NewRAGIndexService(repos, nil, RAGIndexConfig{})
	status, err := svc.GetTaskIndexStatus(context.Background(), 7, task.ID, ai.Profile{
		EmbeddingModel: "text-embedding-3-small",
		EmbeddingDim:   1536,
	})
	if err != nil {
		t.Fatalf("GetTaskIndexStatus() error = %v", err)
	}
	if !status.Indexed || status.Chunks != 2 {
		t.Fatalf("status = %+v, want indexed with 2 chunks", status)
	}

	otherUserStatus, err := svc.GetTaskIndexStatus(context.Background(), 8, task.ID, ai.Profile{
		EmbeddingModel: "text-embedding-3-small",
		EmbeddingDim:   1536,
	})
	if err != nil {
		t.Fatalf("GetTaskIndexStatus(wrong user) error = %v", err)
	}
	if otherUserStatus.Indexed || otherUserStatus.Chunks != 0 {
		t.Fatalf("wrong user status = %+v, want not indexed", otherUserStatus)
	}
}

func TestRAGIndexServiceGetTaskIndexStatusUsesRAGIndexState(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "34343434343434343434343434343434", Filename: "video.mp4", FileURL: "videos/index-status.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.RAGIndex.Upsert(&model.VideoRAGIndex{
		UserID:         7,
		TaskID:         task.ID,
		EmbeddingModel: "text-embedding-3-small",
		EmbeddingDim:   1536,
		Status:         model.RAGIndexStatusFailed,
		ChunkCount:     0,
		LastError:      "embedding timeout",
		BuildVersion:   1,
	}); err != nil {
		t.Fatalf("upsert rag index: %v", err)
	}
	if err := repos.VideoChunk.ReplaceTaskChunks(task.ID, "text-embedding-3-small", []model.VideoChunk{
		{UserID: 7, TaskID: task.ID, ChunkIndex: 0, Content: "stale chunk", ContentHash: "hash0", EmbeddingModel: "text-embedding-3-small", EmbeddingDim: 1536, VectorID: "stale-vector"},
	}); err != nil {
		t.Fatalf("replace chunks: %v", err)
	}

	svc := NewRAGIndexService(repos, nil, RAGIndexConfig{})
	status, err := svc.GetTaskIndexStatus(context.Background(), 7, task.ID, ai.Profile{
		EmbeddingModel: "text-embedding-3-small",
		EmbeddingDim:   1536,
	})
	if err != nil {
		t.Fatalf("GetTaskIndexStatus() error = %v", err)
	}
	if status.Indexed || status.Status != model.RAGIndexStatusFailed || status.LastError != "embedding timeout" {
		t.Fatalf("status = %+v, want failed from rag index table", status)
	}

	otherUserStatus, err := svc.GetTaskIndexStatus(context.Background(), 8, task.ID, ai.Profile{
		EmbeddingModel: "text-embedding-3-small",
		EmbeddingDim:   1536,
	})
	if err != nil {
		t.Fatalf("GetTaskIndexStatus(wrong user) error = %v", err)
	}
	if otherUserStatus.Status == model.RAGIndexStatusFailed || otherUserStatus.Indexed {
		t.Fatalf("wrong user status leaked index row: %+v", otherUserStatus)
	}
}

func newRAGIndexTestRepositories(t *testing.T) *repository.Repositories {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	return repository.NewRepositories(db)
}

func TestChunkEvidenceIDIsStableAndContentBound(t *testing.T) {
	first := ChunkEvidenceID(9, 3, md5Hex("same content"))
	second := ChunkEvidenceID(9, 3, md5Hex("same content"))
	changed := ChunkEvidenceID(9, 3, md5Hex("changed content"))
	if first == "" || first != second || first == changed {
		t.Fatalf("ids first=%q second=%q changed=%q", first, second, changed)
	}
}
