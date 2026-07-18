package repository

import (
	"testing"
	"time"

	"vid-lens/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newUploadSessionRepositoryTestDB(t *testing.T) (*Repositories, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.UploadSession{}, &model.UploadSessionChunk{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewRepositories(db), db
}

func testUploadSession(now time.Time) *model.UploadSession {
	activeKey := "user-7-manifest"
	return &model.UploadSession{
		ID:                  "11111111-1111-1111-1111-111111111111",
		UserID:              7,
		Filename:            "demo.mp4",
		FileSize:            11,
		ChunkSize:           5,
		TotalChunks:         3,
		ExpectedMD5:         "0123456789abcdef0123456789abcdef",
		ManifestFingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ActiveKey:           &activeKey,
		Status:              model.UploadSessionStatusActive,
		ExpiresAt:           now.Add(time.Hour),
	}
}

func TestUploadSessionRepositoryFindsOnlyOwnedSessionAndActiveKey(t *testing.T) {
	repos, _ := newUploadSessionRepositoryTestDB(t)
	now := time.Now().UTC()
	session := testUploadSession(now)
	if err := repos.UploadSession.Create(session); err != nil {
		t.Fatalf("create: %v", err)
	}

	owned, err := repos.UploadSession.FindByIDForUser(session.ID, 7)
	if err != nil || owned == nil || owned.ID != session.ID {
		t.Fatalf("owned session = %+v, %v", owned, err)
	}
	foreign, err := repos.UploadSession.FindByIDForUser(session.ID, 8)
	if err != nil || foreign != nil {
		t.Fatalf("foreign session = %+v, %v, want nil", foreign, err)
	}
	active, err := repos.UploadSession.FindByActiveKey(7, *session.ActiveKey)
	if err != nil || active == nil || active.ID != session.ID {
		t.Fatalf("active session = %+v, %v", active, err)
	}
}

