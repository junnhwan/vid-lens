package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

func newUploadSessionTestService(t *testing.T, now time.Time) *UploadSessionService {
	t.Helper()
	repos := newMediaTestRepositories(t)
	svc := NewUploadSessionService(repos, nil, UploadSessionConfig{
		MaxFileSize:     100,
		MaxChunkSize:    5,
		SessionTTL:      time.Hour,
		CompletionLease: time.Minute,
		Now:             func() time.Time { return now },
	})
	return svc
}

func validUploadSessionRequest() CreateUploadSessionRequest {
	return CreateUploadSessionRequest{
		Filename:    "demo.mp4",
		FileSize:    11,
		ChunkSize:   5,
		TotalChunks: 3,
		ExpectedMD5: "0123456789abcdef0123456789abcdef",
	}
}

func TestCreateUploadSessionValidatesImmutableManifest(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	svc := newUploadSessionTestService(t, now)
	cases := []struct {
		name   string
		mutate func(*CreateUploadSessionRequest)
	}{
		{"missing filename", func(r *CreateUploadSessionRequest) { r.Filename = "" }},
		{"empty file", func(r *CreateUploadSessionRequest) { r.FileSize = 0 }},
		{"file too large", func(r *CreateUploadSessionRequest) { r.FileSize = 101; r.TotalChunks = 21 }},
		{"chunk too large", func(r *CreateUploadSessionRequest) { r.ChunkSize = 6; r.TotalChunks = 2 }},
		{"wrong count", func(r *CreateUploadSessionRequest) { r.TotalChunks = 2 }},
		{"invalid md5", func(r *CreateUploadSessionRequest) { r.ExpectedMD5 = "not-md5" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validUploadSessionRequest()
			tc.mutate(&req)
			if _, err := svc.Create(context.Background(), 7, req); !IsUploadSessionError(err, UploadSessionErrorInvalid) {
				t.Fatalf("Create() error = %v, want invalid upload session", err)
			}
		})
	}
}

func TestCreateUploadSessionResumesSameUserManifestAndReadsDatabaseProgress(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	repos := newMediaTestRepositories(t)
	svc := NewUploadSessionService(repos, nil, UploadSessionConfig{MaxFileSize: 100, MaxChunkSize: 5, SessionTTL: time.Hour, Now: func() time.Time { return now }})

	first, err := svc.Create(context.Background(), 7, validUploadSessionRequest())
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	if err := repos.UploadSession.CreateChunk(&model.UploadSessionChunk{
		SessionID: first.SessionID, ChunkIndex: 1, ActualSize: 5,
		ContentSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ObjectName:    "chunk-1",
	}); err != nil {
		t.Fatalf("seed chunk: %v", err)
	}

	resumed, err := svc.Create(context.Background(), 7, validUploadSessionRequest())
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if resumed.SessionID != first.SessionID {
		t.Fatalf("resumed id = %q, want %q", resumed.SessionID, first.SessionID)
	}
	if len(resumed.Uploaded) != 1 || resumed.Uploaded[0] != 1 {
		t.Fatalf("resumed progress = %v", resumed.Uploaded)
	}

	progress, err := svc.Get(context.Background(), 7, first.SessionID)
	if err != nil || len(progress.Uploaded) != 1 || progress.Uploaded[0] != 1 {
		t.Fatalf("database progress = %+v, %v", progress, err)
	}
}

