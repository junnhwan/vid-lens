package store

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"vidlens/internal/domain"
)

func TestManualOriginalRetentionAndGlobalVideoList(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	r, c := prepared(t, s, session(t, s))
	video, e := s.Video(ctx, *r.SnapshotID)
	if e != nil || video.ExpiresAt != nil {
		t.Fatalf("original not manually retained: %+v %v", video, e)
	}
	if _, e = s.MarkVideoDeleting(ctx, r.SourceID, []string{video.ID}); e == nil {
		t.Fatal("deleted active source")
	}
	if e = s.Publish(ctx, c, domain.Article{SnapshotID: *r.SnapshotID, Title: "published"}); e != nil {
		t.Fatal(e)
	}
	// Old timestamps and pressure must never remove retained original media.
	if _, e = s.Pool.Exec(ctx, "UPDATE media_assets SET expires_at=now()-interval '7 days' WHERE id=$1", video.ID); e != nil {
		t.Fatal(e)
	}
	assets, e := s.CleanupAssets(ctx, true)
	if e != nil || len(assets) != 0 {
		t.Fatalf("automatic cleanup selected originals: %+v %v", assets, e)
	}
	other := session(t, s)
	if _, e = s.CreateRequest(ctx, other, "other", "https://www.bilibili.com/video/BV1xx411c7mD", "", false); e != nil {
		t.Fatal(e)
	}
	items, total, e := s.ManagedVideos(ctx, "", 1, 0)
	if e != nil || total != 2 || len(items) != 1 {
		t.Fatalf("global pagination: %+v %d %v", items, total, e)
	}
	items, total, e = s.ManagedVideos(ctx, "TEST VIDEO", 20, 0)
	if e != nil || total != 1 || len(items) != 1 || items[0].ArticleCount != 1 || items[0].Active || items[0].OriginalBytes != 42 {
		t.Fatalf("video/search aggregates: %+v %d %v", items, total, e)
	}
	b, _ := json.Marshal(items)
	for _, private := range []string{"session_id", "storage_key", "question", "checkpoint"} {
		if strings.Contains(string(b), private) {
			t.Fatalf("private field %s leaked", private)
		}
	}
	if _, e = s.Pool.Exec(ctx, "UPDATE media_assets SET expires_at=NULL WHERE id=$1", video.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Video(ctx, *r.SnapshotID); e != nil {
		t.Fatal("manual video not reusable", e)
	}
}

func TestDeleteProtectsPinsAndDoesNotDeleteNewerDownloads(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	r, c := prepared(t, s, session(t, s))
	old, e := s.Video(ctx, *r.SnapshotID)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Publish(ctx, c, domain.Article{SnapshotID: *r.SnapshotID}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(ctx, "INSERT INTO asset_refs(asset_id,owner_type,owner_id) VALUES($1,'test','protected')", old.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.MarkVideoDeleting(ctx, r.SourceID, []string{old.ID}); e == nil {
		t.Fatal("deleted protected original")
	}
	if _, e = s.Pool.Exec(ctx, "DELETE FROM asset_refs WHERE asset_id=$1", old.ID); e != nil {
		t.Fatal(e)
	}
	a, e := s.MarkVideoDeleting(ctx, r.SourceID, []string{old.ID})
	if e != nil || len(a) != 1 {
		t.Fatalf("delete: %+v %v", a, e)
	}
	if e = s.Deleted(ctx, old.ID, false); e != nil {
		t.Fatal(e)
	}
	v, e := s.StorageOverview(ctx)
	if e != nil || v.OriginalBytes != 42 {
		t.Fatalf("pending delete released quota: %+v %v", v, e)
	}
	if e = s.Deleted(ctx, old.ID, true); e != nil {
		t.Fatal(e)
	}
	a, e = s.MarkVideoDeleting(ctx, r.SourceID, []string{old.ID})
	if e != nil || len(a) != 0 {
		t.Fatalf("delete retry not idempotent: %+v %v", a, e)
	}
	// A newer copy is outside the reviewed asset IDs, even if the HTTP retry
	// comes after that copy has finished downloading.
	fresh := domain.ID()
	if _, e = s.Pool.Exec(ctx, `INSERT INTO media_assets(id,snapshot_id,source_id,run_id,kind,storage_key,sha256,mime,bytes) VALUES($1,$2,$3,$4,'video',$1,'new','video/mp4',99)`, fresh, r.SnapshotID, r.SourceID, c.Run.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.MarkVideoDeleting(ctx, r.SourceID, []string{old.ID}); e == nil {
		t.Fatal("stale delete selected newer original")
	}
	copy, e := s.Asset(ctx, fresh)
	if e != nil || copy.State != "ready" {
		t.Fatalf("new copy mutated: %+v %v", copy, e)
	}
}

func TestStorageQuotaIsAuthoritativeAcrossAdmissionAndReservations(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	r, c := prepared(t, s, session(t, s))
	first, e := s.StorageOverview(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ReserveStorage(ctx, c, 2<<30, IncomingFrameBytes, 100<<30, 200<<30, 85); e != nil {
		t.Fatal(e)
	}
	v, e := s.DiskCapacity(ctx, 100<<30, 200<<30, 85, 3<<30)
	if e != nil || v.UsedBytes != 42 || v.ReservedBytes != 2<<30 {
		t.Fatalf("reservation: %+v %v", v, e)
	}
	in := StorageInput{ExpectedRevision: first.Settings.Revision, VideoQuotaBytes: 1 << 30, ImageQuotaBytes: 1 << 30}
	if e = s.SaveStorageSettings(ctx, in, 1<<30); e == nil {
		t.Fatal("shrank below committed reservations")
	}
	in.VideoQuotaBytes = 6 << 30
	in.ImageQuotaBytes = 2 << 30
	if e = s.SaveStorageSettings(ctx, in, 3<<30); e != nil {
		t.Fatal(e)
	}
	if e = s.SaveStorageSettings(ctx, in, 3<<30); e == nil {
		t.Fatal("stale quota write accepted")
	}
	// The next run cannot use more than the newly selected service quota.
	if e = s.ReserveStorage(ctx, c, 7<<30, IncomingFrameBytes, 100<<30, 200<<30, 85); e == nil {
		t.Fatal("worker ignored persisted quota")
	}
	if e = s.Publish(ctx, c, domain.Article{SnapshotID: *r.SnapshotID}); e != nil {
		t.Fatal(e)
	}
	next, e := s.StorageOverview(ctx)
	if e != nil || next.ReservedVideoBytes != 0 || next.Settings.VideoQuotaBytes != 6<<30 {
		t.Fatalf("finished reservation/settings: %+v %v", next, e)
	}
}

func TestConcurrentDownloadsCannotOverbookTheServiceQuota(t *testing.T) {
	s := testStore(t)
	s.VideoConcurrency = 2
	ctx := context.Background()
	if e := s.SaveStorageSettings(ctx, StorageInput{ExpectedRevision: 1, VideoQuotaBytes: 6 << 30, ImageQuotaBytes: 5 << 30}, 3<<30); e != nil {
		t.Fatal(e)
	}
	claims := make([]domain.Claim, 2)
	for i, url := range []string{"https://youtube.com/watch?v=quotaone111", "https://youtube.com/watch?v=quotatwo222"} {
		owner := session(t, s)
		a, e := s.CreateRequest(ctx, owner, domain.ID(), url, "", false)
		if e != nil {
			t.Fatal(e)
		}
		r, e := s.Request(ctx, owner, a.ID)
		if e != nil {
			t.Fatal(e)
		}
		c, e := s.Claim(ctx, *r.ActiveRunID, 1, "quota")
		if e != nil || c == nil {
			t.Fatal(e)
		}
		claims[i] = *c
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, c := range claims {
		wg.Add(1)
		go func(c domain.Claim) {
			defer wg.Done()
			results <- s.ReserveStorage(ctx, c, 4<<30, IncomingFrameBytes, 100<<30, 200<<30, 85)
		}(c)
	}
	wg.Wait()
	close(results)
	passed := 0
	for e := range results {
		if e == nil {
			passed++
		}
	}
	if passed != 1 {
		t.Fatalf("admitted %d downloads into one available slot", passed)
	}
	v, e := s.DiskCapacity(ctx, 100<<30, 200<<30, 85, 3<<30)
	if e != nil || v.ReservedBytes != 4<<30 || v.Accepting {
		t.Fatalf("overbooked capacity: %+v %v", v, e)
	}
}

func TestStorageMigrationPreservesPublishedContentAndRetainsOldOriginal(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	r, c := prepared(t, s, session(t, s))
	if e := s.Publish(ctx, c, domain.Article{SnapshotID: *r.SnapshotID, Title: "preserved article"}); e != nil {
		t.Fatal(e)
	}
	video, e := s.Video(ctx, *r.SnapshotID)
	if e != nil {
		t.Fatal(e)
	}
	var before string
	if e = s.Pool.QueryRow(ctx, "SELECT content::text FROM articles WHERE generation_run_id=$1", c.Run.ID).Scan(&before); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(ctx, "UPDATE media_assets SET expires_at=$2 WHERE id=$1", video.ID, time.Now().Add(time.Hour)); e != nil {
		t.Fatal(e)
	}
	// Recreate the schema-12 storage state and apply the real embedded upgrade.
	for _, q := range []string{"DELETE FROM schema_migrations WHERE version=13", "DROP TABLE storage_settings", "DROP INDEX sources_requests", "DROP INDEX sources_assets"} {
		if _, e = s.Pool.Exec(ctx, q); e != nil {
			t.Fatal(e)
		}
	}
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	video, e = s.Video(ctx, *r.SnapshotID)
	if e != nil || video.ExpiresAt != nil || video.State != "ready" {
		t.Fatalf("old original not retained: %+v %v", video, e)
	}
	var after string
	if e = s.Pool.QueryRow(ctx, "SELECT content::text FROM articles WHERE generation_run_id=$1", c.Run.ID).Scan(&after); e != nil || before != after {
		t.Fatalf("migration changed article: %v", e)
	}
	v, e := s.StorageOverview(ctx)
	if e != nil || v.Settings.VideoQuotaBytes != DefaultVideoQuota || v.OriginalBytes != 42 {
		t.Fatalf("migrated quota: %+v %v", v, e)
	}
}