func TestUploadSessionRepositoryStoresUniqueChunksInIndexOrder(t *testing.T) {
	repos, _ := newUploadSessionRepositoryTestDB(t)
	session := testUploadSession(time.Now().UTC())
	if err := repos.UploadSession.Create(session); err != nil {
		t.Fatal(err)
	}
	for _, index := range []int{2, 0} {
		chunk := &model.UploadSessionChunk{
			SessionID: session.ID, ChunkIndex: index, ActualSize: 1,
			ContentSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			ObjectName:    "chunk",
		}
		if err := repos.UploadSession.CreateChunk(chunk); err != nil {
			t.Fatalf("create chunk %d: %v", index, err)
		}
	}
	duplicate := &model.UploadSessionChunk{
		SessionID: session.ID, ChunkIndex: 0, ActualSize: 1,
		ContentSHA256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		ObjectName:    "conflict",
	}
	if err := repos.UploadSession.CreateChunk(duplicate); err == nil {
		t.Fatal("duplicate session/index must fail")
	}
	chunks, err := repos.UploadSession.ListChunks(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 || chunks[0].ChunkIndex != 0 || chunks[1].ChunkIndex != 2 {
		t.Fatalf("ordered chunks = %+v", chunks)
	}
	found, err := repos.UploadSession.FindChunk(session.ID, 0)
	if err != nil || found == nil || found.ObjectName != "chunk" {
		t.Fatalf("found chunk = %+v, %v", found, err)
	}
}

func TestUploadSessionRepositoryClaimsAndReclaimsCompletionLease(t *testing.T) {
	repos, _ := newUploadSessionRepositoryTestDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	session := testUploadSession(now)
	if err := repos.UploadSession.Create(session); err != nil {
		t.Fatal(err)
	}

	claimed, err := repos.UploadSession.ClaimCompletion(UploadSessionClaimRequest{
		SessionID: session.ID, UserID: 7, Token: "token-a", Now: now, LeaseUntil: now.Add(time.Minute),
	})
	if err != nil || !claimed {
		t.Fatalf("first claim = %v, %v", claimed, err)
	}
	claimed, err = repos.UploadSession.ClaimCompletion(UploadSessionClaimRequest{
		SessionID: session.ID, UserID: 7, Token: "token-b", Now: now.Add(30 * time.Second), LeaseUntil: now.Add(90 * time.Second),
	})
	if err != nil || claimed {
		t.Fatalf("live lease claim = %v, %v, want false", claimed, err)
	}
	claimed, err = repos.UploadSession.ClaimCompletion(UploadSessionClaimRequest{
		SessionID: session.ID, UserID: 7, Token: "token-c", Now: now.Add(2 * time.Minute), LeaseUntil: now.Add(3 * time.Minute),
	})
	if err != nil || !claimed {
		t.Fatalf("stale lease reclaim = %v, %v", claimed, err)
	}

	current, err := repos.UploadSession.FindByIDForUser(session.ID, 7)
	if err != nil || current.CompletionToken != "token-c" || current.Status != model.UploadSessionStatusCompleting {
		t.Fatalf("claimed session = %+v, %v", current, err)
	}
}

func TestUploadSessionRepositoryExpiresOnlyUnownedStaleCompletion(t *testing.T) {
	repos, _ := newUploadSessionRepositoryTestDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	session := testUploadSession(now)
	session.ExpiresAt = now.Add(time.Minute)
	if err := repos.UploadSession.Create(session); err != nil {
		t.Fatal(err)
	}
	claimed, err := repos.UploadSession.ClaimCompletion(UploadSessionClaimRequest{
		SessionID: session.ID, UserID: 7, Token: "owner", Now: now, LeaseUntil: now.Add(2 * time.Hour),
	})
	if err != nil || !claimed {
		t.Fatalf("claim = %v, %v", claimed, err)
	}

	updated, err := repos.UploadSession.MarkExpired(session.ID, 7, now.Add(90*time.Minute))
	if err != nil || updated {
		t.Fatalf("live completion expiry = %v, %v, want false", updated, err)
	}
	updated, err = repos.UploadSession.MarkExpired(session.ID, 7, now.Add(3*time.Hour))
	if err != nil || !updated {
		t.Fatalf("stale completion expiry = %v, %v, want true", updated, err)
	}
	current, err := repos.UploadSession.FindByIDForUser(session.ID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != model.UploadSessionStatusExpired || current.ActiveKey != nil || current.CompletionToken != "" {
		t.Fatalf("expired completion = %+v", current)
	}
}

func TestUploadSessionRepositoryReleaseFailExpireAndCompleteRequireOwnershipToken(t *testing.T) {
	t.Run("release", func(t *testing.T) {
		repos, _ := newUploadSessionRepositoryTestDB(t)
		now := time.Now().UTC()
		session := testUploadSession(now)
		if err := repos.UploadSession.Create(session); err != nil {
			t.Fatal(err)
		}
		claimed, err := repos.UploadSession.ClaimCompletion(UploadSessionClaimRequest{SessionID: session.ID, UserID: 7, Token: "owned", Now: now, LeaseUntil: now.Add(time.Minute)})
		if err != nil || !claimed {
			t.Fatalf("claim: %v, %v", claimed, err)
		}
		updated, err := repos.UploadSession.ReleaseCompletion(session.ID, 7, "wrong", "failed")
		if err != nil || updated {
			t.Fatalf("wrong-token release = %v, %v", updated, err)
		}
		updated, err = repos.UploadSession.ReleaseCompletion(session.ID, 7, "owned", "retryable")
		if err != nil || !updated {
			t.Fatalf("owned release = %v, %v", updated, err)
		}
		current, _ := repos.UploadSession.FindByIDForUser(session.ID, 7)
		if current.Status != model.UploadSessionStatusActive || current.CompletionToken != "" || current.LastError != "retryable" {
			t.Fatalf("released = %+v", current)
		}
	})

	t.Run("failed", func(t *testing.T) {
		repos, _ := newUploadSessionRepositoryTestDB(t)
		now := time.Now().UTC()
		session := testUploadSession(now)
		if err := repos.UploadSession.Create(session); err != nil {
			t.Fatal(err)
		}
		claimed, _ := repos.UploadSession.ClaimCompletion(UploadSessionClaimRequest{SessionID: session.ID, UserID: 7, Token: "owned", Now: now, LeaseUntil: now.Add(time.Minute)})
		if !claimed {
			t.Fatal("claim failed")
		}
		updated, err := repos.UploadSession.MarkFailed(session.ID, 7, "owned", "hash mismatch")
		if err != nil || !updated {
			t.Fatalf("mark failed = %v, %v", updated, err)
		}
		current, _ := repos.UploadSession.FindByIDForUser(session.ID, 7)
		if current.Status != model.UploadSessionStatusFailed || current.ActiveKey != nil {
			t.Fatalf("failed = %+v", current)
		}
	})

	t.Run("expired", func(t *testing.T) {
		repos, _ := newUploadSessionRepositoryTestDB(t)
		now := time.Now().UTC()
		session := testUploadSession(now)
		session.ExpiresAt = now.Add(-time.Second)
		if err := repos.UploadSession.Create(session); err != nil {
			t.Fatal(err)
		}
		updated, err := repos.UploadSession.MarkExpired(session.ID, 7, now)
		if err != nil || !updated {
			t.Fatalf("mark expired = %v, %v", updated, err)
		}
		current, _ := repos.UploadSession.FindByIDForUser(session.ID, 7)
		if current.Status != model.UploadSessionStatusExpired || current.ActiveKey != nil {
			t.Fatalf("expired = %+v", current)
		}
	})

	t.Run("completed", func(t *testing.T) {
		repos, _ := newUploadSessionRepositoryTestDB(t)
		now := time.Now().UTC()
		session := testUploadSession(now)
		if err := repos.UploadSession.Create(session); err != nil {
			t.Fatal(err)
		}
		claimed, _ := repos.UploadSession.ClaimCompletion(UploadSessionClaimRequest{SessionID: session.ID, UserID: 7, Token: "owned", Now: now, LeaseUntil: now.Add(time.Minute)})
		if !claimed {
			t.Fatal("claim failed")
		}
		assetID, taskID := int64(10), int64(20)
		updated, err := repos.UploadSession.MarkCompleted(UploadSessionCompletion{
			SessionID: session.ID, UserID: 7, Token: "owned", VerifiedMD5: session.ExpectedMD5,
			FinalObjectName: "videos/final.mp4", AssetID: assetID, TaskID: taskID, CompletedAt: now,
		})
		if err != nil || !updated {
			t.Fatalf("mark completed = %v, %v", updated, err)
		}
		current, _ := repos.UploadSession.FindByIDForUser(session.ID, 7)
		if current.Status != model.UploadSessionStatusCompleted || current.TaskID == nil || *current.TaskID != taskID || current.ActiveKey != nil {
			t.Fatalf("completed = %+v", current)
		}
	})
}