func TestUploadSessionReadDoesNotLeakAnotherUsersSession(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	svc := newUploadSessionTestService(t, now)
	created, err := svc.Create(context.Background(), 7, validUploadSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(context.Background(), 8, created.SessionID); !IsUploadSessionError(err, UploadSessionErrorNotFound) {
		t.Fatalf("foreign Get() error = %v, want not found", err)
	}
}

func TestUploadSessionExpiryReleasesManifestForANewSession(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	clock := now
	repos := newMediaTestRepositories(t)
	svc := NewUploadSessionService(repos, nil, UploadSessionConfig{MaxFileSize: 100, MaxChunkSize: 5, SessionTTL: time.Hour, Now: func() time.Time { return clock }})
	first, err := svc.Create(context.Background(), 7, validUploadSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	clock = now.Add(2 * time.Hour)
	if _, err := svc.Get(context.Background(), 7, first.SessionID); !IsUploadSessionError(err, UploadSessionErrorExpired) {
		t.Fatalf("expired Get() error = %v", err)
	}
	second, err := svc.Create(context.Background(), 7, validUploadSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	if second.SessionID == first.SessionID {
		t.Fatal("expired session must not be resumed")
	}
}

func TestUploadSessionGetPreservesLiveCompletionLeasePastSessionTTL(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	clock := now
	repos := newMediaTestRepositories(t)
	svc := NewUploadSessionService(repos, nil, UploadSessionConfig{
		MaxFileSize: 100, MaxChunkSize: 5, SessionTTL: time.Hour,
		CompletionLease: 2 * time.Hour, Now: func() time.Time { return clock },
	})
	created, err := svc.Create(context.Background(), 7, validUploadSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repos.UploadSession.ClaimCompletion(repository.UploadSessionClaimRequest{
		SessionID: created.SessionID, UserID: 7, Token: "live-owner",
		Now: now, LeaseUntil: now.Add(2 * time.Hour),
	})
	if err != nil || !claimed {
		t.Fatalf("claim = %v, %v", claimed, err)
	}

	clock = now.Add(90 * time.Minute)
	view, err := svc.Get(context.Background(), 7, created.SessionID)
	if err != nil {
		t.Fatalf("Get() interrupted live completion lease: %v", err)
	}
	if view.Status != model.UploadSessionStatusCompleting {
		t.Fatalf("Get() status = %q, want completing", view.Status)
	}

	clock = now.Add(3 * time.Hour)
	if _, err := svc.Get(context.Background(), 7, created.SessionID); !IsUploadSessionError(err, UploadSessionErrorExpired) {
		t.Fatalf("Get() after stale lease error = %v, want expired", err)
	}
	current, err := repos.UploadSession.FindByIDForUser(created.SessionID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != model.UploadSessionStatusExpired || current.ActiveKey != nil {
		t.Fatalf("expired stale completion = %+v", current)
	}
}

func TestCompletedUploadSessionIsNotReused(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	repos := newMediaTestRepositories(t)
	svc := NewUploadSessionService(repos, nil, UploadSessionConfig{MaxFileSize: 100, MaxChunkSize: 5, SessionTTL: time.Hour, Now: func() time.Time { return now }})
	first, err := svc.Create(context.Background(), 7, validUploadSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repos.UploadSession.ClaimCompletion(repository.UploadSessionClaimRequest{SessionID: first.SessionID, UserID: 7, Token: "done", Now: now, LeaseUntil: now.Add(time.Minute)})
	if err != nil || !claimed {
		t.Fatalf("claim = %v, %v", claimed, err)
	}
	updated, err := repos.UploadSession.MarkCompleted(repository.UploadSessionCompletion{SessionID: first.SessionID, UserID: 7, Token: "done", VerifiedMD5: validUploadSessionRequest().ExpectedMD5, FinalObjectName: "final", AssetID: 1, TaskID: 1, CompletedAt: now})
	if err != nil || !updated {
		t.Fatalf("complete = %v, %v", updated, err)
	}
	second, err := svc.Create(context.Background(), 7, validUploadSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	if second.SessionID == first.SessionID {
		t.Fatal("completed session must not be reused")
	}
}

func TestCreateUploadSessionReportsExistingAssetWithoutRequiringChunks(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	repos := newMediaTestRepositories(t)
	req := validUploadSessionRequest()
	if err := repos.Asset.Create(&model.VideoAsset{FileMD5: req.ExpectedMD5, ObjectName: "videos/existing.mp4", FileSize: req.FileSize, ContentType: "video/mp4"}); err != nil {
		t.Fatal(err)
	}
	svc := NewUploadSessionService(repos, nil, UploadSessionConfig{MaxFileSize: 100, MaxChunkSize: 5, SessionTTL: time.Hour, Now: func() time.Time { return now }})
	view, err := svc.Create(context.Background(), 7, req)
	if err != nil {
		t.Fatal(err)
	}
	if !view.AssetAvailable {
		t.Fatal("existing matching asset should be reported")
	}
}

type memoryUploadObjectStore struct {
	mu      sync.Mutex
	objects map[string][]byte
	puts    []string
	deletes []string
	putErr  error
}

func newMemoryUploadObjectStore() *memoryUploadObjectStore {
	return &memoryUploadObjectStore{objects: make(map[string][]byte)}
}

func (s *memoryUploadObjectStore) PutObject(_ context.Context, name string, reader io.Reader, size int64, _ string) error {
	if s.putErr != nil {
		return s.putErr
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	if int64(len(data)) != size {
		return fmt.Errorf("put size = %d, want %d", len(data), size)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[name] = append([]byte(nil), data...)
	s.puts = append(s.puts, name)
	return nil
}

func (s *memoryUploadObjectStore) OpenObject(_ context.Context, name string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.objects[name]
	if !ok {
		return nil, fmt.Errorf("object %q not found", name)
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), data...))), nil
}

func (s *memoryUploadObjectStore) DeleteObject(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, name)
	s.deletes = append(s.deletes, name)
	return nil
}

func TestAcceptUploadSessionChunkValidatesBoundsAndExactSize(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	repos := newMediaTestRepositories(t)
	store := newMemoryUploadObjectStore()
	svc := NewUploadSessionService(repos, store, UploadSessionConfig{MaxFileSize: 100, MaxChunkSize: 5, SessionTTL: time.Hour, Now: func() time.Time { return now }})
	session, err := svc.Create(context.Background(), 7, validUploadSessionRequest())
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		index int
		data  []byte
	}{
		{"negative index", -1, []byte("12345")},
		{"past final index", 3, []byte("1")},
		{"short regular chunk", 0, []byte("1234")},
		{"long regular chunk", 0, []byte("123456")},
		{"short final chunk", 2, nil},
		{"long final chunk", 2, []byte("12")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.AcceptChunk(context.Background(), 7, session.SessionID, tc.index, bytes.NewReader(tc.data)); !IsUploadSessionError(err, UploadSessionErrorInvalid) {
				t.Fatalf("AcceptChunk() error = %v, want invalid", err)
			}
		})
	}
	if len(store.puts) != 0 {
		t.Fatalf("invalid chunks reached object store: %v", store.puts)
	}
}

func TestAcceptUploadSessionChunkIsIdempotentAndRejectsDifferentContent(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	repos := newMediaTestRepositories(t)
	store := newMemoryUploadObjectStore()
	svc := NewUploadSessionService(repos, store, UploadSessionConfig{MaxFileSize: 100, MaxChunkSize: 5, SessionTTL: time.Hour, Now: func() time.Time { return now }})
	session, err := svc.Create(context.Background(), 7, validUploadSessionRequest())
	if err != nil {
		t.Fatal(err)
	}

	first, err := svc.AcceptChunk(context.Background(), 7, session.SessionID, 0, bytes.NewBufferString("abcde"))
	if err != nil {
		t.Fatalf("first chunk: %v", err)
	}
	second, err := svc.AcceptChunk(context.Background(), 7, session.SessionID, 0, bytes.NewBufferString("abcde"))
	if err != nil {
		t.Fatalf("idempotent chunk: %v", err)
	}
	if first.ContentSHA256 != second.ContentSHA256 || len(store.puts) != 1 {
		t.Fatalf("idempotent result first=%+v second=%+v puts=%v", first, second, store.puts)
	}
	acceptedObject := first.ObjectName
	acceptedBytes := append([]byte(nil), store.objects[acceptedObject]...)

	if _, err := svc.AcceptChunk(context.Background(), 7, session.SessionID, 0, bytes.NewBufferString("vwxyz")); !IsUploadSessionError(err, UploadSessionErrorConflict) {
		t.Fatalf("different content error = %v, want conflict", err)
	}
	if !bytes.Equal(store.objects[acceptedObject], acceptedBytes) || len(store.puts) != 1 {
		t.Fatalf("accepted object was overwritten: puts=%v bytes=%q", store.puts, store.objects[acceptedObject])
	}
}

func TestAcceptUploadSessionChunkEnforcesOwnerAndLifecycle(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	clock := now
	repos := newMediaTestRepositories(t)
	store := newMemoryUploadObjectStore()
	svc := NewUploadSessionService(repos, store, UploadSessionConfig{MaxFileSize: 100, MaxChunkSize: 5, SessionTTL: time.Hour, Now: func() time.Time { return clock }})
	session, err := svc.Create(context.Background(), 7, validUploadSessionRequest())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.AcceptChunk(context.Background(), 8, session.SessionID, 0, bytes.NewBufferString("abcde")); !IsUploadSessionError(err, UploadSessionErrorNotFound) {
		t.Fatalf("foreign upload error = %v, want not found", err)
	}
	claimed, err := repos.UploadSession.ClaimCompletion(repository.UploadSessionClaimRequest{SessionID: session.SessionID, UserID: 7, Token: "lease", Now: now, LeaseUntil: now.Add(time.Minute)})
	if err != nil || !claimed {
		t.Fatalf("claim = %v, %v", claimed, err)
	}
	if _, err := svc.AcceptChunk(context.Background(), 7, session.SessionID, 0, bytes.NewBufferString("abcde")); !IsUploadSessionError(err, UploadSessionErrorInProgress) {
		t.Fatalf("completing upload error = %v, want in progress", err)
	}
	if _, err := repos.UploadSession.ReleaseCompletion(session.SessionID, 7, "lease", ""); err != nil {
		t.Fatal(err)
	}
	clock = now.Add(2 * time.Hour)
	if _, err := svc.AcceptChunk(context.Background(), 7, session.SessionID, 0, bytes.NewBufferString("abcde")); !IsUploadSessionError(err, UploadSessionErrorExpired) {
		t.Fatalf("expired upload error = %v, want expired", err)
	}
}

func TestAcceptUploadSessionChunkCleansCandidateWhenDatabaseWriteFails(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	repos, db := newMediaTestRepositoriesAndDB(t)
	store := newMemoryUploadObjectStore()
	svc := NewUploadSessionService(repos, store, UploadSessionConfig{MaxFileSize: 100, MaxChunkSize: 5, SessionTTL: time.Hour, Now: func() time.Time { return now }})
	session, err := svc.Create(context.Background(), 7, validUploadSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_upload_chunk_insert BEFORE INSERT ON upload_session_chunks BEGIN SELECT RAISE(FAIL, 'forced chunk insert failure'); END`).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := svc.AcceptChunk(context.Background(), 7, session.SessionID, 0, bytes.NewBufferString("abcde")); err == nil {
		t.Fatal("database failure must be returned")
	}
	if len(store.puts) != 1 || len(store.deletes) != 1 || store.puts[0] != store.deletes[0] {
		t.Fatalf("candidate cleanup puts=%v deletes=%v", store.puts, store.deletes)
	}
	if _, exists := store.objects[store.puts[0]]; exists {
		t.Fatal("failed candidate object still exists")
	}
}

func completableUploadSessionRequest() CreateUploadSessionRequest {
	return CreateUploadSessionRequest{
		Filename:    "demo.mp4",
		FileSize:    11,
		ChunkSize:   5,
		TotalChunks: 3,
		ExpectedMD5: "92b9cccc0b98c3a0b8d0df25a421c0e3",
	}
}

func createUploadSessionWithAllChunks(t *testing.T, svc *UploadSessionService, userID int64) *UploadSessionView {
	t.Helper()
	view, err := svc.Create(context.Background(), userID, completableUploadSessionRequest())
	if err != nil {
		t.Fatalf("create upload session: %v", err)
	}
	for index, data := range [][]byte{[]byte("abcde"), []byte("fghij"), []byte("k")} {
		if _, err := svc.AcceptChunk(context.Background(), userID, view.SessionID, index, bytes.NewReader(data)); err != nil {
			t.Fatalf("accept chunk %d: %v", index, err)
		}
	}
	return view
}

func TestCompleteUploadSessionReleasesClaimWhenChunksAreMissing(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	repos := newMediaTestRepositories(t)
	store := newMemoryUploadObjectStore()
	svc := NewUploadSessionService(repos, store, UploadSessionConfig{MaxFileSize: 100, MaxChunkSize: 5, SessionTTL: time.Hour, CompletionLease: time.Minute, Now: func() time.Time { return now }})
	created, err := svc.Create(context.Background(), 7, completableUploadSessionRequest())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Complete(context.Background(), 7, created.SessionID); !IsUploadSessionError(err, UploadSessionErrorConflict) {
		t.Fatalf("Complete() error = %v, want conflict", err)
	}
	current, err := repos.UploadSession.FindByIDForUser(created.SessionID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != model.UploadSessionStatusActive || current.CompletionToken != "" || current.CompletionLeaseExpiresAt != nil {
		t.Fatalf("missing chunks did not release claim: %+v", current)
	}
}

func TestCompleteUploadSessionVerifiesContentAndIsIdempotent(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	repos, db := newMediaTestRepositoriesAndDB(t)
	store := newMemoryUploadObjectStore()
	svc := NewUploadSessionService(repos, store, UploadSessionConfig{MaxFileSize: 100, MaxChunkSize: 5, SessionTTL: time.Hour, CompletionLease: time.Minute, Now: func() time.Time { return now }})
	created := createUploadSessionWithAllChunks(t, svc, 7)

	first, err := svc.Complete(context.Background(), 7, created.SessionID)
	if err != nil {
		t.Fatalf("first Complete(): %v", err)
	}
	second, err := svc.Complete(context.Background(), 7, created.SessionID)
	if err != nil {
		t.Fatalf("repeated Complete(): %v", err)
	}
	if first.TaskID == 0 || second.TaskID != first.TaskID || second.TraceID != first.TraceID {
		t.Fatalf("completion was not idempotent: first=%+v second=%+v", first, second)
	}
	var taskCount int64
	if err := db.Model(&model.VideoTask{}).Where("id = ?", first.TaskID).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 1 {
		t.Fatalf("task count = %d, want 1", taskCount)
	}
	task, err := repos.Task.FindByID(first.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.SourceType != model.TaskSourceTypeChunked || task.Stage != model.TaskStageUploaded || task.Status != model.TaskStatusPending {
		t.Fatalf("created task = %+v", task)
	}
	finalName := fmt.Sprintf("upload-sessions/%s/final.mp4", created.SessionID)
	if got := string(store.objects[finalName]); got != "abcdefghijk" {
		t.Fatalf("final object = %q", got)
	}
	current, err := repos.UploadSession.FindByIDForUser(created.SessionID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != model.UploadSessionStatusCompleted || current.VerifiedMD5 != completableUploadSessionRequest().ExpectedMD5 || current.TaskID == nil || *current.TaskID != first.TaskID {
		t.Fatalf("completed session = %+v", current)
	}
	for _, chunk := range []int{0, 1, 2} {
		prefix := fmt.Sprintf("upload-sessions/%s/chunks/%d/", created.SessionID, chunk)
		for name := range store.objects {
			if len(name) >= len(prefix) && name[:len(prefix)] == prefix {
				t.Fatalf("accepted chunk object was not cleaned: %s", name)
			}
		}
	}
}

func TestCompleteUploadSessionMarksHashMismatchFailed(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	repos, db := newMediaTestRepositoriesAndDB(t)
	store := newMemoryUploadObjectStore()
	svc := NewUploadSessionService(repos, store, UploadSessionConfig{MaxFileSize: 100, MaxChunkSize: 5, SessionTTL: time.Hour, CompletionLease: time.Minute, Now: func() time.Time { return now }})
	req := completableUploadSessionRequest()
	req.ExpectedMD5 = "0123456789abcdef0123456789abcdef"
	created, err := svc.Create(context.Background(), 7, req)
	if err != nil {
		t.Fatal(err)
	}
	for index, data := range [][]byte{[]byte("abcde"), []byte("fghij"), []byte("k")} {
		if _, err := svc.AcceptChunk(context.Background(), 7, created.SessionID, index, bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := svc.Complete(context.Background(), 7, created.SessionID); !IsUploadSessionError(err, UploadSessionErrorFailed) {
		t.Fatalf("Complete() error = %v, want failed", err)
	}
	current, err := repos.UploadSession.FindByIDForUser(created.SessionID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != model.UploadSessionStatusFailed || current.ActiveKey != nil || current.CompletionToken != "" {
		t.Fatalf("hash mismatch session = %+v", current)
	}
	finalName := fmt.Sprintf("upload-sessions/%s/final.mp4", created.SessionID)
	if _, exists := store.objects[finalName]; exists {
		t.Fatal("mismatched final object still exists")
	}
	var taskCount int64
	if err := db.Model(&model.VideoTask{}).Count(&taskCount).Error; err != nil || taskCount != 0 {
		t.Fatalf("task count = %d, err = %v", taskCount, err)
	}
}

func TestCompleteUploadSessionRejectsLiveLeaseAndReclaimsStaleLease(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	clock := now
	repos := newMediaTestRepositories(t)
	store := newMemoryUploadObjectStore()
	svc := NewUploadSessionService(repos, store, UploadSessionConfig{MaxFileSize: 100, MaxChunkSize: 5, SessionTTL: time.Hour, CompletionLease: time.Minute, Now: func() time.Time { return clock }})
	created := createUploadSessionWithAllChunks(t, svc, 7)
	claimed, err := repos.UploadSession.ClaimCompletion(repository.UploadSessionClaimRequest{SessionID: created.SessionID, UserID: 7, Token: "first-owner", Now: now, LeaseUntil: now.Add(time.Minute)})
	if err != nil || !claimed {
		t.Fatalf("claim = %v, %v", claimed, err)
	}

	if _, err := svc.Complete(context.Background(), 7, created.SessionID); !IsUploadSessionError(err, UploadSessionErrorInProgress) {
		t.Fatalf("live-lease Complete() error = %v, want in progress", err)
	}
	clock = now.Add(2 * time.Minute)
	result, err := svc.Complete(context.Background(), 7, created.SessionID)
	if err != nil || result.TaskID == 0 {
		t.Fatalf("stale-lease Complete() = %+v, %v", result, err)
	}
}

func TestCompleteUploadSessionUsesExistingAssetWithoutChunks(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	repos := newMediaTestRepositories(t)
	req := completableUploadSessionRequest()
	asset := &model.VideoAsset{FileMD5: req.ExpectedMD5, ObjectName: "videos/existing.mp4", FileSize: req.FileSize, ContentType: "video/mp4"}
	if err := repos.Asset.Create(asset); err != nil {
		t.Fatal(err)
	}
	svc := NewUploadSessionService(repos, nil, UploadSessionConfig{MaxFileSize: 100, MaxChunkSize: 5, SessionTTL: time.Hour, CompletionLease: time.Minute, Now: func() time.Time { return now }})
	created, err := svc.Create(context.Background(), 7, req)
	if err != nil {
		t.Fatal(err)
	}

	result, err := svc.Complete(context.Background(), 7, created.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	task, err := repos.Task.FindByID(result.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.AssetID == nil || *task.AssetID != asset.ID || task.FileURL != asset.ObjectName || task.SourceType != model.TaskSourceTypeChunked {
		t.Fatalf("existing-asset task = %+v", task)
	}
}

func TestCompleteUploadSessionReleasesClaimAfterDatabaseFinalizationFailure(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	repos, db := newMediaTestRepositoriesAndDB(t)
	store := newMemoryUploadObjectStore()
	svc := NewUploadSessionService(repos, store, UploadSessionConfig{MaxFileSize: 100, MaxChunkSize: 5, SessionTTL: time.Hour, CompletionLease: time.Minute, Now: func() time.Time { return now }})
	created := createUploadSessionWithAllChunks(t, svc, 7)
	if err := db.Exec(`CREATE TRIGGER fail_upload_session_task_insert BEFORE INSERT ON video_tasks BEGIN SELECT RAISE(FAIL, 'forced task insert failure'); END`).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Complete(context.Background(), 7, created.SessionID); err == nil {
		t.Fatal("Complete() error = nil, want finalization failure")
	}
	current, err := repos.UploadSession.FindByIDForUser(created.SessionID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != model.UploadSessionStatusActive || current.TaskID != nil || current.CompletionToken != "" || current.CompletionLeaseExpiresAt != nil {
		t.Fatalf("finalization failure left bad session state: %+v", current)
	}
	var taskCount, assetCount int64
	if err := db.Model(&model.VideoTask{}).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.VideoAsset{}).Count(&assetCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 0 || assetCount != 0 {
		t.Fatalf("rolled-back rows: tasks=%d assets=%d", taskCount, assetCount)
	}
	finalName := fmt.Sprintf("upload-sessions/%s/final.mp4", created.SessionID)
	if got := string(store.objects[finalName]); got != "abcdefghijk" {
		t.Fatalf("stable retryable final candidate = %q", got)
	}
}

func TestCompleteUploadSessionEnforcesOwnerAndExpiry(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	clock := now
	repos := newMediaTestRepositories(t)
	store := newMemoryUploadObjectStore()
	svc := NewUploadSessionService(repos, store, UploadSessionConfig{MaxFileSize: 100, MaxChunkSize: 5, SessionTTL: time.Hour, CompletionLease: time.Minute, Now: func() time.Time { return clock }})
	created, err := svc.Create(context.Background(), 7, completableUploadSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Complete(context.Background(), 8, created.SessionID); !IsUploadSessionError(err, UploadSessionErrorNotFound) {
		t.Fatalf("foreign Complete() error = %v, want not found", err)
	}
	clock = now.Add(2 * time.Hour)
	if _, err := svc.Complete(context.Background(), 7, created.SessionID); !IsUploadSessionError(err, UploadSessionErrorExpired) {
		t.Fatalf("expired Complete() error = %v, want expired", err)
	}
}
