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
	items, total, _, e := s.ManagedVideos(ctx, "", "", 1, 0)
	if e != nil || total != 2 || len(items) != 1 {
		t.Fatalf("global pagination: %+v %d %v", items, total, e)
	}
	items, total, _, e = s.ManagedVideos(ctx, "TEST VIDEO", "", 20, 0)
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
	if e = s.ReserveStorage(ctx, c, 2<<30, IncomingFrameBytes, 100<<30, 200<<30); e != nil {
		t.Fatal(e)
	}
	v, e := s.DiskCapacity(ctx, 100<<30, 200<<30, 3<<30)
	if e != nil || v.UsedBytes != 42 || v.ReservedBytes != 2<<30 {
		t.Fatalf("reservation: %+v %v", v, e)
	}
	in := StorageInput{ExpectedRevision: first.Settings.Revision, VideoQuotaBytes: 1 << 30, ImageQuotaBytes: 1 << 30, DiskStopPercent: 92}
	if e = s.SaveStorageSettings(ctx, in, 1<<30); e == nil {
		t.Fatal("shrank below committed reservations")
	}
	in.VideoQuotaBytes = 6 << 30
	in.ImageQuotaBytes = 2 << 30
	if e = s.SaveStorageSettings(ctx, in, 3<<30); e != nil {
		t.Fatal(e)
	}
	updated, e := s.StorageOverview(ctx)
	if e != nil || updated.Settings.DiskStopPercent != 92 {
		t.Fatalf("disk stop setting not persisted: %+v %v", updated.Settings, e)
	}
	if e = s.SaveStorageSettings(ctx, in, 3<<30); e == nil {
		t.Fatal("stale quota write accepted")
	}
	// The next run cannot use more than the newly selected service quota.
	if e = s.ReserveStorage(ctx, c, 7<<30, IncomingFrameBytes, 100<<30, 200<<30); e == nil {
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

func TestDiskStopSettingControlsAdmissionAndPublicCapacity(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	settings, err := s.StorageOverview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// 86% used plus a 3 GiB reservation is below the default 90% stop line,
	// while the same request would cross the previous 85% default.
	capacity, err := s.DiskCapacity(ctx, 14<<30, 100<<30, 3<<30)
	if err != nil || !capacity.Accepting {
		t.Fatalf("default 90%% policy refused sufficient disk space: %+v %v", capacity, err)
	}
	if err := s.SaveStorageSettings(ctx, StorageInput{
		ExpectedRevision: settings.Settings.Revision,
		VideoQuotaBytes:  settings.Settings.VideoQuotaBytes,
		ImageQuotaBytes:  settings.Settings.ImageQuotaBytes,
		DiskStopPercent:  85,
	}, 3<<30); err != nil {
		t.Fatal(err)
	}
	capacity, err = s.DiskCapacity(ctx, 14<<30, 100<<30, 3<<30)
	if err != nil || capacity.Accepting {
		t.Fatalf("updated 85%% policy did not close admission: %+v %v", capacity, err)
	}
	allowed, err := s.DiskSpaceAllowed(ctx, 14<<30, 100<<30, 3<<30)
	if err != nil || allowed {
		t.Fatalf("export disk check ignored the saved policy: allowed=%v err=%v", allowed, err)
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
			results <- s.ReserveStorage(ctx, c, 4<<30, IncomingFrameBytes, 100<<30, 200<<30)
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
	v, e := s.DiskCapacity(ctx, 100<<30, 200<<30, 3<<30)
	if e != nil || v.ReservedBytes != 4<<30 || v.Accepting {
		t.Fatalf("overbooked capacity: %+v %v", v, e)
	}
}

func TestManagedVideosSeparateWhatCanBeCleaned(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner := session(t, s)
	r, c := prepared(t, s, owner)
	if _, total, counts, e := s.ManagedVideos(ctx, "", "ready", 20, 0); e != nil || total != 0 || counts.Held != 1 || counts.Ready != 0 {
		t.Fatalf("running original looked cleanable: total=%d counts=%+v %v", total, counts, e)
	}
	if _, total, _, e := s.ManagedVideos(ctx, "", "held", 20, 0); e != nil || total != 1 {
		t.Fatalf("held filter: %d %v", total, e)
	}
	if e := s.Publish(ctx, c, domain.Article{SnapshotID: *r.SnapshotID, Title: "kept"}); e != nil {
		t.Fatal(e)
	}
	items, total, counts, e := s.ManagedVideos(ctx, "", "ready", 20, 0)
	if e != nil || total != 1 || len(items) != 1 || items[0].Cleanup != "ready" || counts.Ready != 1 || counts.Held != 0 || len(items[0].AssetIDs) == 0 {
		t.Fatalf("finished original not cleanable: %+v total=%d counts=%+v %v", items, total, counts, e)
	}
	sourceID, assetID := items[0].ID, items[0].AssetIDs[0]
	if _, e = s.MarkVideoDeleting(ctx, sourceID, []string{assetID}); e != nil {
		t.Fatal(e)
	}
	if e = s.Deleted(ctx, assetID, true); e != nil {
		t.Fatal(e)
	}
	items, total, counts, e = s.ManagedVideos(ctx, "", "gone", 20, 0)
	if e != nil || total != 1 || len(items) != 1 || items[0].Cleanup != "gone" || len(items[0].AssetIDs) != 0 || counts.Gone != 1 || counts.Ready != 0 {
		t.Fatalf("removed original still offered: %+v total=%d counts=%+v %v", items, total, counts, e)
	}
	if _, e = s.CreateRequest(ctx, owner, domain.ID(), "https://youtube.com/watch?v=heldvideo01", "", false); e != nil {
		t.Fatal(e)
	}
	if _, total, counts, e = s.ManagedVideos(ctx, "", "", 20, 0); e != nil || total != 2 || counts.Held != 1 || counts.Gone != 1 || counts.Ready != 0 {
		t.Fatalf("mixed cleanup counts: total=%d counts=%+v %v", total, counts, e)
	}
	if _, _, _, e = s.ManagedVideos(ctx, "", "later", 20, 0); e == nil {
		t.Fatal("unknown cleanup accepted")
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
	for _, q := range []string{"DELETE FROM schema_migrations WHERE version IN (13,23,24,25)", "DROP TABLE storage_settings", "DROP INDEX sources_requests", "DROP INDEX sources_assets"} {
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
	if e != nil || v.Settings.VideoQuotaBytes != DefaultVideoQuota || v.Settings.DiskStopPercent != 98 || v.OriginalBytes != 42 {
		t.Fatalf("migrated quota: %+v %v", v, e)
	}
}
